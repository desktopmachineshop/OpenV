import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import {
  Artifact,
  ProjectMember,
  WorkItem,
  artifactAPI,
  membersAPI,
  workItemsAPI,
} from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { WorkItemDrawer } from '../components/kanban/WorkItemDrawer';
import { attentionRank, columnFor, isOpenColumn } from '../components/kanban/columns';
import { ErrorBanner, SegmentedControl } from '../components/ui';
import { useViewport } from '../hooks/useViewport';
import { useAppStore } from '../state/store';

/**
 * The gate this page and the note's "Add to-do" control sit behind, until
 * every supported stable release carries them.
 */
export const TODO_LIST_FEATURE = 'todo-list';

/** A person (or agent, or team, or nobody) and what they owe. */
interface Group {
  key: string;
  name: string;
  /** Sorts people above the catch-all groups regardless of name. */
  rank: number;
  items: WorkItem[];
}

const UNASSIGNED = '__unassigned__';
const NON_PEOPLE = '__non_people__';

const statusChipStyle = (color: string): React.CSSProperties => ({
  display: 'inline-block',
  padding: '1px 8px',
  borderRadius: 10,
  background: 'var(--surface-alt, var(--surface))',
  // The border carries the column's colour; the label carries its meaning.
  // Colour alone never says what a status is — see the V&V chips.
  border: `1px solid ${color}`,
  color: 'var(--text-muted)',
  fontSize: 12,
  fontWeight: 600,
  whiteSpace: 'nowrap',
});

const cardStyle: React.CSSProperties = {
  background: 'var(--surface)',
  border: '1px solid var(--border)',
  borderRadius: 6,
  padding: 16,
  marginBottom: 24,
};

/** Overdue is only meaningful for work that is not finished. */
const isOverdue = (item: WorkItem): boolean => {
  if (!item.due_date || !isOpenColumn(item.column)) return false;
  const due = new Date(item.due_date);
  if (Number.isNaN(due.getTime())) return false;
  return due.getTime() < Date.now();
};

const formatDue = (value: string): string => {
  const due = new Date(value);
  if (Number.isNaN(due.getTime())) return value;
  return due.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
};

/**
 * Groups work items by the person they are assigned to.
 *
 * Exported for its own tests, and pure for the same reason: the grouping is
 * the whole point of the page, and it is much easier to be sure about it
 * here than through a rendered tree.
 *
 * Every project member gets a group even when they owe nothing — "who owes
 * what" is only answerable if the people with a clean slate are visible
 * too. Items assigned to an agent or a team are gathered into one group
 * rather than dropped, and so is anything unassigned.
 */
export const groupByAssignee = (
  items: WorkItem[],
  members: ProjectMember[]
): Group[] => {
  const nameFor = (m: ProjectMember) => m.user_name?.trim() || m.user_email || 'Unknown member';
  const groups = new Map<string, Group>();

  members.forEach((m) => {
    groups.set(m.user_id, { key: m.user_id, name: nameFor(m), rank: 0, items: [] });
  });

  const ensure = (key: string, name: string, rank: number): Group => {
    let g = groups.get(key);
    if (!g) {
      g = { key, name, rank, items: [] };
      groups.set(key, g);
    }
    return g;
  };

  items.forEach((item) => {
    if (item.assignee_type === 'user' && item.assignee_id) {
      // Someone who has left the project still owns what they were given:
      // showing it under their id beats hiding it.
      ensure(item.assignee_id, 'Former member', 1).items.push(item);
    } else if (item.assignee_id) {
      ensure(NON_PEOPLE, 'Agents and teams', 2).items.push(item);
    } else {
      ensure(UNASSIGNED, 'Unassigned', 3).items.push(item);
    }
  });

  groups.forEach((g) => {
    g.items.sort((a, b) => {
      // Due first, soonest first, then by how live the work is so what is
      // being worked on sits above the backlog.
      const aDue = a.due_date && isOpenColumn(a.column) ? Date.parse(a.due_date) : NaN;
      const bDue = b.due_date && isOpenColumn(b.column) ? Date.parse(b.due_date) : NaN;
      const aHas = !Number.isNaN(aDue);
      const bHas = !Number.isNaN(bDue);
      if (aHas !== bHas) return aHas ? -1 : 1;
      if (aHas && bHas && aDue !== bDue) return aDue - bDue;
      const ao = attentionRank(a.column);
      const bo = attentionRank(b.column);
      if (ao !== bo) return ao - bo;
      return a.title.localeCompare(b.title);
    });
  });

  return Array.from(groups.values()).sort(
    (a, b) => a.rank - b.rank || a.name.localeCompare(b.name)
  );
};

