import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import {
  MemoryRouter,
  NavigateFunction,
  Route,
  Routes,
  useLocation,
  useNavigate,
  useNavigationType,
} from 'react-router-dom';
import { ProjectSettings } from './ProjectSettings';
import { DialogProvider } from '../components/ui';
import { useAppStore } from '../state/store';

// Refactor plan step S16c (invariant I21): project settings as they render
// today, tab by tab, before F6 moves the JSX of the seven tabs into
// views/projectSettings/<Name>Tab.tsx. F6 keeps every piece of state, the
// eight loads and the handlers in the shell, so it must leave every file
// snapshot, every ordered API-call list, the ?tab= history and the tablist
// markup below identical.
//
//   click  the page opened at /projects/p1/settings (no ?tab=, so General
//          shows), then each tab button clicked in TABS order.
//   link   a fresh mount at ?tab=<key>, once per tab.
//
// Each tab, entered each way, writes container.innerHTML, one tag per line so
// a diff reads, to __snapshots__/ProjectSettings.<tab>.<click|link>.html, and
// asserts the ordered API calls, the URL and the history action the switch
// leaves, the tablist's ARIA and the values of the form controls (which
// innerHTML does not carry). The two ways look the same but serialize
// differently, because innerHTML keeps the DOM's history: React patches a
// tab button's style one property at a time when it stops being the open tab,
// and when a table's <div> takes over the node of the "Loading…" <div> before
// it, the node keeps an empty style="" (on a deep link the tab is open while
// its load is under way; a click opens it after). So each way has its own
// file. Three more tests pin that an unknown ?tab= shows General, that a tab
// switch keeps unsaved input and reloads nothing, and what a stable-channel
// workspace (the gated sections off) sees. The store is the real one;
// children render for real; only the API client is replaced, by canned
// answers.
//
// Regenerate only for a deliberate change to what the view shows or loads,
// never in a refactor pull request:
//   npx vitest run src/views/ProjectSettings.snapshot.test.tsx -u

// Every *API namespace of the client is replaced by recorders over a canned
// responder: the call is logged, in the order made, as `ns.method(args)`.
// URL builders are synchronous and do no I/O, so they keep the real code.
const recorder = vi.hoisted(() => ({
  calls: [] as string[],
  respond: (_name: string, _args: unknown[]): unknown => undefined,
}));

vi.mock('../api/client', async (importOriginal) => {
  const actual = await importOriginal<Record<string, unknown>>();
  const out: Record<string, unknown> = { ...actual };
  for (const [ns, value] of Object.entries(actual)) {
    if (!ns.endsWith('API') || typeof value !== 'object' || value === null) continue;
    const wrapped: Record<string, unknown> = {};
    for (const [method, fn] of Object.entries(value as Record<string, unknown>)) {
      if (typeof fn !== 'function' || /Url$|URL$/.test(method)) {
        wrapped[method] = fn;
        continue;
      }
      wrapped[method] = (...args: unknown[]) => {
        const name = `${ns}.${method}`;
        recorder.calls.push(`${name}(${args.map((a) => JSON.stringify(a)).join(', ')})`);
        const data = recorder.respond(name, args);
        return data === undefined
          ? Promise.reject(new Error(`no canned response for ${name}`))
          : Promise.resolve(data).then((body) => ({ data: body, headers: {} }));
      };
    }
    out[ns] = wrapped;
  }
  return out;
});

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

// ---- canned data -----------------------------------------------------------

const project = (id: string, name: string, parent: string, org = 'o1', agentAuth = 'user-account') => ({
  id,
  org_id: org,
  name,
  description: '',
  agent_auth: agentAuth,
  parent_project_id: parent,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
});

//  p0 Vehicle            top level       a parent candidate
//    p1 Fuel pump        this project, refines p0, runs on the API key
//      p2 Impeller       a child: listed, and not a candidate
//        p3 Seal kit     a grandchild: not a candidate either
//  p5 Chassis            top level       a parent candidate
//  p9 Elsewhere          another workspace: not a candidate
const PROJECTS = [
  project('p0', 'Vehicle', ''),
  project('p1', 'Fuel pump', 'p0', 'o1', 'api-key'),
  project('p2', 'Impeller', 'p1'),
  project('p3', 'Seal kit', 'p2'),
  project('p5', 'Chassis', ''),
  project('p9', 'Elsewhere', '', 'o2'),
];

