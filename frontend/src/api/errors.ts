import type { ApiErrorCode } from '../generated/contract';

/**
 * The API's error codes (`code` in an error body, internal/api/httperr.go)
 * the app branches on, each its own key and value. The object is held to the
 * generated contract's ApiErrorCode (refactor plan X4b), so a code the server
 * renames or drops fails tsc here; arch/vocabParity.test.ts reads each one the
 * app compares a code with through it.
 */
export const API_ERROR = {
  email_unverified: 'email_unverified',
  limit_reached: 'limit_reached',
  plan_read_only: 'plan_read_only',
  reset_invalid: 'reset_invalid',
  reset_email_unavailable: 'reset_email_unavailable',
} as const satisfies { readonly [Code in ApiErrorCode]?: Code };

/**
 * Extracts a human-readable message from an API (axios) error.
 *
 * Backend errors arrive as `{ error: "..." }` JSON bodies; interpolating
 * `err.response?.data` directly into a template string renders
 * "[object Object]". This helper standardizes on the pattern already used by
 * KanbanBoard: `err.response?.data?.error || err.message`, with a couple of
 * defensive extras (plain-string bodies, `{ message: ... }` bodies).
 */
/** True when the API refused the call because the account's email is unverified. */
export function isEmailUnverifiedError(err: unknown): boolean {
  const anyErr = err as { response?: { status?: number; data?: { code?: unknown } } } | null;
  return anyErr?.response?.status === 403 && anyErr?.response?.data?.code === API_ERROR.email_unverified;
}

/** What the API said when a workspace limit stopped the call. */
export interface LimitRefusal {
  /** The catalogued key, e.g. "max_members". */
  limit: string;
  label: string;
  used: number;
  allowed: number;
  /** How to raise it, phrased for whoever can actually do it on this
   *  deployment: a plan upgrade when hosted, a setting when self-hosted. */
  remedy: string;
}

/**
 * Reads a limit refusal off an error, or null when it is not one.
 *
 * The plain message from apiErrorMessage already carries the whole story
 * including the remedy, so this is only needed where the UI wants to act on
 * the numbers — showing a meter, or linking to the upgrade.
 */
export function limitRefusal(err: unknown): LimitRefusal | null {
  const anyErr = err as { response?: { status?: number; data?: Record<string, unknown> } } | null;
  const data = anyErr?.response?.data;
  if (anyErr?.response?.status !== 403 || !data || data.code !== API_ERROR.limit_reached) return null;
  return {
    limit: String(data.limit ?? ''),
    label: String(data.label ?? ''),
    used: Number(data.used ?? 0),
    allowed: Number(data.allowed ?? 0),
    remedy: String(data.remedy ?? ''),
  };
}

/** True when the API refused a write because the workspace is over its
 *  plan and read-only. The message already says what to do; this lets the
 *  UI point at the Billing tab. */
export function isPlanReadOnlyError(err: unknown): boolean {
  const anyErr = err as { response?: { status?: number; data?: { code?: unknown } } } | null;
  return anyErr?.response?.status === 403 && anyErr?.response?.data?.code === API_ERROR.plan_read_only;
}

/** The stable `code` field of an API error body, or '' when there is none. */
export function apiErrorCode(err: unknown): string {
  const anyErr = err as { response?: { data?: { code?: unknown } } } | null;
  const code = anyErr?.response?.data?.code;
  return typeof code === 'string' ? code : '';
}

/** Seconds the API asked the client to wait, from a 429's Retry-After header. */
export function retryAfterSeconds(err: unknown): number | null {
  const anyErr = err as { response?: { headers?: Record<string, unknown> } } | null;
  const raw = anyErr?.response?.headers?.['retry-after'];
  const n = typeof raw === 'string' ? parseInt(raw, 10) : NaN;
  return Number.isFinite(n) && n > 0 ? n : null;
}

export function apiErrorMessage(err: unknown, fallback = 'Request failed'): string {
  const anyErr = err as {
    response?: { data?: unknown };
    message?: unknown;
  } | null;

  const data = anyErr?.response?.data;
  if (typeof data === 'string' && data.trim()) return data;
  if (data && typeof data === 'object') {
    const body = data as { error?: unknown; message?: unknown };
    if (typeof body.error === 'string' && body.error) return body.error;
    if (typeof body.message === 'string' && body.message) return body.message;
  }
  if (typeof anyErr?.message === 'string' && anyErr.message) return anyErr.message;
  return fallback;
}

/**
 * The inline chain `err.response?.data?.error || err.message || fallback`
 * that older call sites write out (quirk Q20), as one function that gives
 * what the chain gives for every input: the body's `error` when truthy, else
 * the error's `message` when truthy, else the fallback. A truthy value is
 * passed on as it is, string or not, and a null or undefined err throws as
 * the chain does. Unlike apiErrorMessage, it shows a plain-text body as the
 * error's message ("Request failed with status code 500") and never reads a
 * `{ message }` body. Moving a call site here (X15b-X15e) changes nothing it
 * renders; moving one on to apiErrorMessage changes what it renders, which is
 * a release-noted change of its own.
 */
export function legacyErrorText(err: unknown, fallback: string): string {
  const anyErr = err as any;
  return anyErr.response?.data?.error || anyErr.message || fallback;
}
