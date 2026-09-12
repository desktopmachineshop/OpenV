import React, { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { PANEL_NOTIFICATIONS, PANEL_PARAM } from '../appShortcuts';
import { AppNotification, NotificationView, notificationsAPI } from '../api/client';
import { useViewport } from '../hooks/useViewport';
import { useConfirm } from './ui';

interface NotificationBellProps {
  /**
   * 'dark' fits the project sidebar header; 'light' fits the white org-level
   * navbar. Mirrors the UserMenu variants.
   */
  variant?: 'dark' | 'light';
}

// How many rows a page of any view carries. The inbox rarely fills one; the
// cleared history is the view that pages.
const PAGE_SIZE = 30;

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
  // Membership and privilege changes land on the people list they are about:
  // the workspace's members tab, or the project's own.
  if (ref.kind === 'membership') return '/org/settings?tab=members';
  if (ref.kind === 'project_membership' && ref.project_id) {
    return `/projects/${ref.project_id}/settings?tab=members`;
  }
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
  // Phones: the panel takes the width of the screen below the top bar instead
  // of a 320px popover hanging off a corner (which lands half off-screen).
  const viewport = useViewport();
  const compact = viewport.isCompact;
  const navigate = useNavigate();
  const dark = variant === 'dark';
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<AppNotification[]>([]);
  const [unread, setUnread] = useState(0);
  const [loading, setLoading] = useState(false);
  const [clearing, setClearing] = useState(false);
  // Which tab is showing. The inbox is what the badge counts; flagged and
  // cleared are the two ways a member gets back to something afterwards.
  const [view, setView] = useState<NotificationView>('inbox');
  const [cursor, setCursor] = useState<string | undefined>(undefined);
  const [paging, setPaging] = useState(false);
  const confirm = useConfirm();
  const rootRef = useRef<HTMLDivElement | null>(null);
  // The SSE subscription is set up once; this lets its handler see the tab
  // showing right now without tearing the stream down on every switch.
  const viewRef = useRef<NotificationView>('inbox');
  useEffect(() => {
    viewRef.current = view;
  }, [view]);

  const refresh = useCallback(async (which: NotificationView = 'inbox') => {
    setLoading(true);
    try {
      const res = await notificationsAPI.list({ view: which, limit: PAGE_SIZE });
      setItems(res.data.notifications || []);
      setUnread(res.data.unread_count);
      setCursor(res.data.next_cursor);
    } catch {
      // ignore — the bell simply stays stale on transient errors
    } finally {
      setLoading(false);
    }
  }, []);

  // Page the current view. The cursor is opaque: whatever the last page
  // returned goes straight back as `before`, so new arrivals at the top of
  // the list cannot make a page skip or repeat rows the way an offset would.
  const loadMore = async () => {
    if (!cursor || paging) return;
    setPaging(true);
    try {
      const res = await notificationsAPI.list({ view, limit: PAGE_SIZE, before: cursor });
      setItems((prev) => [...prev, ...(res.data.notifications || [])]);
      setCursor(res.data.next_cursor);
    } catch {
      // The cursor stands, so the button is still there to try again.
    } finally {
      setPaging(false);
    }
  };

  const showView = async (which: NotificationView) => {
    if (which === view) return;
    setView(which);
    setItems([]);
    setCursor(undefined);
    await refresh(which);
  };

  // The installed-app "Notifications" shortcut lands on ?panel=notifications
  // (manifest.json): there is no notifications route — the inbox is this
  // panel — so the parameter opens it, then is dropped so a reload or a back
  // navigation does not reopen it.
  const [searchParams, setSearchParams] = useSearchParams();
  useEffect(() => {
    if (searchParams.get(PANEL_PARAM) !== PANEL_NOTIFICATIONS) return;
    setOpen(true);
    const next = new URLSearchParams(searchParams);
    next.delete(PANEL_PARAM);
    setSearchParams(next, { replace: true });
  }, [searchParams, setSearchParams]);

  // Initial badge + live updates over SSE.
  useEffect(() => {
    refresh();
    const es = new EventSource(notificationsAPI.streamUrl(), { withCredentials: true });
    es.addEventListener('notification', (ev) => {
      try {
        const n: AppNotification = JSON.parse((ev as MessageEvent).data);
        setUnread((u) => u + 1);
        // A new notification arrives in the inbox. Dropping it into the
        // flagged or cleared list while one of those is showing would put a
        // row there that does not belong to that view.
        setItems((prev) => (viewRef.current === 'inbox' ? [n, ...prev].slice(0, PAGE_SIZE) : prev));
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

  // Clearing archives rather than deletes: the rows move to the Cleared tab,
  // so this asks for confirmation but does not warn about losing anything.
  const clearAll = async () => {
    const unreadPart = unread > 0 ? ` ${unread} of them unread.` : '';
    const ok = await confirm({
      title: 'Clear notifications',
      message: `Clear all ${items.length} notifications?${unreadPart} They move to the Cleared tab, where you can still read them.`,
      confirmLabel: 'Clear all',
    });
    if (!ok) return;
    setClearing(true);
    try {
      const res = await notificationsAPI.clearAll();
      setItems([]);
      setCursor(undefined);
      setUnread(res.data.unread_count);
    } catch {
      // Left as it was: the list on screen still matches the server.
    } finally {
      setClearing(false);
    }
  };

  // The one destructive action, and it only reaches what is already cleared.
  const deleteCleared = async () => {
    const ok = await confirm({
      title: 'Delete cleared notifications',
      message: `Permanently delete all ${items.length} cleared notifications? This cannot be undone.`,
      confirmLabel: 'Delete forever',
      danger: true,
    });
    if (!ok) return;
    setClearing(true);
    try {
      const res = await notificationsAPI.deleteCleared();
      setItems([]);
      setCursor(undefined);
      setUnread(res.data.unread_count);
    } catch {
      // Left as it was.
    } finally {
      setClearing(false);
    }
  };

  // Flagging is optimistic: it is one boolean, the row is already on screen,
  // and a failure puts it straight back.
  const toggleFlag = async (n: AppNotification) => {
    const next = !n.flagged;
    setItems((prev) => prev.map((x) => (x.id === n.id ? { ...x, flagged: next } : x)));
    try {
      await notificationsAPI.setFlagged(n.id, next);
      // Unflagging from the Flagged tab takes the row out of that view.
      if (!next && viewRef.current === 'flagged') {
        setItems((prev) => prev.filter((x) => x.id !== n.id));
      }
    } catch {
      setItems((prev) => prev.map((x) => (x.id === n.id ? { ...x, flagged: !next } : x)));
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
    if (next) refresh(view);
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
          minWidth: 40,
          minHeight: 40,
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
          role="dialog"
          aria-label="Notifications"
          style={{
            ...(compact
              ? { position: 'fixed', left: 8, right: 8, top: 56, width: 'auto', maxHeight: 'calc(100vh - 72px)' }
              : {
                  position: 'absolute',
                  // The dark bell sits in the sidebar footer, at the bottom of the
                  // window: a panel dropping down from it would be off-screen, so
                  // it opens upward the way the user menu beside it does.
                  ...(dark
                    ? { bottom: 'calc(100% + 6px)', left: 0 }
                    : { top: 'calc(100% + 6px)', right: 0 }),
                  width: 320,
                  maxHeight: 420,
                }),
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
            <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
              {view === 'inbox' && unread > 0 && (
                <button
                  onClick={markAll}
                  style={{
                    background: 'none',
                    border: 'none',
                    color: 'var(--accent)',
                    cursor: 'pointer',
                    fontSize: 12.5,
                    padding: '6px 0',
                    minHeight: 36,
                  }}
                >
                  Mark all read
                </button>
              )}
              {/* Each bulk action belongs to the tab it acts on: clearing is
                  an inbox action, deleting forever only makes sense where the
                  cleared rows are. Muted rather than accent-coloured, so
                  neither is the control a thumb finds first. */}
              {view === 'inbox' && items.length > 0 && (
                <button
                  onClick={clearAll}
                  disabled={clearing}
                  style={{
                    background: 'none',
                    border: 'none',
                    color: 'var(--text-muted)',
                    cursor: clearing ? 'default' : 'pointer',
                    fontSize: 12.5,
                    padding: '6px 0',
                    minHeight: 36,
                  }}
                >
                  {clearing ? 'Clearing…' : 'Clear all'}
                </button>
              )}
              {view === 'cleared' && items.length > 0 && (
                <button
                  onClick={deleteCleared}
                  disabled={clearing}
                  style={{
                    background: 'none',
                    border: 'none',
                    color: 'var(--text-muted)',
                    cursor: clearing ? 'default' : 'pointer',
                    fontSize: 12.5,
                    padding: '6px 0',
                    minHeight: 36,
                  }}
                >
                  {clearing ? 'Deleting…' : 'Delete forever'}
                </button>
              )}
            </div>
          </div>

          {/* The three views. Tabs rather than a filter menu: there are
              exactly three, and which one is showing decides what the bulk
              action above does, so it needs to be visible at a glance. */}
          <div
            role="tablist"
            aria-label="Notification views"
            style={{
              display: 'flex',
              gap: 4,
              padding: '6px 8px',
              borderBottom: '1px solid var(--border-soft)',
            }}
          >
            {([
              ['inbox', 'Inbox'],
              ['flagged', 'Flagged'],
              ['cleared', 'Cleared'],
            ] as [NotificationView, string][]).map(([key, label]) => (
              <button
                key={key}
                role="tab"
                aria-selected={view === key}
                onClick={() => showView(key)}
                style={{
                  flex: 1,
                  background: view === key ? 'var(--tint-blue)' : 'none',
                  border: 'none',
                  borderRadius: 4,
                  color: view === key ? 'var(--text)' : 'var(--text-muted)',
                  fontSize: 12.5,
                  fontWeight: view === key ? 600 : 400,
                  cursor: 'pointer',
                  padding: '6px 8px',
                  minHeight: 36,
                }}
              >
                {label}
              </button>
            ))}
          </div>

          {items.length === 0 ? (
            <div style={{ padding: 16, fontSize: 13, color: 'var(--text-muted)' }}>
              {loading
                ? 'Loading…'
                : view === 'flagged'
                  ? 'Nothing flagged. Flag a notification to keep it here.'
                  : view === 'cleared'
                    ? 'Nothing cleared yet.'
                    : "You're all caught up."}
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
                  <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                    {timeAgo(n.created_at)}
                  </span>
                  {/* Flagging is how a notification stays reachable after the
                      inbox is cleared, so it is offered on every row in every
                      view — including the cleared one, where it is the way to
                      pull something back out of the history. */}
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      toggleFlag(n);
                    }}
                    aria-pressed={n.flagged}
                    aria-label={n.flagged ? 'Remove flag' : 'Flag this notification'}
                    title={n.flagged ? 'Remove flag' : 'Flag to keep'}
                    style={{
                      background: 'none',
                      border: 'none',
                      cursor: 'pointer',
                      padding: '6px 0',
                      // A single glyph is only ~13 px wide, well under the
                      // 32 px tap floor, so the target is widened to match
                      // its height rather than left the size of the star.
                      minHeight: 36,
                      minWidth: 36,
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      fontSize: 14,
                      lineHeight: 1,
                      color: n.flagged ? 'var(--accent)' : 'var(--text-muted)',
                    }}
                  >
                    {n.flagged ? '★' : '☆'}
                  </button>
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
                        padding: '6px 0',
                        minHeight: 36,
                        fontSize: 12.5,
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

          {/* Only while the server says there is another page. The cleared
              history is the view this is really for. */}
          {cursor && (
            <button
              onClick={loadMore}
              disabled={paging}
              style={{
                display: 'block',
                width: '100%',
                background: 'none',
                border: 'none',
                borderTop: '1px solid var(--border-soft)',
                color: 'var(--accent)',
                cursor: paging ? 'default' : 'pointer',
                fontSize: 12.5,
                padding: '10px 12px',
                minHeight: 44,
              }}
            >
              {paging ? 'Loading…' : 'Load older'}
            </button>
          )}
        </div>
      )}
    </div>
  );
};
