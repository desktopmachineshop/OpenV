/**
 * Pure helpers for evidence bundles, kept out of the view so they can be
 * tested (and reused) without dragging the router in.
 */

/** Renders a byte count the way a person would say it. */
export const humanBytes = (n: number): string => {
  if (n < 1024) return `${n} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let value = n / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(1)} ${units[unit]}`;
};

/**
 * Conditions are free-form, and a form cannot know a discipline's vocabulary
 * in advance, so they are entered as "name: value" lines. A line with no
 * colon is kept as a bare note rather than dropped — losing what somebody
 * typed would be worse than storing it untidily. Only the FIRST colon
 * separates, so a value may contain one ("started: 09:30").
 */
export const parseConditions = (text: string): Record<string, unknown> => {
  const out: Record<string, unknown> = {};
  text
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .forEach((line, index) => {
      const at = line.indexOf(':');
      if (at < 0) {
        out[`note ${index + 1}`] = line;
        return;
      }
      const key = line.slice(0, at).trim();
      const value = line.slice(at + 1).trim();
      // A line that opens with a colon has no name to file it under; there is
      // nothing meaningful to keep.
      if (key) out[key] = value;
    });
  return out;
};

export const formatConditions = (conditions: Record<string, unknown>): string =>
  Object.entries(conditions || {})
    .map(([k, v]) => `${k}: ${String(v)}`)
    .join('\n');
