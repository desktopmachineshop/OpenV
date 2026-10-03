// The types api/notifications.ts sends and receives.
// Notifications: the bell's list, notification preferences, web push
// subscriptions, and the domain event feed.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

export interface NotificationPrefs {
  email_notifications: boolean;
  /** Web push opt-in (REQ-109). Defaults false until a device is granted permission. */
  push_notifications: boolean;
}

/** Whether this deployment can send web push, and the key to subscribe with. */
export interface PushConfig {
  enabled: boolean;
  public_key: string;
}

/** One device the member has subscribed. The encryption keys are never returned. */
export interface PushSubscriptionRecord {
  id: string;
  endpoint: string;
  user_agent?: string;
  created_at: string;
  last_used_at?: string;
  failed_at?: string;
}

// Domain audit event (see internal/domain/events). Actors are "system",
// "user:<id>" or "agent:<run_id>".
export interface DomainEvent {
  id: string;
  org_id?: string;
  event_type: string;
  project_id?: string;
  entity_id?: string;
  actor: string;
  payload: Record<string, any>;
  created_at: string;
  // Names the server resolved for the raw IDs above, so the activity log can
  // show who did what to which thing (the IDs still ride along). Any of these
  // may be absent when the referenced row is gone.
  actor_kind?: 'user' | 'agent' | 'worker' | 'system' | string;
  actor_id?: string;
  actor_name?: string;
  entity_kind?: string;
  entity_name?: string;
}

// In-app notification (issue #132). entity_ref carries {kind, ...ids} so the
// bell can navigate to the subject; see NotificationBell.
export interface AppNotification {
  id: string;
  org_id?: string;
  user_id: string;
  type: 'proposal_pending' | 'run_failed' | 'interview_completed' | 'mention' | string;
  title: string;
  body?: string;
  entity_ref: Record<string, any>;
  read: boolean;
  /** The member's own "keep this in reach"; survives clearing. */
  flagged: boolean;
  /** Set once the member has cleared it — absent while it is in the inbox. */
  cleared_at?: string;
  created_at: string;
}

/** Which slice of the member's notifications a listing returns. */
export type NotificationView = 'inbox' | 'flagged' | 'cleared';

export interface NotificationList {
  notifications: AppNotification[] | null;
  unread_count: number;
  /** Present only while more pages remain; hand it straight back as `before`. */
  next_cursor?: string;
}
