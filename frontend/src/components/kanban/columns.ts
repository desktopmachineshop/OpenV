// The board columns, in board order.
//
// This lives on its own because three surfaces now name the same five
// states: the board, the To-dos page, and the status chip a note shows
// beside the to-do raised from it. A label that reads "To Do" in one place
// and "Todo" in another is the kind of thing nobody files a bug about and
// everybody notices.
export interface BoardColumn {
  key: string;
  label: string;
  color: string;
}

export const BOARD_COLUMNS: BoardColumn[] = [
  { key: 'backlog', label: 'Backlog', color: 'var(--neutral)' },
  { key: 'todo', label: 'To Do', color: 'var(--accent)' },
  { key: 'in-progress', label: 'In Progress', color: 'var(--warning)' },
  { key: 'review', label: 'Review', color: 'var(--purple-soft)' },
  { key: 'done', label: 'Done', color: 'var(--success)' },
];

/** The column a to-do starts in when it is raised from a note. */
export const DEFAULT_TODO_COLUMN = 'todo';

/** The column that means the work is finished. */
export const DONE_COLUMN = 'done';

/**
 * The order a to-do list reads in, which is not the board's.
 *
 * Left to right, the board starts at Backlog — the work nobody has
 * committed to. A list of what someone owes wants the opposite end first:
 * what they are doing now, then what is waiting on others, then what is
 * queued, then the backlog, and finished work last.
 */
export const ATTENTION_ORDER = ['in-progress', 'review', 'todo', 'backlog', 'done'];

/** Where a column sits in ATTENTION_ORDER; unknown columns sort last. */
export const attentionRank = (key: string): number => {
  const i = ATTENTION_ORDER.indexOf(key);
  return i === -1 ? ATTENTION_ORDER.length : i;
};

const byKey = new Map(BOARD_COLUMNS.map((c) => [c.key, c]));

/**
 * The column with this key. An unknown key — a column retired in a later
 * release, say — yields a neutral entry carrying the key itself rather than
 * undefined, so a status is always rendered as something.
 */
export const columnFor = (key: string): BoardColumn =>
  byKey.get(key) || { key, label: key || 'Unknown', color: 'var(--neutral)' };

/** Whether a work item in this column still needs doing. */
export const isOpenColumn = (key: string): boolean => key !== DONE_COLUMN;