/**
 * TodoList is the "who owes what" page: every to-do in the project under the
 * person it belongs to.
 *
 * It is a view over the board rather than a second list of work — a to-do
 * and a card are the same row, so moving one on the board changes what this
 * page says, and a to-do raised from a note appears here without anyone
 * filing it twice.
 */
export const TodoList: React.FC = () => {
  const { projectId } = useParams<{ projectId: string }>();
  const currentUser = useAppStore((s) => s.currentUser);
  const { isPhone } = useViewport();

  const [items, setItems] = useState<WorkItem[]>([]);
  const [members, setMembers] = useState<ProjectMember[]>([]);
  const [artifactMap, setArtifactMap] = useState<Record<string, Artifact>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [scope, setScope] = useState<'everyone' | 'mine'>('everyone');
  const [showDone, setShowDone] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  // ?item=<id> is how a note's smart link lands on the to-do it raised,
  // rather than on the top of a page the reader then has to search.
  const [searchParams, setSearchParams] = useSearchParams();
  const deepLinked = searchParams.get('item');

  const load = useCallback(async () => {
    if (!projectId) return;
    setLoading(true);
    setError('');
    try {
      const [itemsRes, membersRes] = await Promise.all([
        workItemsAPI.list(projectId),
        membersAPI.list(projectId),
      ]);
      setItems(itemsRes.data || []);
      setMembers(membersRes.data || []);
    } catch (err) {
      setError(apiErrorMessage(err, 'Failed to load to-dos'));
    } finally {
      setLoading(false);
    }
  }, [projectId]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    if (deepLinked) setSelectedId(deepLinked);
  }, [deepLinked]);

  // Drop the parameter once the drawer is closed, so a reload or a shared
  // URL does not keep reopening something the reader has dismissed.
  const closeDrawer = useCallback(() => {
    setSelectedId(null);
    if (deepLinked) {
      const next = new URLSearchParams(searchParams);
      next.delete('item');
      setSearchParams(next, { replace: true });
    }
  }, [deepLinked, searchParams, setSearchParams]);

  // Artifact titles for the rows that cite one. Failing to load them costs
  // the row a label, not the page.
  useEffect(() => {
    if (!projectId) return;
    let cancelled = false;
    artifactAPI
      .list(projectId)
      .then((res) => {
        if (cancelled) return;
        const map: Record<string, Artifact> = {};
        (res.data || []).forEach((a) => {
          map[a.id] = a;
        });
        setArtifactMap(map);
      })
      .catch(() => {
        /* row labels only */
      });
    return () => {
      cancelled = true;
    };
  }, [projectId]);

  const visible = useMemo(() => {
    let list = items;
    if (!showDone) list = list.filter((i) => isOpenColumn(i.column));
    if (scope === 'mine' && currentUser) {
      list = list.filter(
        (i) => i.assignee_type === 'user' && i.assignee_id === currentUser.id
      );
    }
    // A to-do arrived at from a note stays in the list behind its drawer
    // even when the filters would hide it — it is done, say, or belongs to
    // someone else. Closing the drawer onto a page that does not contain
    // what you just followed a link to reads as the link being broken.
    if (deepLinked && !list.some((i) => i.id === deepLinked)) {
      const item = items.find((i) => i.id === deepLinked);
      if (item) list = [...list, item];
    }
    return list;
  }, [items, showDone, scope, currentUser, deepLinked]);

  const groups = useMemo(() => {
    const all = groupByAssignee(visible, members);
    // "Just mine" is one person's list; the empty groups that make the
    // everyone view readable would only be noise there.
    if (scope === 'mine') return all.filter((g) => g.items.length > 0);
    return all;
  }, [visible, members, scope]);

  const openCount = useMemo(
    () => items.filter((i) => isOpenColumn(i.column)).length,
    [items]
  );

  if (!projectId) return null;

  return (
    <div style={{ padding: isPhone ? '0 12px 24px' : '0 24px 24px' }}>
      <div
        style={{
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 12,
          marginBottom: 16,
        }}
      >
        <div>
          <h2 style={{ margin: 0, color: 'var(--text)' }}>To-dos</h2>
          <div style={{ color: 'var(--text-muted)', fontSize: 13, marginTop: 4 }}>
            {openCount} open across the project. The same work as the{' '}
            <Link to={`/projects/${projectId}/board`}>board</Link>, arranged by who owes it.
          </div>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
          <SegmentedControl
            value={scope}
            onChange={(v) => setScope(v as 'everyone' | 'mine')}
            options={[
              { value: 'everyone', label: 'Everyone' },
              { value: 'mine', label: 'Just mine' },
            ]}
          />
          <label
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 6,
              color: 'var(--text-muted)',
              fontSize: 13,
              minHeight: 44,
            }}
          >
            <input
              type="checkbox"
              checked={showDone}
              onChange={(e) => setShowDone(e.target.checked)}
            />
            Show done
          </label>
        </div>
      </div>

      {error && <ErrorBanner message={error} onDismiss={() => setError('')} />}

      {loading ? (
        <div style={{ color: 'var(--text-muted)' }}>Loading to-dos…</div>
      ) : groups.length === 0 ? (
        <div style={{ ...cardStyle, color: 'var(--text-muted)' }}>
          {scope === 'mine'
            ? 'Nothing is assigned to you.'
            : 'This project has no members yet.'}
        </div>
      ) : (
        groups.map((group) => (
          <section key={group.key} style={cardStyle}>
            <div
              style={{
                display: 'flex',
                alignItems: 'baseline',
                gap: 8,
                marginBottom: group.items.length ? 12 : 0,
              }}
            >
              <h3 style={{ margin: 0, fontSize: 16, color: 'var(--text)' }}>{group.name}</h3>
              <span style={{ color: 'var(--text-muted)', fontSize: 13 }}>
                {group.items.length === 0
                  ? 'nothing open'
                  : `${group.items.length} ${group.items.length === 1 ? 'item' : 'items'}`}
              </span>
            </div>

            {group.items.map((item) => {
              const col = columnFor(item.column);
              const overdue = isOverdue(item);
              return (
                <button
                  key={item.id}
                  type="button"
                  onClick={() => setSelectedId(item.id)}
                  style={{
                    display: 'flex',
                    width: '100%',
                    alignItems: 'center',
                    flexWrap: 'wrap',
                    gap: 10,
                    padding: '10px 8px',
                    minHeight: 44,
                    background: 'none',
                    border: 'none',
                    borderTop: '1px solid var(--border)',
                    textAlign: 'left',
                    cursor: 'pointer',
                    color: 'var(--text)',
                  }}
                >
                  <span style={statusChipStyle(col.color)}>{col.label}</span>
                  <span style={{ flex: '1 1 200px', minWidth: 0 }}>{item.title}</span>
                  {item.source_chatter_id && (
                    <span
                      title="Raised from a note"
                      style={{ color: 'var(--text-muted)', fontSize: 12 }}
                    >
                      from a note
                    </span>
                  )}
                  {item.artifact_ids.slice(0, 1).map((id) => (
                    <span key={id} style={{ color: 'var(--text-muted)', fontSize: 12 }}>
                      {artifactMap[id]?.ref || 'linked artifact'}
                    </span>
                  ))}
                  {item.due_date && (
                    <span
                      style={{
                        fontSize: 12,
                        fontWeight: overdue ? 700 : 400,
                        color: overdue ? 'var(--danger)' : 'var(--text-muted)',
                      }}
                    >
                      {overdue ? 'Overdue ' : 'Due '}
                      {formatDue(item.due_date)}
                    </span>
                  )}
                </button>
              );
            })}
          </section>
        ))
      )}

      {selectedId && (
        <WorkItemDrawer
          workItemId={selectedId}
          projectId={projectId}
          artifactMap={artifactMap}
          onClose={closeDrawer}
          onChanged={load}
          onDeleted={() => {
            closeDrawer();
            load();
          }}
        />
      )}
    </div>
  );
};
