import { Baseline } from '../api/client';

/**
 * Reading a baseline in a list.
 *
 * A baseline is what a later argument about what was agreed gets settled
 * against, so "which one is this?" has to be answerable from the line itself:
 * its name, when it was taken, and who took it. Both places baselines appear
 * are native `<select>` elements, and an `<option>` holds text and nothing
 * else, so all three have to fit on one line.
 *
 * Kept here rather than in the views so it can be tested without dragging a
 * router in behind it.
 */

/** The date a reader actually wants: short, unambiguous, no clock. */
export const baselineDate = (iso: string): string => {
  const when = new Date(iso);
  if (Number.isNaN(when.getTime())) return '';
  return when.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
};

/**
 * The author, or nothing at all.
 *
 * Baselines captured before authorship was recorded have no author, and
 * neither does one taken by an automation holding a workspace key. Both are
 * shown as an absence rather than as "Unknown", which would read like a
 * person whose name failed to load.
 */
export const baselineAuthor = (baseline: Baseline): string =>
  (baseline.created_by_name || '').trim();

/** Name, date and author on one line, skipping whatever is missing. */
export const baselineLabel = (baseline: Baseline): string =>
  [baseline.name, baselineDate(baseline.created_at), baselineAuthor(baseline)]
    .filter((part) => part !== '')
    .join(' · ');
