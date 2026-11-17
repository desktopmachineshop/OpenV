// The small rules of a to-do (issue #731) that the note, the project's list
// and My to-dos share: how much text one holds, what a note becomes, and the
// words and colour of its status. The server holds the same limit
// (internal/domain/todos, MaxTextLength) and refuses a second paragraph.

/** Where My to-dos lives: a workspace page, beside the workspace's Runs. */
export const MY_TODOS_PATH = '/org/todos';

/** The most characters a to-do holds: a paragraph. */
export const TODO_TEXT_LIMIT = 1000;

/**
 * The to-do a note becomes: its first paragraph, cut at a word before the
 * limit. A note of three paragraphs is somebody's thinking; the to-do is the
 * one thing to do about it, and the note it links back to keeps the rest.
 */
export const todoTextFromNote = (message: string): string => {
  const first = (message || '').replace(/\r\n/g, '\n').trim().split(/\n\s*\n/)[0].trim();
  if (first.length <= TODO_TEXT_LIMIT) return first;
  const cut = first.slice(0, TODO_TEXT_LIMIT - 1);
  const lastSpace = cut.lastIndexOf(' ');
  return `${(lastSpace > TODO_TEXT_LIMIT / 2 ? cut.slice(0, lastSpace) : cut).trimEnd()}…`;
};

/** Whether text is one paragraph: no blank line inside it. */
export const isOneParagraph = (text: string): boolean => !/\n\s*\n/.test(text.trim());

/** A to-do's status as a note's chip shows it: the words, and a colour that only repeats them. */
export const todoStatusFor = (status: string): { label: string; color: string } =>
  status === 'done'
    ? { label: 'Done', color: 'var(--success, #2e7d32)' }
    : { label: 'Open', color: 'var(--accent)' };

/** Overdue is only meaningful for a to-do still open. */
export const todoOverdue = (todo: { done: boolean; due_date: string | null }, now = Date.now()): boolean => {
  if (todo.done || !todo.due_date) return false;
  const due = Date.parse(todo.due_date);
  return !Number.isNaN(due) && due < now;
};

/** A due date as a person reads it. */
export const formatTodoDue = (value: string): string => {
  const due = new Date(value);
  if (Number.isNaN(due.getTime())) return value;
  return due.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
};

/** A date input's YYYY-MM-DD as the midnight UTC instant the API stores. */
export const dueFromInput = (value: string): string | null => (value ? `${value}T00:00:00Z` : null);

/** The YYYY-MM-DD a date input shows for a stored due date. */
export const dueToInput = (value: string | null): string => (value ? value.slice(0, 10) : '');
