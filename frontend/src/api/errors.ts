/**
 * Extracts a human-readable message from an API (axios) error.
 *
 * Backend errors arrive as `{ error: "..." }` JSON bodies; interpolating
 * `err.response?.data` directly into a template string renders
 * "[object Object]". This helper standardizes on the pattern already used by
 * KanbanBoard: `err.response?.data?.error || err.message`, with a couple of
 * defensive extras (plain-string bodies, `{ message: ... }` bodies).
 */
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
