import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { projectAPI, type Project } from '../api/client';
import { NewProjectParent, placeUnder } from '../components/ProjectListTree';
import { ProjectActions } from '../components/ProjectListActions';
import { ProductOverviewParent, parentCandidates } from './ProductOverviewParent';
import { useAppStore } from '../state/store';

// Where a project sits among the workspace's projects: its parent is set on
// its Overview, a child is started from the parent's menu on the projects
// page, and the form that creates a project takes its parent.

vi.mock('../api/client', async (importOriginal) => mockApi(await importOriginal<typeof import('../api/client')>()));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

const project = (id: string, parent = '', org = 'o1'): Project => ({ id, name: id.toUpperCase(), parent_project_id: parent, org_id: org }) as unknown as Project;
const projects = [project('system'), project('sub', 'system'), project('part', 'sub'), project('other'), project('elsewhere', '', 'o2')];

const gates = (flowDown: boolean) =>
  useAppStore.setState({ features: { channel: 'nightly' as const, stable_release: '', preview: false, features: { 'flow-down': flowDown } } } as never);

const mount = async (ui: React.ReactElement) => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => root.render(<MemoryRouter>{ui}</MemoryRouter>));
  await act(async () => {});
};

afterEach(() => {
  vi.clearAllMocks();
  // The pure cases mount nothing.
  if (!container?.isConnected) return;
  act(() => root.unmount());
  container.remove();
});

const choose = (el: HTMLSelectElement, value: string) =>
  act(async () => {
    Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!.call(el, value);
    el.dispatchEvent(new Event('change', { bubbles: true }));
  });

describe('parentCandidates', () => {
  it('leaves out the project, everything under it and other workspaces', () => {
    expect(parentCandidates(projects, projects[1]).map((p) => p.id)).toEqual(['system', 'other']);
    expect(parentCandidates(projects, projects[0]).map((p) => p.id)).toEqual(['other']);
  });
});

describe('ProductOverviewParent', () => {
  beforeEach(() => {
    vi.mocked(projectAPI.list).mockResolvedValue({ data: projects } as never);
    vi.mocked(projectAPI.update).mockResolvedValue({ data: project('sub', 'other') } as never);
  });

  it('shows what the project is part of and its child projects, and sets the parent', async () => {
    gates(true);
    await mount(<ProductOverviewParent projectId="sub" />);
    const select = container.querySelector('#parent-project') as HTMLSelectElement;
    expect(select.value).toBe('system');
    expect(Array.from(select.options).map((o) => o.value)).toEqual(['', 'system', 'other']);
    expect(Array.from(container.querySelectorAll('a')).map((a) => [a.textContent, a.getAttribute('href')])).toEqual([['PART', '/projects/part']]);
    await choose(select, 'other');
    expect(projectAPI.update).toHaveBeenCalledWith('sub', { parent_project_id: 'other' });
    expect((container.querySelector('#parent-project') as HTMLSelectElement).value).toBe('other');
  });

  it('is not shown where projects have no parents', async () => {
    gates(false);
    await mount(<ProductOverviewParent projectId="sub" />);
    expect(container.querySelector('#parent-project')).toBeNull();
    expect(projectAPI.list).not.toHaveBeenCalled();
  });
});

describe('a new project at its level', () => {
  it('offers New child project first in a project\'s menu', async () => {
    const onNewChild = vi.fn();
    await mount(<ProjectActions name="System" menu onTemplate={() => {}} onEdit={() => {}} onDownload={() => {}} onDelete={() => {}} onNewChild={onNewChild} />);
    await act(async () => {
      container.querySelector('button[aria-label="Actions for System"]')!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    const items = Array.from(container.querySelectorAll('[role="menuitem"]'));
    expect(items.map((b) => b.textContent)).toEqual(['New child project', 'Edit project', 'Download', 'Save as template', 'Delete project']);
    await act(async () => {
      items[0].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(onNewChild).toHaveBeenCalledTimes(1);
  });

  it('lists the projects as a tree in the form\'s Parent project field', async () => {
    const onChange = vi.fn();
    await mount(<NewProjectParent projects={projects.slice(0, 4)} value="sub" onChange={onChange} />);
    const select = container.querySelector('#new-project-parent') as HTMLSelectElement;
    expect(select.value).toBe('sub');
    expect(Array.from(select.options).map((o) => o.textContent)).toEqual(['None (top-level project)', 'SYSTEM', '  SUB', '    PART', 'OTHER']);
    await choose(select, '');
    expect(onChange).toHaveBeenCalledWith('');
  });

  it('files a created project under the parent chosen, and leaves a top-level one alone', async () => {
    vi.mocked(projectAPI.update).mockResolvedValue({ data: project('new', 'system') } as never);
    expect((await placeUnder(project('new'), 'system')).parent_project_id).toBe('system');
    expect(projectAPI.update).toHaveBeenCalledWith('new', { parent_project_id: 'system' });
    vi.mocked(projectAPI.update).mockClear();
    expect(await placeUnder(project('top'), '')).toEqual(project('top'));
    expect(projectAPI.update).not.toHaveBeenCalled();
  });
});
