import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { Artifact, ProjectMember, Todo, artifactAPI, membersAPI, todosAPI } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { WorkItemDrawer } from '../components/kanban/WorkItemDrawer';
import { TodoItems, todoActions } from '../components/todos/TodoItems';
import { TodoQuickAdd } from '../components/todos/TodoQuickAdd';
import { ErrorBanner, SegmentedControl } from '../components/ui';
import { useViewport } from '../hooks/useViewport';
import { useAppStore } from '../state/store';
import { MY_TODOS_PATH } from '../components/todos/todoText';

/**
 * A project's to-do list (issue #731), the To-dos page once the todos
 * feature is on: one-paragraph jobs to tick off, kept off the board. Quick
 * add at the top, the open ones, then the completed ones collapsed.
 *
 * ?todo=<id> is where a note's chip lands: that to-do is highlighted. ?item=
 * still opens a board card in its drawer, which is where a note raised
 * before to-dos had a table, and a promoted to-do, link to.
 */
export const ProjectTodos: React.FC<{ projectId: string }> = ({ projectId }) => {
  const currentUser = useAppStore((s) => s.currentUser);
  const { isPhone } = useViewport();
  const [todos, setTodos] = useState<Todo[]>([]);
  const [members, setMembers] = useState<ProjectMember[]>([]);
  const [artifactMap, setArtifactMap] = useState<Record<string, Artifact>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [scope, setScope] = useState<'everyone' | 'mine'>('everyone');
  const [searchParams, setSearchParams] = useSearchParams();
  const highlightId = searchParams.get('todo');
  const itemId = searchParams.get('item');

  const load = useCallback(async () => {
    try {
      const [todosRes, membersRes] = await Promise.all([todosAPI.listProject(projectId), membersAPI.list(projectId)]);
      setTodos(todosRes.data || []);
      setMembers(membersRes.data || []);
      setError('');
    } catch (err) {
      setError(apiErrorMessage(err, 'Failed to load to-dos'));
    } finally {
      setLoading(false);
    }
  }, [projectId]);

  useEffect(() => {
    setLoading(true);
    void load();
  }, [load]);

  // The drawer of a board card shows its artifacts by ref; only loaded when
  // a card is opened, which is the exception on this page.
  useEffect(() => {
    if (!itemId || Object.keys(artifactMap).length) return;
    artifactAPI
      .list(projectId)
      .then((res) => {
        const map: Record<string, Artifact> = {};
        (res.data || []).forEach((a) => {
          map[a.id] = a;
        });
        setArtifactMap(map);
      })
      .catch(() => {
        /* labels only */
      });
  }, [itemId, projectId, artifactMap]);

  const openItem = useCallback(
    (id: string | null) => {
      const next = new URLSearchParams(searchParams);
      if (id) next.set('item', id);
      else next.delete('item');
      setSearchParams(next, { replace: !id });
    },
    [searchParams, setSearchParams]
  );

  const actions = useMemo(() => todoActions(load, { promote: true, onPromoted: openItem }), [load, openItem]);

  const visible = useMemo(
    () =>
      scope === 'mine' && currentUser
        ? todos.filter((t) => t.assignee_id === currentUser.id || t.id === highlightId)
        : todos,
    [todos, scope, currentUser, highlightId]
  );
  const openCount = todos.filter((t) => !t.done).length;

  const add = async (draft: { text: string; assigneeId: string | null; dueDate: string | null }) => {
    try {
      await todosAPI.createProject(projectId, {
        text: draft.text,
        assignee_id: draft.assigneeId,
        due_date: draft.dueDate,
      });
    } catch (err) {
      throw new Error(apiErrorMessage(err, 'Could not add the to-do'));
    }
    await load();
  };

  return (
    <div style={{ padding: isPhone ? '0 12px 24px' : '0 24px 24px', maxWidth: 900 }} data-help-key="todos">
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', gap: 12, marginBottom: 16 }}>
        <div>
          <h2 className="ov-page-title" style={{ margin: 0, color: 'var(--text)', fontSize: 20, fontWeight: 700 }}>To-dos</h2>
          <div style={{ color: 'var(--text-muted)', fontSize: 13, marginTop: 4 }}>
            {openCount} open. Small jobs to tick off; bigger work goes on the{' '}
            <Link to={`/projects/${projectId}/board`}>board</Link>. Yours across every project:{' '}
            <Link to={MY_TODOS_PATH}>My to-dos</Link>.
          </div>
        </div>
        <SegmentedControl
          value={scope}
          onChange={(v) => setScope(v as 'everyone' | 'mine')}
          options={[
            { value: 'everyone', label: 'Everyone' },
            { value: 'mine', label: 'Just mine' },
          ]}
        />
      </div>

      {error && <ErrorBanner message={error} onDismiss={() => setError('')} />}

      <section
        style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 6, padding: 16 }}
      >
        <TodoQuickAdd onAdd={add} members={members} />
        {loading ? (
          <div style={{ color: 'var(--text-muted)' }}>Loading to-dos…</div>
        ) : (
          <TodoItems
            todos={visible}
            actions={actions}
            members={members}
            highlightId={highlightId}
            emptyText={scope === 'mine' ? 'Nothing open is assigned to you.' : 'Nothing to do. Add a to-do above, or raise one from a note.'}
          />
        )}
      </section>

      {itemId && (
        <WorkItemDrawer
          workItemId={itemId}
          projectId={projectId}
          artifactMap={artifactMap}
          onClose={() => openItem(null)}
          onChanged={load}
          onDeleted={() => openItem(null)}
        />
      )}
    </div>
  );
};