const MEMBERS = [
  { project_id: 'p1', user_id: 'u1', role: 'owner', user_name: 'Dana Reyes', user_email: 'dana@example.com', avatar_url: '' },
  {
    project_id: 'p1',
    user_id: 'u2',
    role: 'editor',
    user_name: 'Sam Lee',
    user_email: 'sam@example.com',
    avatar_url: 'https://example.com/sam.png',
  },
  { project_id: 'p1', user_id: 'u3', role: 'reviewer', user_email: 'kim@example.com' },
  { project_id: 'p1', user_id: 'u4', role: 'viewer', user_name: 'Ola Berg' },
];

const REPOS = [
  {
    id: 'repo-1',
    project_id: 'p1',
    name: 'main-app',
    remote_url: 'https://github.com/acme/main-app.git',
    default_branch: 'main',
    credential_strategy: 'host',
    my_local_path: '/home/dana/main-app',
  },
  { id: 'repo-2', project_id: 'p1', name: 'docs', remote_url: '', default_branch: '', credential_strategy: 'host' },
];

const TEAM_GRANTS = [{ project_id: 'p1', org_team_id: 't1', role: 'editor', team_name: 'Firmware' }];

const ORG_TEAMS = [
  { id: 't1', org_id: 'o1', name: 'Firmware', description: '', members: [] },
  { id: 't2', org_id: 'o1', name: 'Test lab', description: '', members: [] },
];

// The clock is frozen at 2026-06-01: one link open, one expiring later, one
// expired (still offered for revoking) and one revoked.
const SHARE_LINKS = [
  { id: 'sl-1', project_id: 'p1', role: 'public', label: 'Investors', created_at: '2026-03-01T09:00:00Z' },
  {
    id: 'sl-2',
    project_id: 'p1',
    role: 'reviewer',
    label: 'Supplier review',
    created_at: '2026-04-01T09:00:00Z',
    expires_at: '2026-09-30T23:59:59Z',
  },
  { id: 'sl-3', project_id: 'p1', role: 'public', label: '', created_at: '2026-01-10T09:00:00Z', expires_at: '2026-02-01T23:59:59Z' },
  {
    id: 'sl-4',
    project_id: 'p1',
    role: 'reviewer',
    label: 'Old',
    created_at: '2026-01-05T09:00:00Z',
    revoked_at: '2026-02-15T09:00:00Z',
  },
];

const PARTIES = {
  parties: [
    { name: 'Acme Pumps', default: true },
    { name: 'Landing gear supplier', note: 'Tier 2' },
  ],
};

const ATTRIBUTE_DEFINITIONS = [
  {
    id: 'a1',
    org_id: null,
    project_id: 'p1',
    key: 'priority',
    label: 'Priority',
    data_type: 'enum',
    enum_values: ['low', 'medium', 'high'],
    applies_to_type: 'requirement',
    required: true,
    sort_order: 1,
    created_at: '2026-01-02T00:00:00Z',
  },
  {
    id: 'a2',
    org_id: null,
    project_id: 'p1',
    key: 'cost',
    label: 'Cost',
    data_type: 'number',
    enum_values: [],
    applies_to_type: '',
    required: false,
    sort_order: 2,
    created_at: '2026-01-02T00:00:00Z',
  },
];

const ARTIFACT_TYPES = [
  { value: 'requirement', label: 'Requirement', description: '', color: '#2c8ef0' },
  { value: 'test_case', label: 'Test case', description: '', color: '#3aa76d' },
];

// The workspace speaks "shall" and quiets passive voice; the project makes
// weak wording an error.
const QUALITY_RULES = {
  effective: {
    convention: 'shall',
    severities: { 'weak-word': 'error', 'passive-voice': 'info', 'not-testable': 'warning' },
  },
  workspace: { convention: 'shall', severities: { 'passive-voice': 'info' } },
  project: { severities: { 'weak-word': 'error' } },
  summary: 'shall; weak wording is an error',
  catalog: {
    conventions: ['shall', 'rfc2119'],
    rules: ['weak-word', 'passive-voice', 'not-testable'],
    severities: ['error', 'warning', 'info', 'off'],
    defaults: {
      convention: 'shall',
      severities: { 'weak-word': 'warning', 'passive-voice': 'warning', 'not-testable': 'warning' },
    },
    labels: {
      shall: 'the system shall',
      rfc2119: 'MUST, SHOULD, MAY',
      'weak-word': 'weak or subjective wording',
      'passive-voice': 'who acts is unstated',
      'not-testable': 'no measurable criterion',
    },
  },
};

