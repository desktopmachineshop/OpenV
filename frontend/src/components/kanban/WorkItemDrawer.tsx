import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  Artifact,
  WorkItem,
  WorkItemActivity,
  workItemsAPI,
} from '../../api/client';
import { ExpandableText } from '../ExpandableText';
import { GANTT_PLAN_FEATURE } from '../../features';
import { useFeature } from '../../hooks/useFeature';
import { WorkItemTodos } from './WorkItemTodos';
import { OpenIcon } from '../ui/OpenIcon';
import { ErrorBanner, buttonStyle, chipStyle, useConfirm } from '../ui';

interface WorkItemDrawerProps {
  workItemId: string;
  projectId: string;
  artifactMap: Record<string, Artifact>;
  onClose: () => void;
  onChanged: () => void;
  onDeleted: () => void;
  /**
   * More about the task, shown beside its details. With it the task opens
   * as a wide pane in the middle of the window, over everything, instead of
   * a drawer at the side.
   */
  aside?: React.ReactNode;
  /**
   * Where the card shows. Left out, it is a drawer over the side of the
   * page (or the wide pane, with an aside). "docked" puts it in the page
   * beside what it belongs to, which gives it room rather than be covered;
   * "pane" opens it wide in the middle of the window, over everything.
   */
  mode?: 'docked' | 'pane';
  /** Offers the card in full from its header: the docked card's way to the pane. */
  onExpand?: () => void;
}

const activityStyle = (kind: string): React.CSSProperties => {
  switch (kind) {
    case 'comment':
      return { background: 'var(--tint-yellow)', border: '1px solid var(--tint-yellow-border)' };
    case 'status':
    case 'moved':
      return { background: 'var(--tint-blue)', border: '1px solid var(--tint-blue-border)' };
    case 'agent':
    case 'agent_run':
      return { background: 'var(--tint-purple)', border: '1px solid var(--tint-purple-border)' };
    case 'error':
      return { background: 'var(--tint-red)', border: '1px solid var(--tint-red-border)' };
    default:
      return { background: 'var(--surface-alt)', border: '1px solid var(--surface-alt)' };
  }
};

