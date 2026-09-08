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
  return anyErr?.response?.status === 403 && anyErr?.response?.data?.code === 'email_unverified';
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