const CANNED: Record<string, (...args: any[]) => unknown> = {
  'membersAPI.list': () => MEMBERS,
  'repoConnectionsAPI.list': () => REPOS,
  'projectTeamAccessAPI.list': () => TEAM_GRANTS,
  'shareLinkAPI.list': () => SHARE_LINKS,
  'orgTeamsAPI.list': () => ORG_TEAMS,
  'projectAPI.get': (id: string) => PROJECTS.find((p) => p.id === id),
  'projectAPI.list': () => PROJECTS,
  'projectAPI.parties': () => PARTIES,
  'attributeDefinitionAPI.listByProject': () => ATTRIBUTE_DEFINITIONS,
  'metaAPI.artifactTypes': () => ARTIFACT_TYPES,
  'qualityRulesAPI.forProject': () => QUALITY_RULES,
};

recorder.respond = (name, args) => CANNED[name]?.(...args);

// ---- harness ---------------------------------------------------------------

const initialStore = useAppStore.getState();

let container: HTMLDivElement;
let root: Root;
// Where the view leaves the URL, and how it got there: ?tab= is replaced.
let location = '';
let navigationType = '';
let navigate: NavigateFunction = () => {};

const HistoryProbe: React.FC = () => {
  const here = useLocation();
  location = `${here.pathname}${here.search}`;
  navigationType = useNavigationType();
  navigate = useNavigate();
  return null;
};

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const mount = async (entries: string[], initialIndex = entries.length - 1) => {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={entries} initialIndex={initialIndex}>
        <DialogProvider>
          <Routes>
            <Route path="/projects" element={<div>Project list</div>} />
            <Route path="/projects/:projectId/settings" element={<ProjectSettings />} />
          </Routes>
        </DialogProvider>
        <HistoryProbe />
      </MemoryRouter>
    );
  });
  await flush();
};

const byText = <T extends Element>(selector: string, text: string): T => {
  const found = Array.from(container.querySelectorAll<T>(selector)).find(
    (node) => (node.textContent ?? '').trim() === text
  );
  expect(found, `${selector} "${text}"`).toBeTruthy();
  return found!;
};

const click = async (node: Element) => {
  await act(async () => {
    (node as HTMLElement).click();
  });
  await flush();
};

