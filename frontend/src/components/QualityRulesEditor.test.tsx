import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { QualityRulesEditor } from './QualityRulesEditor';
import { qualityRulesAPI, QualityRules, QualityRuleSet } from '../api/client';

// The editor talks to the quality-rules endpoints; the module also builds an
// axios client at import time, so it is mocked wholesale.
jest.mock('../api/client', () => ({
  qualityRulesAPI: {
    forWorkspace: jest.fn(),
    forProject: jest.fn(),
    setForWorkspace: jest.fn(),
    setForProject: jest.fn(),
  },
}));

// jest.mock is hoisted above the imports, so the imported binding is the mock.
const api = qualityRulesAPI as jest.Mocked<typeof qualityRulesAPI>;

// React 18 requires this flag when driving createRoot through act().
(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const catalog: QualityRules['catalog'] = {
  conventions: ['shall', 'rfc2119'],
  rules: ['weak-word', 'placeholder'],
  severities: ['error', 'warning', 'info', 'off'],
  defaults: {
    convention: 'shall',
    severities: { 'weak-word': 'warning', placeholder: 'error' },
  },
  labels: {
    shall: 'shall convention',
    rfc2119: 'rfc2119 convention',
    'weak-word': 'weak or subjective wording',
    placeholder: 'TBD/TODO placeholder text',
  },
};

const payload = (workspace: QualityRuleSet | null, project: QualityRuleSet | null): QualityRules => ({
  effective: {
    convention: project?.convention || workspace?.convention || 'shall',
    severities: { ...catalog.defaults.severities, ...workspace?.severities, ...project?.severities },
  },
  workspace,
  project,
  summary: 'summary',
  catalog,
});

// The component only reads `data`, so the rest of the axios response is
// stubbed away rather than built out.
const response = (workspace: QualityRuleSet | null, project: QualityRuleSet | null) =>
  ({ data: payload(workspace, project) } as any);

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  jest.clearAllMocks();
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
});

// The editor loads its rules in an effect, so a render has to flush the
// resolved promise before the DOM is worth asserting on.
const render = async (el: React.ReactElement) => {
  await act(async () => {
    root.render(el);
  });
};

const selects = () => Array.from(container.querySelectorAll('select'));

// The rule rows render in catalog order, so index 0 is "weak-word".
const weakWordSelect = () => selects()[0];

const options = (select: HTMLSelectElement) =>
  Array.from(select.options).map((o) => o.value);

const optionLabels = (select: HTMLSelectElement) =>
  Array.from(select.options).map((o) => o.textContent?.trim());

const choose = (select: HTMLSelectElement, value: string) => {
  const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!;
  act(() => {
    setter.call(select, value);
    select.dispatchEvent(new Event('change', { bubbles: true }));
  });
};

const radios = () =>
  Array.from(container.querySelectorAll('input[type=radio]')) as HTMLInputElement[];

const overrideNotes = () =>
  Array.from(container.querySelectorAll('div'))
    .filter((d) => d.textContent?.startsWith('Workspace default is') && d.children.length === 0)
    .map((d) => d.textContent);

const buttonByText = (text: string): HTMLButtonElement => {
  const btn = Array.from(container.querySelectorAll('button')).find(
    (b) => b.textContent?.trim() === text
  );
  if (!btn) throw new Error(`Button "${text}" not found`);
  return btn;
};

describe('QualityRulesEditor severities', () => {
  it('lists each severity once and preselects the inherited one', async () => {
    api.forWorkspace.mockResolvedValue(response(null, null));
    await render(<QualityRulesEditor level="workspace" id="org-1" canEdit />);

    // No extra "Platform default (Warning)" entry: four severities, and the
    // inherited one is simply the selected one.
    expect(options(weakWordSelect())).toEqual(['error', 'warning', 'info', 'off']);
    expect(optionLabels(weakWordSelect())).toEqual(['Error', 'Warning', 'Info', 'Off']);
    expect(weakWordSelect().value).toBe('warning');
    expect(overrideNotes()).toEqual([]);
  });

  it('preselects the workspace value in a project and flags an override', async () => {
    api.forProject.mockResolvedValue(response({ severities: { 'weak-word': 'off' } }, null));
    await render(<QualityRulesEditor level="project" id="proj-1" canEdit />);

    expect(options(weakWordSelect())).toEqual(['error', 'warning', 'info', 'off']);
    expect(weakWordSelect().value).toBe('off');
    expect(overrideNotes()).toEqual([]);

    choose(weakWordSelect(), 'error');
    expect(overrideNotes()).toEqual(['Workspace default is Off']);

    // Picking the workspace value again clears the override rather than
    // storing a copy of it, so the rule keeps following the workspace.
    choose(weakWordSelect(), 'off');
    expect(overrideNotes()).toEqual([]);
  });

  it('saves only the severities that differ from the workspace', async () => {
    api.forProject.mockResolvedValue(response({ severities: { 'weak-word': 'off' } }, null));
    api.setForProject.mockResolvedValue(
      response({ severities: { 'weak-word': 'off' } }, { severities: { 'weak-word': 'error' } })
    );
    await render(<QualityRulesEditor level="project" id="proj-1" canEdit />);

    choose(weakWordSelect(), 'error');
    await act(async () => {
      buttonByText('Save rules').dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(api.setForProject).toHaveBeenCalledWith('proj-1', {
      severities: { 'weak-word': 'error' },
    });
  });
});

describe('QualityRulesEditor convention', () => {
  it('offers each convention once, preselecting the inherited one', async () => {
    api.forWorkspace.mockResolvedValue(response(null, null));
    await render(<QualityRulesEditor level="workspace" id="org-1" canEdit />);

    const checked = radios().filter((r) => r.checked);
    expect(radios()).toHaveLength(2);
    expect(checked).toHaveLength(1);
    expect(container.textContent).not.toContain('Platform default');
  });

  it('flags a project convention that differs from the workspace', async () => {
    api.forProject.mockResolvedValue(response({ convention: 'rfc2119' }, null));
    await render(<QualityRulesEditor level="project" id="proj-1" canEdit />);

    // Inherited rfc2119 is the second radio and starts selected, unflagged.
    expect(radios()[1].checked).toBe(true);
    expect(overrideNotes()).toEqual([]);

    act(() => {
      radios()[0].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(overrideNotes()).toEqual(['Workspace default is RFC 2119 — MUST / SHOULD / MAY']);
  });
});
