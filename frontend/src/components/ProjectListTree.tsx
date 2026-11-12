// The Home page's projects as a hierarchy: a project whose parent is in the
// list sits under it, indented, and a parent folds its children away.
import React, { useCallback, useState } from 'react';
import type { Project } from '../api/client';

/** One project of the list as drawn: how deep it sits and how many projects are under it. */
export interface ProjectRow {
  project: Project;
  depth: number;
  /** Every project beneath it, at any depth. */
  below: number;
  folded: boolean;
}

/**
 * The projects in tree order: each parent followed by its children, in the
 * order the list gave them. A project whose parent is not in the list (it is
 * in another workspace, or the reader cannot see it) is a root, and so is
 * every project of a cycle, so none is ever left out. The children of a
 * folded project are not returned.
 */
export const projectRows = (projects: Project[], folded: ReadonlySet<string> = new Set()): ProjectRow[] => {
  const byId = new Map(projects.map((p) => [p.id, p]));
  const children = new Map<string, Project[]>();
  const roots: Project[] = [];
  for (const p of projects) {
    const parent = p.parent_project_id;
    if (parent && parent !== p.id && byId.has(parent)) children.set(parent, [...(children.get(parent) || []), p]);
    else roots.push(p);
  }
  const out: ProjectRow[] = [];
  const seen = new Set<string>();
  const count = (id: string, path: Set<string>): number =>
    (children.get(id) || []).reduce((n, c) => (path.has(c.id) ? n : n + 1 + count(c.id, new Set(path).add(c.id))), 0);
  const visit = (p: Project, depth: number) => {
    if (seen.has(p.id)) return;
    seen.add(p.id);
    const isFolded = folded.has(p.id);
    out.push({ project: p, depth, below: count(p.id, new Set([p.id])), folded: isFolded });
    if (!isFolded) for (const c of children.get(p.id) || []) visit(c, depth + 1);
  };
  roots.forEach((p) => visit(p, 0));
  // What only a cycle reaches has no root above it: list each as one.
  const hidden = new Set<string>();
  const hide = (id: string) => {
    for (const c of children.get(id) || []) {
      if (!hidden.has(c.id)) {
        hidden.add(c.id);
        hide(c.id);
      }
    }
  };
  out.filter((r) => r.folded).forEach((r) => hide(r.project.id));
  for (const p of projects) if (!seen.has(p.id) && !hidden.has(p.id)) visit(p, 0);
  return out;
};

const STORAGE_KEY = 'openv-home-folded';

const load = (): Set<string> => {
  try {
    const raw = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
    return new Set(Array.isArray(raw) ? raw.filter((x) => typeof x === 'string') : []);
  } catch {
    return new Set();
  }
};

/** The projects folded on the Home page, remembered in this browser. */
export const useFoldedProjects = (): [Set<string>, (id: string) => void] => {
  const [folded, setFolded] = useState(load);
  const toggle = useCallback((id: string) => {
    setFolded((prev) => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      try {
        localStorage.setItem(STORAGE_KEY, JSON.stringify([...next]));
      } catch {
        // Folding still works for this visit.
      }
      return next;
    });
  }, []);
  return [folded, toggle];
};

/** The space before a project's name: the fold button of a parent, or its width. The row itself is indented by the project's depth. */
export const ProjectFold: React.FC<{ row: ProjectRow; onToggle: (id: string) => void }> = ({ row, onToggle }) => (
  <span style={{ display: 'inline-flex', alignItems: 'center', flex: 'none' }}>
    {row.below > 0 ? (
      <button
        type="button"
        className="ov-tap"
        aria-expanded={!row.folded}
        aria-label={`${row.folded ? 'Unfold' : 'Fold'} ${row.project.name}`}
        title={row.folded ? `Show the ${row.below} project${row.below === 1 ? '' : 's'} under it` : 'Hide the projects under it'}
        onClick={(e) => {
          e.stopPropagation();
          onToggle(row.project.id);
        }}
        style={{ width: 24, height: 24, padding: 0, border: 'none', background: 'none', color: 'var(--text-muted)', fontSize: 12, cursor: 'pointer' }}
      >
        {row.folded ? '▶' : '▼'}
      </button>
    ) : (
      <span aria-hidden style={{ width: 24 }} />
    )}
  </span>
);
