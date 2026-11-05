import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Navigate, useSearchParams } from 'react-router-dom';
import { Todo, projectAPI, todosAPI } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { Navbar } from '../components/Navbar';
import { TodoItems, todoActions } from '../components/todos/TodoItems';
import { TodoQuickAdd } from '../components/todos/TodoQuickAdd';
import { ErrorBanner } from '../components/ui';
import { TODOS_FEATURE } from '../features';
import { useFeature } from '../hooks/useFeature';
import { useViewport } from '../hooks/useViewport';
import { useAppStore } from '../state/store';

/**
 * My to-dos (issue #731): one person's to-dos across the workspace's
 * projects, the ones assigned to them, and their personal ones, which have
 * no project and nobody else sees. Quick add makes a personal to-do.
 *
 * Gated on the todos feature: without it, or with no workspace, the page
 * sends a member to the projects list, where /org/todos led before.
 */
export const MyTodosPage: React.FC = () => {
  const gatesSettled = useAppStore((s) => s.features !== null || s.featuresFailed);
  const noWorkspace = useAppStore((s) => s.orgsLoaded && !s.activeOrgId);
  const on = useFeature(TODOS_FEATURE);
  if ((gatesSettled && !on) || noWorkspace) return <Navigate to="/projects" replace />;
  return (
    <div className="app-shell" style={{ background: 'var(--bg-app)', display: 'flex', flexDirection: 'column' }}>
      <Navbar title="My to-dos" showWorkspaceControls />
      {on ? (
        <MyTodos />
      ) : (
        <p style={{ padding: 20, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</p>
      )}
    </div>
  );
};

const MyTodos: React.FC = () => {
  const activeOrgId = useAppStore((s) => s.activeOrgId);
  const { isPhone } = useViewport();
  const [todos, setTodos] = useState<Todo[] | null>(null);
  const [projectNames, setProjectNames] = useState<Record<string, string>>({});
  const [error, setError] = useState('');
  const [searchParams] = useSearchParams();

  const load = useCallback(async () => {
    try {
      const res = await todosAPI.listMine();
      setTodos(res.data || []);
      setError('');
    } catch (err) {
      setError(apiErrorMessage(err, 'Failed to load your to-dos'));
      setTodos((prev) => prev || []);
    }
  }, []);

  // A workspace switch lists the new workspace's to-dos.
  useEffect(() => {
    setTodos(null);
    void load();
    projectAPI
      .list()
      .then((res) => {
        const names: Record<string, string> = {};
        (res.data || []).forEach((p) => {
          names[p.id] = p.name;
        });
        setProjectNames(names);
      })
      .catch(() => {
        /* rows show no project name */
      });
  }, [load, activeOrgId]);

  const actions = useMemo(() => todoActions(load), [load]);

  const add = async (draft: { text: string; dueDate: string | null }) => {
    try {
      await todosAPI.createPersonal({ text: draft.text, due_date: draft.dueDate });
    } catch (err) {
      throw new Error(apiErrorMessage(err, 'Could not add the to-do'));
    }
    await load();
  };

  const openCount = (todos || []).filter((t) => !t.done).length;

  return (
    <main
      style={{ padding: isPhone ? '16px 12px 24px' : '24px', maxWidth: 900, width: '100%', boxSizing: 'border-box', margin: '0 auto' }}
    >
      <h1 style={{ margin: 0, fontSize: 22, color: 'var(--text)' }}>My to-dos</h1>
      <p style={{ color: 'var(--text-muted)', fontSize: 13, margin: '4px 0 16px' }}>
        {todos === null
          ? 'Loading…'
          : `${openCount} open: what is assigned to you in this workspace's projects, and your own personal to-dos, which nobody else sees.`}
      </p>
      {error && <ErrorBanner message={error} onDismiss={() => setError('')} />}
      <section style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 6, padding: 16 }}>
        <TodoQuickAdd onAdd={add} placeholder="Add a personal to-do and press Enter" />
        {todos !== null && (
          <TodoItems
            todos={todos}
            actions={actions}
            projectNames={projectNames}
            highlightId={searchParams.get('todo')}
            emptyText="Nothing to do. To-dos assigned to you in any project show up here."
          />
        )}
      </section>
    </main>
  );
};
