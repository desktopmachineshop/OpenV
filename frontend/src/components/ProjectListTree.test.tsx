import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import type { Project } from '../api/client';
import { ProjectFold, projectRows, useFoldedProjects } from './ProjectListTree';

// The Home page's projects as a hierarchy: children sit under their parent,
// indented, a parent folds them away, and no project is ever left out.

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const project = (id: string, parent = ''): Project => ({ id, name: id.toUpperCase(), parent_project_id: parent }) as unknown as Project;

const shape = (rows: ReturnType<typeof projectRows>) => rows.map((r) => `${'  '.repeat(r.depth)}${r.project.id}${r.below ? ` +${r.below}` : ''}${r.folded ? ' folded' : ''}`);

describe('projectRows', () => {
  const projects = [project('sub', 'sys'), project('other'), project('sys'), project('part', 'sub'), project('sub2', 'sys'), project('orphan', 'gone')];

  it('lists each parent with its children under it, in the list order', () => {
    expect(shape(projectRows(projects))).toEqual(['other', 'sys +3', '  sub +1', '    part', '  sub2', 'orphan']);
  });

  it('leaves out what is under a folded project, and says how many', () => {
    expect(shape(projectRows(projects, new Set(['sys'])))).toEqual(['other', 'sys +3 folded', 'orphan']);
    expect(shape(projectRows(projects, new Set(['sub'])))).toEqual(['other', 'sys +3', '  sub +1 folded', '  sub2', 'orphan']);
  });

  it('keeps every project of a cycle, and one that is its own parent', () => {
    const rows = projectRows([project('a', 'b'), project('b', 'a'), project('self', 'self')]);
    expect(rows.map((r) => r.project.id).sort()).toEqual(['a', 'b', 'self']);
  });
});

describe('ProjectFold', () => {
  let container: HTMLDivElement;
  let root: Root;

  const Harness: React.FC<{ onCard: () => void }> = ({ onCard }) => {
    const [folded, toggle] = useFoldedProjects();
    return (
      <div>
        {projectRows([project('sys'), project('sub', 'sys')], folded).map((row) => (
          <div key={row.project.id} data-row={row.project.id} onClick={onCard}>
            <ProjectFold row={row} onToggle={toggle} />
          </div>
        ))}
      </div>
    );
  };

  beforeEach(() => localStorage.clear());
  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  it('folds a parent without opening it, and remembers the fold', async () => {
    const onCard = vi.fn();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    await act(async () => root.render(<Harness onCard={onCard} />));
    expect(container.querySelector('[data-row="sub"] button')).toBeNull();
    const fold = container.querySelector('[data-row="sys"] button')!;
    expect(fold.getAttribute('aria-label')).toBe('Fold SYS');
    await act(async () => {
      fold.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelector('[data-row="sub"]')).toBeNull();
    expect(container.querySelector('[data-row="sys"] button')!.getAttribute('aria-label')).toBe('Unfold SYS');
    expect(onCard).not.toHaveBeenCalled();
    expect(JSON.parse(localStorage.getItem('openv-home-folded')!)).toEqual(['sys']);
  });
});
