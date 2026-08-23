import React, { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  Artifact,
  WorkItem,
  WorkItemActivity,
  workItemsAPI,
} from '../../api/client';
import { ExpandableText } from '../ExpandableText';

interface WorkItemDrawerProps {
  workItemId: string;
  projectId: string;
  artifactMap: Record<string, Artifact>;
  onClose: () => void;
  onChanged: () => void;
  onDeleted: () => void;
}

const activityStyle = (kind: string): React.CSSProperties => {
  switch (kind) {
    case 'comment':
      return { background: '#fff9e6', border: '1px solid #f1e0a8' };
    case 'status':
    case 'moved':
      return { background: '#eef6fc', border: '1px solid #cfe5f5' };
    case 'agent':
    case 'agent_run':
      return { background: '#f0eefc', border: '1px solid #d5d0f0' };
    case 'error':
      return { background: '#fdedec', border: '1px solid #f5b7b1' };
    default:
      return { background: '#f8f9f9', border: '1px solid #e5e8e8' };
  }
};

export const WorkItemDrawer: React.FC<WorkItemDrawerProps> = ({
  workItemId,
  projectId,
  artifactMap,
  onClose,
  onChanged,
  onDeleted,
}) => {
  const [item, setItem] = useState<WorkItem | null>(null);
  const [activity, setActivity] = useState<WorkItemActivity[]>([]);
  const [error, setError] = useState('');
  const [editingTitle, setEditingTitle] = useState(false);
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [descriptionDirty, setDescriptionDirty] = useState(false);
  const [comment, setComment] = useState('');

  const load = useCallback(() => {
    workItemsAPI
      .get(workItemId)
      .then((res) => {
        setItem(res.data.item);
        setActivity(res.data.activity || []);
        setTitle(res.data.item.title);
        if (!descriptionDirty) setDescription(res.data.item.description || '');
        setError('');
      })
      .catch((err: any) => {
        setError(err.response?.data?.error || err.message || 'Failed to load card');
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
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
    if (!window.confirm(`Delete card "${item.title}"?`)) return;
    try {
      await workItemsAPI.remove(item.id);
      onDeleted();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to delete card');
    }
  };

  return (
    <div
      style={{
        position: 'fixed',
        top: 0,
        right: 0,
        bottom: 0,
        width: 420,
        background: '#fff',
        boxShadow: '-4px 0 16px rgba(0,0,0,0.15)',
        zIndex: 100,
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      <div
        style={{
          padding: '14px 18px',
          borderBottom: '1px solid #ecf0f1',
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
            style={{ margin: 0, flex: 1, fontSize: 16, color: '#2c3e50', cursor: 'text' }}
            title="Click to edit title"
            onClick={() => setEditingTitle(true)}
          >
            {item?.title || '…'}
          </h3>
        )}
        <button
          onClick={onClose}
          style={{
            background: 'none',
            border: 'none',
            fontSize: 20,
            cursor: 'pointer',
            color: '#7f8c8d',
            lineHeight: 1,
          }}
          title="Close"
        >
          ×
        </button>
      </div>

      <div style={{ flex: 1, overflowY: 'auto', padding: 18 }}>
        {error && <div style={{ color: '#e74c3c', fontSize: 13, marginBottom: 10 }}>{error}</div>}

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
                  to={`/projects/${projectId}/requirements`}
                  style={{
                    display: 'inline-block',
                    margin: '2px 6px 2px 0',
                    padding: '3px 10px',
                    background: '#ecf0f1',
                    borderRadius: 10,
                    fontSize: 12,
                    color: '#2c3e50',
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
          <div style={{ marginBottom: 16 }}>
            <Link
              to={`/projects/${projectId}/agent-runs?run=${item.agent_run_id}`}
              className="button-secondary"
              style={{ textDecoration: 'none', display: 'inline-block', padding: '6px 14px', fontSize: 13, borderRadius: 4, color: '#fff' }}
            >
              View run →
            </Link>
          </div>
        )}

        <label>Activity</label>
        <div style={{ marginBottom: 12 }}>
          {activity.length === 0 && (
            <div style={{ color: '#7f8c8d', fontSize: 13 }}>No activity yet.</div>
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
              <div style={{ fontSize: 11, color: '#7f8c8d', marginBottom: 3 }}>
                <strong>{a.actor || 'system'}</strong> · {a.kind} ·{' '}
                {new Date(a.created_at).toLocaleString()}
              </div>
              <div style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word', color: '#2c3e50' }}>
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

      <div style={{ borderTop: '1px solid #ecf0f1', padding: 14, textAlign: 'right' }}>
        <button
          onClick={remove}
          style={{
            background: '#e74c3c',
            color: '#fff',
            border: 'none',
            padding: '8px 16px',
            borderRadius: 4,
            cursor: 'pointer',
            fontSize: 13,
          }}
        >
          Delete card
        </button>
      </div>
    </div>
  );
};
