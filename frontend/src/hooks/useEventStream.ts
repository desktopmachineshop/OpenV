import { useEffect, useRef } from 'react';
import type { SseEventName } from '../generated/contract';

// The one module allowed to open an EventSource (refactor plan X15, quirk
// Q21; EVENT_SOURCE_HOOK in eslint.config.js). The app's four SSE consumers
// each wrote their own reconnect loop, in three different ways; this hook
// takes each way as a named policy and reproduces it as written, so moving a
// consumer onto it (X15b-X15e) changes nothing a reader can see:
//
//   RunDetailPanel     backoff (3 retries at 1, 2 and 4 s, a live event
//                      restoring them; then the caller polls) and after_seq
//   GuidedChatPanel    a capped exponent for ever (2, 4, 8, 15, 15 ... s; an
//                      open restarts it)
//   InterviewChat      the same, without credentials (the public interview),
//                      asking at each drop whether its interview has ended
//   NotificationBell   the browser's own reconnect (no error handler at all)

/** What a listener may do to the stream its event arrived on. */
export interface EventStreamHandle {
  /**
   * Closes the EventSource the event arrived on, as RunDetailPanel does on a
   * terminal status. A closed EventSource fires no error, so nothing reopens
   * it until the url changes.
   */
  close(): void;
}

export type EventStreamListener = (event: MessageEvent, stream: EventStreamHandle) => void;

/**
 * The server events a stream listens for, each under its own name. A caller
 * passes an object literal with the names as plain keys ({ log, status }),
 * the same keys on every render: arch/sseListeners.test.ts reads the keys of
 * each call as the events that caller listens for (invariant I9), and the
 * type holds them to the names the server sends (SseEventName).
 */
export type EventStreamListeners = { readonly [Name in SseEventName]?: EventStreamListener };

/**
 * NotificationBell's policy: the hook sets no error handler, so a dropped
 * EventSource reconnects by itself, when the browser decides to.
 */
export interface BrowserReconnect {
  readonly kind: 'browser';
}

/**
 * GuidedChatPanel's and InterviewChat's policy: retry for ever. Each drop
 * closes the stream and raises n by one, up to maxExponent, then waits
 * min(baseDelayMs * 2^n, maxDelayMs); an open sets n back to 0. Like the
 * retryRef each of those components keeps, n outlives a change of url.
 * onRetry is read when it is needed, from the newest render.
 */
export interface CappedExponentReconnect {
  readonly kind: 'cappedExponent';
  readonly baseDelayMs: number;
  readonly maxExponent: number;
  readonly maxDelayMs: number;
  /**
   * A retry is scheduled: called before its delay. InterviewChat asks its
   * intro again there, and once its interview has ended, whose stream the
   * server refuses (#379 bug 216), leaves the stream: url null, which
   * cancels the retry.
   */
  readonly onRetry?: () => void;
}

/**
 * RunDetailPanel's policy: up to maxAttempts retries, retry n (from 0) after
 * baseDelayMs * 2^n, and any event the stream listens for restoring the whole
 * budget (an open does not). Once the budget is spent, or when the
 * EventSource cannot be made, onGiveUp. The callbacks are read when they are
 * needed, from the newest render.
 */
export interface BackoffReconnect {
  readonly kind: 'backoff';
  readonly baseDelayMs: number;
  readonly maxAttempts: number;
  /** A retry is scheduled: called before its delay (RunDetailPanel catches its log up). */
  readonly onRetry?: () => void;
  /** The budget is spent, or the EventSource could not be made (RunDetailPanel polls instead). */
  readonly onGiveUp?: () => void;
  /** Read at each drop: true leaves the stream closed, with no retry and no onGiveUp (a terminal run). */
  readonly isDone?: () => boolean;
}

export type ReconnectPolicy = BrowserReconnect | CappedExponentReconnect | BackoffReconnect;

/** Today's three reconnect policies with their timings, by the callers that use them. */
export const RECONNECT = {
  /** NotificationBell. */
  browser: { kind: 'browser' },
  /** GuidedChatPanel and InterviewChat: 2, 4, 8, 15, 15 ... s. */
  cappedExponent: { kind: 'cappedExponent', baseDelayMs: 1000, maxExponent: 6, maxDelayMs: 15000 },
  /** RunDetailPanel (its MAX_SSE_RECONNECT_ATTEMPTS and SSE_RECONNECT_BASE_DELAY_MS): 1, 2, 4 s. */
  backoff: { kind: 'backoff', baseDelayMs: 1000, maxAttempts: 3 },
} as const satisfies Record<string, ReconnectPolicy>;