// React tracks a control's value itself, so the value goes through the
// native setter and then the event React listens for.
const setValue = async (node: HTMLInputElement | HTMLSelectElement, value: string) => {
  const proto = node instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
  await act(async () => {
    Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(node, value);
    node.dispatchEvent(new Event(node instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
  });
  await flush();
};

// The snapshot is container.innerHTML with a line break put between adjacent
// tags ("><"); text never holds that pair, because the serializer escapes it.
const snapshot = (name: string) =>
  expect(container.innerHTML.replace(/></g, '>\n<') + '\n').toMatchFileSnapshot(
    `./__snapshots__/ProjectSettings.${name}.html`
  );

// innerHTML does not carry a <select>'s current choice or a box's checked
// state (React sets the properties), so the form controls are read beside it.
const controls = () =>
  Array.from(container.querySelectorAll<HTMLInputElement | HTMLSelectElement>('input, select')).map((node) => {
    if (node instanceof HTMLSelectElement) return `select=${node.value}`;
    if (node.type === 'checkbox' || node.type === 'radio') return `${node.type}=${node.checked}`;
    return `${node.type}=${node.value}`;
  });

// TABS in ProjectSettings.tsx, in order: the key ?tab= takes and the label.
const TABS = [
  ['general', 'General'],
  ['members', 'Access'],
  ['repos', 'Repositories'],
  ['agents', 'Agents'],
  ['attributes', 'Attributes'],
  ['quality', 'Quality rules'],
  ['danger', 'Danger Zone'],
] as const;
type TabKey = (typeof TABS)[number][0];

const tabButton = (label: string) => byText<HTMLButtonElement>('[role="tablist"] > button', label);

// The tablist as assistive technology meets it today: one role="tablist" strip
// of plain buttons. No button has role="tab", aria-selected, aria-controls,
// an id or a type, and no panel has role="tabpanel"; the open tab is marked
// only by its style (an accent underline and weight 600).
const tablist = () => {
  const strips = Array.from(container.querySelectorAll('[role="tablist"]'));
  return {
    strips: strips.map((s) => `${s.tagName.toLowerCase()}.${s.className}`),
    children: strips.flatMap((s) =>
      Array.from(s.children).map((b) =>
        [
          b.tagName.toLowerCase(),
          (b.textContent ?? '').trim(),
          ...['role', 'aria-selected', 'aria-controls', 'id', 'type', 'tabindex'].map(
            (attr) => `${attr}=${b.getAttribute(attr)}`
          ),
        ].join(' ')
      )
    ),
    selected: strips.flatMap((s) =>
      Array.from(s.children as HTMLCollectionOf<HTMLElement>)
        .filter((b) => b.style.fontWeight === '600')
        .map((b) => b.textContent)
    ),
    ariaElsewhere: container.querySelectorAll('[role="tab"], [role="tabpanel"], [aria-selected], [aria-controls]').length,
  };
};

const expectTablist = (selectedLabel: string) =>
  expect(tablist()).toEqual({
    strips: ['div.tab-strip'],
    children: TABS.map(
      ([, label]) => `button ${label} role=null aria-selected=null aria-controls=null id=null type=null tabindex=null`
    ),
    selected: [selectedLabel],
    ariaElsewhere: 0,
  });

beforeEach(() => {
  recorder.calls = [];
  location = '';
  navigationType = '';
  // Share links print through toLocaleDateString, which follows the
  // machine's zone and locale; ISO text keeps the snapshot the same
  // everywhere. The clock is frozen too (whether a link has expired is
  // judged against now); timers stay real.
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-06-01T12:00:00Z'));
  vi.spyOn(Date.prototype, 'toLocaleString').mockImplementation(function (this: Date) {
    return this.toISOString();
  });
  vi.spyOn(Date.prototype, 'toLocaleDateString').mockImplementation(function (this: Date) {
    return this.toISOString().slice(0, 10);
  });
  vi.spyOn(Date.prototype, 'toLocaleTimeString').mockImplementation(function (this: Date) {
    return this.toISOString().slice(11, 19);
  });
  useAppStore.setState(
    {
      ...initialStore,
      projectId: 'p1',
      activeOrgId: 'o1',
      currentUser: {
        id: 'u1',
        email: 'dana@example.com',
        name: 'Dana Reyes',
        avatar_url: '',
        auth_provider: 'local',
        is_admin: false,
        email_verified: true,
        created_at: '2026-01-01T00:00:00Z',
      },
      features: {
        channel: 'nightly',
        stable_release: '',
        preview: false,
        features: { 'flow-down': true, 'artifact-owners': true, 'share-links': true },
      },
    },
    true
  );
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  useAppStore.setState(initialStore, true);
  vi.restoreAllMocks();
  vi.useRealTimers();
});

// The eight loads of the mount effect, in the order it starts them, whatever
// tab is open: members, repositories, team access, share links (made even
// with share links gated off), the workspace's teams, the project, attribute
// definitions and artifact types. Loading the project goes on, one call
// after the other, to the workspace's projects and the project's parties.
const LOAD = [
  'membersAPI.list("p1")',
  'repoConnectionsAPI.list("p1")',
  'projectTeamAccessAPI.list("p1")',
  'shareLinkAPI.list("p1")',
  'orgTeamsAPI.list("o1")',
  'projectAPI.get("p1")',
  'attributeDefinitionAPI.listByProject("p1")',
  'metaAPI.artifactTypes()',
  'projectAPI.list()',
  'projectAPI.parties("p1")',
];
// The quality tab's editor loads its rules each time it mounts, that is,
// each time the tab is opened.
const QUALITY = 'qualityRulesAPI.forProject("p1")';

// What each tab's form controls hold once loaded, in document order.
const CONTROLS: Record<TabKey, string[]> = {
  // The parent picker (p1 refines p0), then the new-party name and note.
  general: ['select=p0', 'text=', 'text='],
  // Four member roles, the new member's email and role; the team grant's
  // role, the team to grant and its role; the share link's access, label
  // and expiry.
  members: [
    'select=owner',
    'select=editor',
    'select=reviewer',
    'select=viewer',
    'email=',
    'select=editor',
    'select=editor',
    'select=',
    'select=editor',
    'select=public',
    'text=',
    'date=',
  ],
  // Each repository's "your local path" draft.
  repos: ['text=/home/dana/main-app', 'text='],
  // p1 runs on the workspace API key.
  agents: ['radio=false', 'radio=true'],
  // The new attribute's key, label, type, applies-to and required box.
  attributes: ['text=', 'text=', 'select=text', 'select=', 'checkbox=false'],
  // The convention, then weak wording (the project's error), passive voice
  // (the workspace's info) and not testable (the default warning).
  quality: ['radio=true', 'radio=false', 'select=error', 'select=info', 'select=warning'],
  danger: [],
};

describe('ProjectSettings characterization (S16c)', () => {
  it('by click: every tab in TABS order, ?tab= replaced, nothing reloaded', async () => {
    // A page before settings, so the test can tell what Back reaches.
    await mount(['/projects', '/projects/p1/settings']);
    expect(recorder.calls).toEqual(LOAD);
    expect(location).toBe('/projects/p1/settings');
    expect(navigationType).toBe('POP');
    expectTablist('General');

    const calls = [...LOAD];
    for (const [key, label] of TABS) {
      await click(tabButton(label));
      if (key === 'quality') calls.push(QUALITY);
      expect(recorder.calls, key).toEqual(calls);
      expect(location, key).toBe(`/projects/p1/settings?tab=${key}`);
      expect(navigationType, key).toBe('REPLACE');
      expectTablist(label);
      expect(controls(), key).toEqual(CONTROLS[key]);
      await snapshot(`${key}.click`);
    }

    // Seven switches replaced one history entry: Back leaves settings.
    await act(async () => {
      navigate(-1);
    });
    await flush();
    expect(location).toBe('/projects');
    expect(container.textContent).toBe('Project list');
    expect(recorder.calls).toEqual(calls);
  });

  it.each(TABS)('by deep link: ?tab=%s', async (key, label) => {
    await mount([`/projects/p1/settings?tab=${key}`]);
    // The editor is a child, and children's effects run first, so on a deep
    // link its load comes before the page's own.
    expect(recorder.calls).toEqual(key === 'quality' ? [QUALITY, ...LOAD] : LOAD);
    expect(location).toBe(`/projects/p1/settings?tab=${key}`);
    expect(navigationType).toBe('POP');
    expectTablist(label);
    expect(controls()).toEqual(CONTROLS[key]);
    await snapshot(`${key}.link`);
  });

  it('an unknown ?tab= shows General and stays; a switch keeps the other parameters', async () => {
    await mount(['/projects/p1/settings?tab=bogus&from=menu']);
    expect(location).toBe('/projects/p1/settings?tab=bogus&from=menu');
    expectTablist('General');
    expect(controls()).toEqual(CONTROLS.general);
    // Exactly what ?tab=general renders.
    await snapshot('general.link');

    await click(tabButton('Access'));
    expect(location).toBe('/projects/p1/settings?tab=members&from=menu');
    expect(navigationType).toBe('REPLACE');
    expect(recorder.calls).toEqual(LOAD);
  });

  it('a tab switch keeps unsaved input, except the quality editor, and reloads nothing else', async () => {
    await mount(['/projects/p1/settings?tab=general']);
    const inputs = (selector: string) => Array.from(container.querySelectorAll<HTMLInputElement>(selector));
    const selects = () => Array.from(container.querySelectorAll<HTMLSelectElement>('select'));

    // General: a party not yet added.
    await setValue(container.querySelector('input[placeholder^="Party name"]')!, 'Seal supplier');
    await setValue(container.querySelector('input[placeholder="Note (optional)"]')!, 'Tier 3');

    // Access: a member, a team grant and a share link, none submitted.
    await click(tabButton('Access'));
    await setValue(container.querySelector('input[type="email"]')!, 'new@example.com');
    await setValue(selects()[4], 'reviewer');
    await setValue(selects()[6], 't2');
    await setValue(selects()[7], 'viewer');
    await setValue(selects()[8], 'reviewer');
    await setValue(container.querySelector('input[placeholder="Who this link is for"]')!, 'Design review');
    await setValue(container.querySelector('input[type="date"]')!, '2026-12-31');

    // Repositories: the connect form, open and filled, and a path draft.
    await click(tabButton('Repositories'));
    await click(byText('button', '+ Connect repository'));
    await setValue(container.querySelector('input[placeholder="e.g. main-app"]')!, 'firmware');
    await setValue(
      container.querySelector('input[placeholder="https://github.com/org/repo.git"]')!,
      'https://github.com/acme/firmware.git'
    );
    await setValue(container.querySelector('input[placeholder="main"]')!, 'develop');
    await setValue(inputs('input[placeholder="where this repo lives on YOUR machine"]')[1], '/home/dana/docs');

    // Attributes: an enum attribute for test cases, required.
    await click(tabButton('Attributes'));
    await setValue(container.querySelector('#attr-key')!, 'risk');
    await setValue(container.querySelector('#attr-label')!, 'Risk');
    await setValue(container.querySelector('#attr-type')!, 'enum');
    await setValue(container.querySelector('#attr-enum')!, 'low, high');
    await setValue(container.querySelector('#attr-applies')!, 'test_case');
    await click(container.querySelector('#attr-required')!);

    // Quality rules: weak wording turned off, not saved.
    await click(tabButton('Quality rules'));
    await setValue(selects()[0], 'off');
    expect(controls()).toEqual(['radio=true', 'radio=false', 'select=off', 'select=info', 'select=warning']);

    // Away, then back to each tab in turn.
    await click(tabButton('Danger Zone'));
    await click(tabButton('Agents'));

    await click(tabButton('General'));
    expect(controls()).toEqual(['select=p0', 'text=Seal supplier', 'text=Tier 3']);

    await click(tabButton('Access'));
    expect(controls()).toEqual([
      'select=owner',
      'select=editor',
      'select=reviewer',
      'select=viewer',
      'email=new@example.com',
      'select=reviewer',
      'select=editor',
      'select=t2',
      'select=viewer',
      'select=reviewer',
      'text=Design review',
      'date=2026-12-31',
    ]);

    await click(tabButton('Repositories'));
    expect(byText('button', 'Cancel')).toBeTruthy();
    expect(controls()).toEqual([
      'text=firmware',
      'text=https://github.com/acme/firmware.git',
      'text=develop',
      'text=/home/dana/main-app',
      'text=/home/dana/docs',
    ]);

    await click(tabButton('Attributes'));
    expect(controls()).toEqual([
      'text=risk',
      'text=Risk',
      'select=enum',
      'text=low, high',
      'select=test_case',
      'checkbox=true',
    ]);

    // The editor keeps its draft itself, so leaving the tab drops it: it
    // mounts afresh, loads the rules again and shows them as saved.
    await click(tabButton('Quality rules'));
    expect(controls()).toEqual(CONTROLS.quality);

    expect(recorder.calls).toEqual([...LOAD, QUALITY, QUALITY]);
    expect(location).toBe('/projects/p1/settings?tab=quality');
  });

  it('stable channel: the gated sections stay hidden, the loads do not change', async () => {
    useAppStore.setState({
      features: { channel: 'stable', stable_release: '1.0.0', preview: false, features: {} },
    });
    await mount(['/projects/p1/settings']);
    expect(recorder.calls).toEqual(LOAD);
    expectTablist('General');
    expect(controls()).toEqual([]);
    await snapshot('general.stable');

    await click(tabButton('Access'));
    expect(container.querySelector('h3')?.textContent).toBe('People');
    expect(Array.from(container.querySelectorAll('h3')).map((h) => h.textContent)).toEqual([
      'People',
      'Add member',
      'Teams',
    ]);
    expect(recorder.calls).toEqual(LOAD);
  });
});