/** The card at the side of the page, and as a pane in the middle of the window. */
const DRAWER_FRAME: React.CSSProperties = { top: 0, right: 0, bottom: 0, width: 'min(420px, 100vw)', boxShadow: '-4px 0 16px rgba(0,0,0,0.15)' };
/** The card in the page, beside the board: it takes its width from the board, which stays in view. */
const DOCKED_FRAME: React.CSSProperties = { position: 'relative', width: 'min(400px, 40%)', flexShrink: 0, alignSelf: 'stretch', minHeight: 0, border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden' };
const PANE_FRAME: React.CSSProperties = { top: '4vh', left: '50%', transform: 'translateX(-50%)', width: 'min(1180px, 96vw)', height: '92vh', borderRadius: 10, overflow: 'hidden', boxShadow: '0 12px 48px rgba(0,0,0,0.3)' };

export const WorkItemDrawer: React.FC<WorkItemDrawerProps> = ({
  workItemId,
  projectId,
  artifactMap,
  onClose,
  onChanged,
  onDeleted,
  aside,
  mode,
  onExpand,
}) => {
  const pane = Boolean(aside) || mode === 'pane';
  const docked = mode === 'docked' && !pane;
  const confirm = useConfirm();
  // A card's to-dos count toward its task's progress on the plan.
  const ganttPlan = useFeature(GANTT_PLAN_FEATURE);
  const [item, setItem] = useState<WorkItem | null>(null);
  const [activity, setActivity] = useState<WorkItemActivity[]>([]);
  const [error, setError] = useState('');
  const [editingTitle, setEditingTitle] = useState(false);
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [descriptionDirty, setDescriptionDirty] = useState(false);
  const [comment, setComment] = useState('');

  // `load` is memoized on [workItemId] only, so reading `descriptionDirty`
  // directly inside it would capture a stale value (frozen at false) and a
  // reload (e.g. after adding a comment) could clobber unsaved edits. The ref
  // always reflects the latest dirty flag.
  const descriptionDirtyRef = useRef(descriptionDirty);
  descriptionDirtyRef.current = descriptionDirty;

  // Same stale-closure guard for the title: while the user is editing it, a
  // reload (e.g. after adding a comment) must not clobber their in-progress
  // edit with the server value.
  const editingTitleRef = useRef(editingTitle);
  editingTitleRef.current = editingTitle;

  const load = useCallback(() => {
    workItemsAPI
      .get(workItemId)
      .then((res) => {
        setItem(res.data.item);
        setActivity(res.data.activity || []);
        if (!editingTitleRef.current) setTitle(res.data.item.title);
        if (!descriptionDirtyRef.current) setDescription(res.data.item.description || '');
        setError('');
      })
      .catch((err: any) => {
        setError(err.response?.data?.error || err.message || 'Failed to load card');
      });
  }, [workItemId]);

  useEffect(() => {
    load();
  }, [load]);

  const saveTitle = async () => {
    if (!item || !title.trim() || title === item.title) {
      setEditingTitle(false);
      return;
    }
    try {
      await workItemsAPI.update(item.id, { title: title.trim() });
      setEditingTitle(false);
      load();
      onChanged();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to save title');
    }
  };

  const saveDescription = async () => {
    if (!item || !descriptionDirty) return;
    try {
      await workItemsAPI.update(item.id, { description });
      setDescriptionDirty(false);
      load();
      onChanged();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to save description');
    }
  };

  const addComment = async () => {
    if (!item || !comment.trim()) return;
    try {
      await workItemsAPI.comment(item.id, comment.trim());
      setComment('');
      load();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to add comment');
    }
  };

  const remove = async () => {
    if (!item) return;
    const ok = await confirm({
      title: 'Delete card',
      message: `Delete card "${item.title}"?`,
      confirmLabel: 'Delete',
      danger: true,
    });
    if (!ok) return;
    try {
      await workItemsAPI.remove(item.id);
      onDeleted();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to delete card');
    }
  };

  return (
    <>
    {pane && <div aria-hidden onClick={onClose} style={{ position: 'fixed', inset: 0, background: 'var(--overlay)', zIndex: 949 }} />}
    <div
      data-help-pane="board.card"
      data-help-key="board.card"
      {...(pane ? { role: 'dialog', 'aria-modal': true, 'aria-label': `Task ${item?.title || ''}` } : docked ? { role: 'complementary', 'aria-label': `Card ${item?.title || ''}` } : {})}
      style={{
        ...(docked ? DOCKED_FRAME : { position: 'fixed', ...(pane ? PANE_FRAME : DRAWER_FRAME) }),
        background: 'var(--surface)',
        // Above the project nav drawer (900), below dialogs (2000).
        zIndex: docked ? undefined : 950,
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      <div
        style={{
          padding: '14px 18px',
          borderBottom: '1px solid var(--neutral-soft)',
          display: 'flex',
          alignItems: 'flex-start',
          gap: 8,
        }}
      >
        {editingTitle ? (
          <input
            value={title}
            autoFocus
            onChange={(e) => setTitle(e.target.value)}
            onBlur={saveTitle}
            onKeyDown={(e) => e.key === 'Enter' && saveTitle()}
            style={{ fontSize: 16, fontWeight: 600 }}
          />
        ) : (
          <h3
            style={{ margin: 0, flex: 1, fontSize: 16, color: 'var(--text)', cursor: 'text' }}
            title="Click to edit title"
            onClick={() => setEditingTitle(true)}
          >
            {item?.title || '…'}
          </h3>
        )}
        {onExpand && (
          <button type="button" onClick={onExpand} aria-label="Open the card in full" title="Open the card in full" style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', width: 40, minHeight: 40, padding: 0, marginTop: -8 }}>
            <OpenIcon />
          </button>
        )}
        <button onClick={onClose} aria-label="Close" title="Close" style={{ background: 'none', border: 'none', fontSize: 22, cursor: 'pointer', color: 'var(--text-muted)', lineHeight: 1, width: 40, minHeight: 40, padding: 0, marginTop: -8, marginRight: -8, flexShrink: 0 }}>
          ×
        </button>
      </div>

      <div style={{ flex: 1, minHeight: 0, display: 'flex' }}>
      <div style={{ flex: 1, minWidth: 0, overflowY: 'auto', padding: 18 }}>
        <ErrorBanner message={error} onDismiss={() => setError('')} />

        <div className="form-group">
          <label>Description</label>
          <textarea
            value={description}
            onChange={(e) => {
              setDescription(e.target.value);
              setDescriptionDirty(true);
            }}
            onBlur={saveDescription}
            style={{ minHeight: 80, fontSize: 13 }}
            placeholder="Add a description…"
          />
        </div>

        {item && item.artifact_ids && item.artifact_ids.length > 0 && (
          <div style={{ marginBottom: 16 }}>
            <label>Linked artifacts</label>
            <div>
              {item.artifact_ids.map((id) => (
                <Link
                  key={id}
                  to={`/projects/${projectId}/requirements?artifact=${id}`}
                  className="ov-tap"
                  style={{
                    ...chipStyle('neutral', 'md'),
                    margin: '2px 6px 2px 0',
                    fontWeight: 600,
                    color: 'var(--text)',
                    textDecoration: 'none',
                  }}
                >
                  {artifactMap[id]?.title || id}
                </Link>
              ))}
            </div>
          </div>
        )}

        {item?.agent_run_id && (
          <div style={{ marginBottom: 16 }} data-help-key="board.agents">
            <Link
              to={`/projects/${projectId}/agent-runs?run=${item.agent_run_id}`}
              style={{ ...buttonStyle('secondary'), textDecoration: 'none' }}
            >
              View run →
            </Link>
          </div>
        )}

        {ganttPlan && item && <WorkItemTodos workItemId={item.id} />}

        <label>Activity</label>
        <div style={{ marginBottom: 12 }}>
          {activity.length === 0 && (
            <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>No activity yet.</div>
          )}
          {activity.map((a) => (
            <div
              key={a.id}
              style={{
                ...activityStyle(a.kind),
                borderRadius: 4,
                padding: '8px 10px',
                marginBottom: 8,
                fontSize: 13,
              }}
            >
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 3 }}>
                <strong>{a.actor || 'system'}</strong> · {a.kind} ·{' '}
                {new Date(a.created_at).toLocaleString()}
              </div>
              <div style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word', color: 'var(--text)' }}>
                <ExpandableText text={a.content || ''} limit={600} />
              </div>
            </div>
          ))}
        </div>

        <div className="form-group">
          <textarea
            value={comment}
            onChange={(e) => setComment(e.target.value)}
            placeholder="Write a comment…"
            style={{ minHeight: 60, fontSize: 13 }}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && e.ctrlKey) {
                e.preventDefault();
                addComment();
              }
            }}
          />
          <button
            className="button"
            style={{ marginTop: 6, padding: '6px 16px', fontSize: 13 }}
            onClick={addComment}
            disabled={!comment.trim()}
          >
            Comment
          </button>
        </div>
      </div>
      {aside && (
        <div data-task-aside style={{ width: 'min(420px, 42%)', flexShrink: 0, overflowY: 'auto', borderLeft: '1px solid var(--neutral-soft)', background: 'var(--surface-alt)' }}>
          {aside}
        </div>
      )}
      </div>

      <div style={{ borderTop: '1px solid var(--neutral-soft)', padding: 14, textAlign: 'right' }}>
        <button
          onClick={remove}
          style={buttonStyle('danger')}
        >
          Delete card
        </button>
      </div>
    </div>
    </>
  );
};
