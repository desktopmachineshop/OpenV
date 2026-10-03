import React, { act } from 'react';
import { readFileSync } from 'fs';
import { join } from 'path';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { ProjectSettings } from './ProjectSettings';
import { qualityRulesAPI, shareLinkAPI } from '../api/client';
import { DialogProvider } from '../components/ui';
import { useAppStore } from '../state/store';

// Project settings as a member meets them: which tab is open, what survives a
// tab switch, what is loaded, and how the controls are drawn (#379, bugs
// 104-107).

const QUALITY_RULES = {
  effective: { convention: 'shall', severities: { 'weak-word': 'error' } },
  workspace: { convention: 'shall', severities: {} },
  project: { severities: { 'weak-word': 'error' } },
  summary: 'shall; weak wording is an error',
  catalog: {
    conventions: ['shall', 'rfc2119'],
    rules: ['weak-word'],
    severities: ['error', 'warning', 'info', 'off'],
    defaults: { convention: 'shall', severities: { 'weak-word': 'warning' } },
    labels: { shall: 'the system shall', rfc2119: 'MUST, SHOULD, MAY', 'weak-word': 'weak or subjective wording' },
  },
};

const ok = (data: unknown) => Promise.resolve({ data });

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    membersAPI: { list: () => ok([]) },
    repoConnectionsAPI: { list: () => ok([]) },
    projectTeamAccessAPI: { list: () => ok([]) },
    shareLinkAPI: { list: () => ok([]) },
    orgTeamsAPI: { list: () => ok([]) },
    projectAPI: {
      get: () => ok({ id: 'p1', org_id: 'o1', name: 'Fuel pump', parent_project_id: '' }),
      list: () => ok([]),
      parties: () => ok({ parties: [{ name: 'Acme Pumps', default: true }, { name: 'Landing gear supplier', note: 'Tier 2' }] }),
    },
    attributeDefinitionAPI: { listByProject: () => ok([]) },
    metaAPI: { artifactTypes: () => ok([]) },
    qualityRulesAPI: { forProject: () => ok(JSON.parse(JSON.stringify(QUALITY_RULES))) },
  })
);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const initialStore = useAppStore.getState();
const features = (on: Record<string, boolean>) => ({ channel: 'nightly' as const, stable_release: '', preview: false, features: on });

let container: HTMLDivElement;
let root: Root;

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const mount = async (entry: string) => {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[entry]}>
        <DialogProvider>
          <Routes>
            <Route path="/projects/:projectId/settings" element={<ProjectSettings />} />
          </Routes>
        </DialogProvider>
      </MemoryRouter>
    );
  });
  await flush();
};

const click = async (node: Element) => {
  await act(async () => {
    (node as HTMLElement).click();
  });
  await flush();
};

const choose = async (node: HTMLSelectElement, value: string) => {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!.call(node, value);
    node.dispatchEvent(new Event('change', { bubbles: true }));
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

// The strip's entries, found by their text whatever role they carry.
const tab = (label: string) => byText<HTMLElement>('[role="tablist"] > *', label);

beforeEach(() => {
  vi.clearAllMocks();
  useAppStore.setState(
    {
      ...initialStore,
      projectId: 'p1',
      activeOrgId: 'o1',
      features: features({ 'flow-down': true, 'artifact-owners': true, 'share-links': true }),
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
});

describe('ProjectSettings', () => {
  // Bug 104: a screen reader could not tell which tab was open.
  it('exposes the tab strip as tabs, with the open one selected and labelling its panel', async () => {
    await mount('/projects/p1/settings?tab=members');

    const tabs = Array.from(container.querySelectorAll<HTMLElement>('[role="tablist"] > *'));
    expect(tabs.map((t) => t.getAttribute('role'))).toEqual(Array(7).fill('tab'));
    expect(tabs.filter((t) => t.getAttribute('aria-selected') === 'true').map((t) => t.textContent)).toEqual(['Access']);
    expect(tabs.filter((t) => t.getAttribute('aria-selected') === 'false')).toHaveLength(6);

    const panel = () => {
      const panels = container.querySelectorAll<HTMLElement>('[role="tabpanel"]');
      expect(panels).toHaveLength(1);
      return panels[0];
    };
    // Every tab names the panel it controls, and the panel is labelled by
    // the open tab.
    for (const t of tabs) {
      expect(t.id).not.toBe('');
      expect(t.getAttribute('aria-controls')).toBe(panel().id);
    }
    expect(panel().getAttribute('aria-labelledby')).toBe(tab('Access').id);
    expect(panel().textContent).toContain('Add member');

    await click(tab('Quality rules'));
    expect(tab('Quality rules').getAttribute('aria-selected')).toBe('true');
    expect(tab('Access').getAttribute('aria-selected')).toBe('false');
    expect(panel().getAttribute('aria-labelledby')).toBe(tab('Quality rules').id);
  });

  // Bug 105: the quality rules editor kept its own draft, so a tab switch
  // dropped it, and every visit loaded the rules again.
  it('keeps an unsaved quality-rules change across a tab switch and loads the rules once', async () => {
    await mount('/projects/p1/settings?tab=quality');
    const weakWording = () => container.querySelector<HTMLSelectElement>('select')!;
    expect(weakWording().value).toBe('error');

    await choose(weakWording(), 'off');
    await click(tab('General'));
    await click(tab('Quality rules'));

    expect(weakWording().value).toBe('off');
    expect(byText<HTMLButtonElement>('button', 'Save rules').disabled).toBe(false);
    expect(vi.mocked(qualityRulesAPI.forProject)).toHaveBeenCalledTimes(1);
  });

  // Bug 106: share links were requested with the feature off.
  it('requests no share links while the share-links feature is off', async () => {
    useAppStore.setState({ features: features({ 'flow-down': true, 'artifact-owners': true }) });
    await mount('/projects/p1/settings?tab=members');
    expect(vi.mocked(shareLinkAPI.list)).not.toHaveBeenCalled();
    expect(container.textContent).not.toContain('Share links');
  });

  it('loads share links once the feature is on, also when the gates arrive after the page', async () => {
    useAppStore.setState({ features: null });
    await mount('/projects/p1/settings?tab=members');
    expect(vi.mocked(shareLinkAPI.list)).not.toHaveBeenCalled();

    await act(async () => {
      useAppStore.setState({ features: features({ 'share-links': true }) });
    });
    await flush();
    expect(vi.mocked(shareLinkAPI.list)).toHaveBeenCalledTimes(1);
    expect(vi.mocked(shareLinkAPI.list)).toHaveBeenCalledWith('p1');
  });

  // Bug 107: the Reference parties buttons named classes no stylesheet
  // defines, so they drew as bare browser buttons.
  it('draws the Reference parties buttons with classes the stylesheet defines', async () => {
    await mount('/projects/p1/settings?tab=general');
    const css = readFileSync(join(__dirname, '../index.css'), 'utf8');
    const defined = (cls: string) => new RegExp(`\\.${cls}(?![\\w-])`).test(css);

    const remove = byText<HTMLButtonElement>('button', 'Remove');
    const add = byText<HTMLButtonElement>('button', 'Add party');
    for (const button of [remove, add]) {
      expect(button.classList.length, button.textContent ?? '').toBeGreaterThan(0);
      for (const cls of Array.from(button.classList)) {
        expect(defined(cls), `.${cls} on "${button.textContent}"`).toBe(true);
      }
    }
    // The primary and secondary looks the rest of settings uses.
    expect(add.className).toBe('button');
    expect(remove.className).toBe('button-secondary');
  });
});
