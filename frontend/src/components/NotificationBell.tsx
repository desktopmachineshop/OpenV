import React, { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { AppNotification, notificationsAPI } from '../api/client';

interface NotificationBellProps {
  /**
   * 'dark' fits the project sidebar header; 'light' fits the white org-level
   * navbar. Mirrors the UserMenu variants.
   */
  variant?: 'dark' | 'light';
}

// timeAgo renders a compact relative timestamp ("2m", "3h", "5d").
const timeAgo = (iso: string): string => {
  const seconds = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (seconds < 60) return 'now';
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86400)}d`;
};

// pathForNotification maps entity_ref to an app route. Unknown kinds fall
// back to the project overview (or the projects list without a project).
const pathForNotification = (n: AppNotification): string => {
  const ref = n.entity_ref || {};
  // Workspace budget alerts are not project-scoped — deep-link to the
  // workspace usage tab where the budget lives.
  if (ref.kind === 'org_usage') return '/org/settings?tab=usage';
  const projectId = ref.project_id;
  if (!projectId) return '/projects';
  switch (ref.kind) {
    case 'run':
      return `/projects/${projectId}/agent-runs${ref.run_id ? `?run=${ref.run_id}` : ''}`;
    case 'proposal':
      // Proposals are reviewed from the runs view (run detail panel).
      return `/projects/${projectId}/agent-runs${ref.run_id ? `?run=${ref.run_id}` : ''}`;
    case 'interview':
      return `/projects/${projectId}/interviews`;
    case 'artifact':
      return `/projects/${projectId}/requirements`;
    default:
      return `/projects/${projectId}`;
  }
};

// NotificationBell: unread badge + dropdown inbox, fed by the REST list and
// kept live by the per-user SSE stream (EventSource reconnects on its own).
export const NotificationBell: React.FC<NotificationBellProps> = ({ variant = 'light' }) => {
  const navigate = useNavigate();
  const dark = variant === 'dark';
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<AppNotification[]>([]);
  const [unread, setUnread] = useState(0);
  const [loading, setLoading] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const res = await notificationsAPI.list({ limit: 30 });
      setItems(res.data.notifications || []);
      setUnread(res.data.unread_count);
    } catch {
      // ignore — the bell simply stays stale on transient errors
    } finally {
      setLoading(false);
    }
  }, []);

  // Initial badge + live updates over SSE.
  useEffect(() => {
    refresh();
    const es = new EventSource(notificationsAPI.streamUrl(), { withCredentials: true });
    es.addEventListener('notification', (ev) => {
      try {
        const n: AppNotification = JSON.parse((ev as MessageEvent).data);
        setUnread((u) => u + 1);
        setItems((prev) => [n, ...prev].slice(0, 30));
      } catch {
        // malformed frame — ignore
      }
    });
    return () => es.close();
  }, [refresh]);

  // Close on click outside.
  useEffect(() => {
    if (!open) return;
    const onClickOutside = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener('mousedown', onClickOutside);
    return () => document.removeEventListener('mousedown', onClickOutside);
  }, [open]);

  const markRead = async (n: AppNotification) => {
    if (n.read) return;
    setItems((prev) => prev.map((x) => (x.id === n.id ? { ...x, read: true } : x)));
    setUnread((u) => Math.max(0, u - 1));
    try {
      const res = await notificationsAPI.markRead([n.id]);
      setUnread(res.data.unread_count);
    } catch {
      // optimistic update stands; refetch next open
    }
  };

  const markAll = async () => {
    setItems((prev) => prev.map((x) => ({ ...x, read: true })));
    setUnread(0);
    try {
      const res = await notificationsAPI.markAllRead();
      setUnread(res.data.unread_count);
    } catch {
      // ignore
    }
  };

  const openItem = (n: AppNotification) => {
    markRead(n);
    setOpen(false);
    navigate(pathForNotification(n));
  };

  const toggle = () => {
    const next = !open;
    setOpen(next);
    if (next) refresh();
  };

  return (
    <div ref={rootRef} style={{ position: 'relative' }}>
      <button
        onClick={toggle}
        title="Notifications"
        aria-label={unread > 0 ? `Notifications (${unread} unread)` : 'Notifications'}
        style={{
          position: 'relative',
          background: 'none',
          border: 'none',
          cursor: 'pointer',
          padding: 4,
          fontSize: 16,
          lineHeight: 1,
          color: dark ? 'var(--sidebar-text-dim)' : 'var(--text)',
        }}
      >
        {/* Bell glyph */}
        <span aria-hidden="true">🔔</span>
        {unread > 0 && (
          <span
            style={{
              position: 'absolute',
              top: -2,
              right: -4,
              minWidth: 16,
              height: 16,
              padding: '0 4px',
              borderRadius: 8,
              background: 'var(--danger)',
              color: '#fff',
              fontSize: 10,
              fontWeight: 700,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              boxSizing: 'border-box',
            }}
          >
            {unread > 99 ? '99+' : unread}
          </span>
        )}
      </button>

      {open && (
        <div
          style={{
            position: 'absolute',
            top: 'calc(100% + 6px)',
            ...(dark ? { left: 0 } : { right: 0 }),
            width: 320,
            maxHeight: 420,
            overflowY: 'auto',
            background: 'var(--surface)',
            border: '1px solid var(--border)',
            borderRadius: 6,
            boxShadow: '0 6px 18px rgba(0,0,0,0.18)',
            zIndex: 1500,
          }}
        >
          <div
            style={{
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'center',
              padding: '8px 12px',
              borderBottom: '1px solid var(--border-soft)',
            }}
          >
            <span style={{ fontSize: 13, fontWeight: 700, color: 'var(--text)' }}>
              Notifications
            </span>
            {unread > 0 && (
              <button
                onClick={markAll}
                style={{
                  background: 'none',
                  border: 'none',
                  color: 'var(--accent)',
                  cursor: 'pointer',
                  fontSize: 12,
                  padding: 0,
                }}
              >
                Mark all read
              </button>
            )}
          </div>

          {items.length === 0 ? (
            <div style={{ padding: 16, fontSize: 13, color: 'var(--text-muted)' }}>
              {loading ? 'Loading…' : "You're all caught up."}
            </div>
          ) : (
            items.map((n) => (
              <div
                key={n.id}
                onClick={() => openItem(n)}
                style={{
                  display: 'flex',
                  gap: 8,
                  padding: '10px 12px',
                  cursor: 'pointer',
                  borderBottom: '1px solid var(--border-soft)',
                  background: n.read ? 'transparent' : 'var(--tint-blue)',
                }}
              >
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div
                    style={{
                      fontSize: 13,
                      fontWeight: n.read ? 400 : 600,
                      color: 'var(--text)',
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {n.title}
                  </div>
                  {n.body && (
                    <div
                      style={{
                        fontSize: 12,
                        color: 'var(--text-muted)',
                        marginTop: 2,
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                      }}
                    >
                      {n.body}
                    </div>
                  )}
                </div>
                <div
                  style={{
                    display: 'flex',
                    flexDirection: 'column',
                    alignItems: 'flex-end',
                    gap: 4,
                    flexShrink: 0,
                  }}
                >
                  <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
                    {timeAgo(n.created_at)}
                  </span>
                  {!n.read && (
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        markRead(n);
                      }}
                      title="Mark read"
                      style={{
                        background: 'none',
                        border: 'none',
                        cursor: 'pointer',
                        padding: 0,
                        fontSize: 11,
                        color: 'var(--accent)',
                      }}
                    >
                      Mark read
                    </button>
                  )}
                </div>
              </div>
            ))
          )}
        </div>
      )}
    </div>
  );
};
