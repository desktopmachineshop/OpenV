// Notifications: the bell's list, notification preferences, web push
// subscriptions, and the domain event feed.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import { API_BASE_URL, client } from './http';
import type {
  DomainEvent,
  NotificationList,
  NotificationPrefs,
  NotificationView,
  PushConfig,
  PushSubscriptionRecord,
} from './types/notifications';

// Per-user notification preferences: the email opt-out (issue #187) and the
// web push opt-in (REQ-109). update() takes a partial — the server leaves any
// preference the body does not name exactly as it was.
export const notificationPrefsAPI = {
  get: () => client.get<NotificationPrefs>('/api/v1/me/notification-prefs'),
  update: (prefs: Partial<NotificationPrefs>) =>
    client.put<NotificationPrefs>('/api/v1/me/notification-prefs', prefs),
};

// Web push subscriptions (REQ-109). One subscription per device; the browser
// owns the endpoint and the keys, the server only stores them.
export const pushAPI = {
  config: () => client.get<PushConfig>('/api/v1/me/push/config'),
  list: () =>
    client.get<{ subscriptions: PushSubscriptionRecord[] }>('/api/v1/me/push-subscriptions'),
  subscribe: (body: { endpoint: string; keys: { p256dh: string; auth: string }; user_agent?: string }) =>
    client.post<PushSubscriptionRecord>('/api/v1/me/push-subscriptions', body),
  // DELETE with a body: axios puts it under `data`.
  unsubscribe: (endpoint: string) =>
    client.delete<void>('/api/v1/me/push-subscriptions', { data: { endpoint } }),
};

export const eventsAPI = {
  // The backend clamps limit to (0, 500], defaulting to 100. `before` is a
  // keyset cursor (an event ID from a previous page): the server returns only
  // events strictly older than it. When another (older) page may exist, the
  // response carries an X-Next-Cursor header with the cursor for it.
  list: (params: { project_id?: string; event_type?: string; limit?: number; before?: string }) =>
    client.get<DomainEvent[]>('/api/v1/events', { params }),
};

export const notificationsAPI = {
  list: (params?: { view?: NotificationView; unread?: boolean; limit?: number; before?: string }) =>
    client.get<NotificationList>('/api/v1/notifications', {
      params: {
        ...(params?.view ? { view: params.view } : {}),
        ...(params?.unread ? { unread: 'true' } : {}),
        ...(params?.limit ? { limit: params.limit } : {}),
        ...(params?.before ? { before: params.before } : {}),
      },
    }),
  markRead: (ids: string[]) =>
    client.post<{ updated: number; unread_count: number }>('/api/v1/notifications/read', { ids }),
  markAllRead: () =>
    client.post<{ updated: number; unread_count: number }>('/api/v1/notifications/read-all'),
  // Archives the inbox: the rows move to the cleared view rather than going
  // anywhere, so this is recoverable in the sense that matters.
  clearAll: () =>
    client.post<{ cleared: number; unread_count: number }>('/api/v1/notifications/clear'),
  // The one call that destroys notifications, and it can only reach what has
  // already been cleared. Ask before calling it.
  deleteCleared: () =>
    client.delete<{ deleted: number; unread_count: number }>('/api/v1/notifications/cleared'),
  setFlagged: (id: string, flagged: boolean) =>
    client.put<{ flagged: boolean }>(`/api/v1/notifications/${id}/flag`, { flagged }),
  streamUrl: () => `${API_BASE_URL}/api/v1/notifications/stream`,
};