export interface EventStreamOptions {
  /**
   * Sends the session cookie with the stream request. Every stream of the
   * signed-in app does; the public interview, whose reader has no session,
   * passes false. A change reopens the stream.
   */
  readonly withCredentials: boolean;
  /** What a drop leads to (RECONNECT holds today's three). A change of kind reopens the stream. */
  readonly reconnect: ReconnectPolicy;
  /**
   * RunDetailPanel's after_seq: the URL each connect opens in place of url,
   * built as it opens, so that a reconnect resumes after the last seq the
   * caller has read. url stays the stream's identity.
   */
  readonly resumeUrl?: () => string;
}

/**
 * Opens an EventSource on url (none while url is null) and keeps it open
 * under the given reconnect policy until url, withCredentials or the
 * policy's kind changes, or the component unmounts; each of those closes it
 * and cancels a pending retry. Each event reaches the listener of the newest
 * render, so new handlers never reopen the stream.
 */
export function useEventStream(
  url: string | null,
  listeners: EventStreamListeners,
  options: EventStreamOptions
): void {
  // Built on every render so that its loop reads the listeners parameter
  // itself: arch/sseListeners.test.ts resolves the names a stream listens
  // for from the keys of the object literal each caller passes here (S6).
  const listen = (es: EventSource, deliver: (name: SseEventName, event: MessageEvent) => void) => {
    for (const name of Object.keys(listeners) as SseEventName[]) {
      es.addEventListener(name, (event) => deliver(name, event));
    }
  };
  const latest = useRef({ listeners, options, listen });
  latest.current = { listeners, options, listen };

  // CappedExponentReconnect's n.
  const exponent = useRef(0);

  const { withCredentials } = options;
  const kind = options.reconnect.kind;

  useEffect(() => {
    if (url === null) return undefined;
    let es: EventSource | null = null;
    let retryTimer: number | null = null;
    let closed = false;
    // BackoffReconnect's spent budget, per stream (RunDetailPanel's reconnectAttempts).
    let attempts = 0;

    const open = () => {
      const source = new EventSource(latest.current.options.resumeUrl?.() ?? url, { withCredentials });
      es = source;
      if (kind === 'cappedExponent') {
        source.onopen = () => {
          exponent.current = 0;
        };
      }
      const handle: EventStreamHandle = { close: () => source.close() };
      latest.current.listen(source, (name, event) => {
        // A live event proves the stream healthy again (backoff only).
        if (kind === 'backoff') attempts = 0;
        latest.current.listeners[name]?.(event, handle);
      });
      if (kind === 'browser') return;
      source.onerror = () => {
        source.close();
        if (es === source) es = null;
        if (closed) return;
        const policy = latest.current.options.reconnect;
        if (policy.kind === 'cappedExponent') {
          const n = Math.min(exponent.current + 1, policy.maxExponent);
          exponent.current = n;
          const delay = Math.min(policy.baseDelayMs * 2 ** n, policy.maxDelayMs);
          policy.onRetry?.();
          retryTimer = window.setTimeout(connect, delay);
        } else if (policy.kind === 'backoff') {
          if (policy.isDone?.()) return;
          if (attempts < policy.maxAttempts) {
            const delay = policy.baseDelayMs * 2 ** attempts;
            attempts += 1;
            policy.onRetry?.();
            retryTimer = window.setTimeout(connect, delay);
          } else {
            policy.onGiveUp?.();
          }
        }
      };
    };

    const connect = () => {
      if (closed) return;
      if (kind !== 'backoff') {
        open();
        return;
      }
      try {
        open();
      } catch {
        const policy = latest.current.options.reconnect;
        if (policy.kind === 'backoff') policy.onGiveUp?.();
      }
    };

    connect();

    return () => {
      closed = true;
      es?.close();
      es = null;
      if (retryTimer !== null) window.clearTimeout(retryTimer);
    };
  }, [url, withCredentials, kind]);
}
