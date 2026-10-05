import type { SseEventName } from './generated/contract';

// The events the server sends on an SSE stream (invariant I9), each name its
// own key and value. The object is held to the generated contract's
// SseEventName (refactor plan X4b), whose names S6 reads from the Go sources
// (contracts/sse-events.json): a name the server renames or drops, or one
// missing here, fails tsc. Every addEventListener on an EventSource names its
// event through this object; arch/sseListeners.test.ts reads each name from
// its literal type. "error" also reaches onerror, which is how the app
// listens for it.
export const SSE_EVENT = {
  assistant_partial: 'assistant_partial',
  error: 'error',
  log: 'log',
  message: 'message',
  notification: 'notification',
  partial: 'partial',
  status: 'status',
} as const satisfies { readonly [Name in SseEventName]: Name };
