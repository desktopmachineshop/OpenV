import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mockApi } from '../../test/mockApi';
import type { Todo } from '../../api/client';

vi.mock('../../api/client', async (orig) => mockApi(await orig(), {}));

import { todosAPI, workItemsAPI } from '../../api/client';
import { useAppStore } from '../../state/store';
import { TodoItems, todoActions } from './TodoItems';
import { TodoQuickAdd } from './TodoQuickAdd';
import { isOneParagraph, todoOverdue, todoTextFromNote } from './todoText';
import { NoteTodoChip, createNoteTodo } from '../NoteTodo';

const todo = (over: Partial<Todo>): Todo => ({
  id: 't1',
  org_id: 'o',
  project_id: 'p',
  text: 'Check the wording',
  done: false,
  done_at: null,
  done_note: '',
  assignee_id: null,
  due_date: null,
  source_artifact_id: null,
  source_chatter_id: null,
  promoted_work_item_id: null,
  created_by: 'me',
  created_at: '2026-11-01T00:00:00Z',
  updated_at: '2026-11-01T00:00:00Z',
  ...over,
});

const initialStore = useAppStore.getState();
const setFeatures = (flags: Record<string, boolean>) =>
  useAppStore.setState({
    features: { channel: 'nightly', stable_release: '', preview: false, features: flags },
  } as never);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;
beforeEach(() => {
  vi.clearAllMocks();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});
afterEach(() => {
  act(() => root.unmount());
  container.remove();
  useAppStore.setState(initialStore, true);
});

const render = (ui: React.ReactElement) => act(() => root.render(ui));
const flush = () => act(async () => {
  await new Promise((r) => setTimeout(r, 0));
});
const byText = (text: string) =>
  Array.from(container.querySelectorAll('*')).find((el) => el.children.length === 0 && el.textContent === text);
const button = (name: RegExp | string) =>
  Array.from(container.querySelectorAll('button')).find((b) =>
    typeof name === 'string' ? b.textContent === name : name.test(b.textContent || '')
  );
const click = (el: Element | null | undefined) => act(() => (el as HTMLElement).click());
const typeInto = (input: HTMLInputElement, value: string) => {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
  act(() => {
    setter.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
};

describe('to-do text', () => {
  it('takes a note’s first paragraph and cuts a long one at a word', () => {
    expect(todoTextFromNote('Ask Sam\nabout seals\n\nThe rest of my thinking')).toBe('Ask Sam\nabout seals');
    const long = todoTextFromNote('word '.repeat(400));
    expect(long.length).toBeLessThanOrEqual(1000);
    expect(long.endsWith('…')).toBe(true);
  });

  it('knows one paragraph from two, and an open past due date from a done one', () => {
    expect(isOneParagraph('one\nline')).toBe(true);
    expect(isOneParagraph('one\n\ntwo')).toBe(false);
    expect(todoOverdue({ done: false, due_date: '2020-01-01T00:00:00Z' })).toBe(true);
    expect(todoOverdue({ done: true, due_date: '2020-01-01T00:00:00Z' })).toBe(false);
  });
});

describe('TodoItems', () => {
  it('lists the open to-dos and collapses the completed ones', () => {
    const actions = todoActions(() => undefined);
    render(
      <MemoryRouter>
        <TodoItems
          todos={[todo({}), todo({ id: 't2', text: 'Already done', done: true })]}
          actions={actions}
          emptyText="Nothing"
        />
      </MemoryRouter>
    );
    expect(byText('Check the wording')).toBeTruthy();
    expect(byText('Already done')).toBeUndefined();
    click(button(/Completed \(1\)/));
    expect(byText('Already done')).toBeTruthy();
  });

  it('ticks a to-do off and reloads', async () => {
    vi.mocked(todosAPI.setDone).mockResolvedValue({ data: todo({ done: true }) } as never);
    const reload = vi.fn();
    render(
      <MemoryRouter>
        <TodoItems todos={[todo({})]} actions={todoActions(reload)} emptyText="Nothing" />
      </MemoryRouter>
    );
    click(container.querySelector('input[aria-label="Tick off: Check the wording"]'));
    await flush();
    expect(reload).toHaveBeenCalled();
    expect(todosAPI.setDone).toHaveBeenCalledWith('t1', true);
  });

  it('promotes a project’s open to-do to a board task', async () => {
    vi.mocked(todosAPI.promote).mockResolvedValue({
      data: { todo: todo({ done: true }), work_item: { id: 'wi-1' } },
    } as never);
    const onPromoted = vi.fn();
    render(
      <MemoryRouter>
        <TodoItems
          todos={[todo({})]}
          actions={todoActions(() => undefined, { promote: true, onPromoted })}
          emptyText="Nothing"
        />
      </MemoryRouter>
    );
    click(button('Promote to board task'));
    await flush();
    expect(onPromoted).toHaveBeenCalledWith('wi-1');
  });

  it('offers no promotion for a personal to-do', () => {
    render(
      <MemoryRouter>
        <TodoItems
          todos={[todo({ project_id: null })]}
          actions={todoActions(() => undefined, { promote: true })}
          emptyText="Nothing"
        />
      </MemoryRouter>
    );
    expect(button('Promote to board task')).toBeUndefined();
    expect(byText('Personal')).toBeTruthy();
  });
});

describe('TodoQuickAdd', () => {
  it('adds what was typed', async () => {
    const onAdd = vi.fn().mockResolvedValue(undefined);
    render(<TodoQuickAdd onAdd={onAdd} />);
    const input = container.querySelector('input[aria-label="New to-do"]') as HTMLInputElement;
    typeInto(input, 'Book the rig');
    click(button('Add'));
    await flush();
    expect(onAdd).toHaveBeenCalledWith({ text: 'Book the rig', assigneeId: null, dueDate: null });
  });
});

describe('a note’s to-do', () => {
  const entry = { id: 'n1', artifact_id: 'a1', message: 'Ask @sam\n\nmore' };

  it('is a to-do of its own with the todos feature', async () => {
    setFeatures({ todos: true });
    vi.mocked(todosAPI.createProject).mockResolvedValue({ data: todo({}) } as never);
    await createNoteTodo('p', entry, { assigneeId: 'sam' });
    expect(todosAPI.createProject).toHaveBeenCalledWith('p', {
      text: 'Ask @sam',
      assignee_id: 'sam',
      due_date: null,
      source_chatter_id: 'n1',
    });
    expect(workItemsAPI.create).not.toHaveBeenCalled();
  });

  it('is a board card without it, as before', async () => {
    setFeatures({});
    vi.mocked(workItemsAPI.create).mockResolvedValue({ data: {} } as never);
    await createNoteTodo('p', entry, {});
    expect(workItemsAPI.create).toHaveBeenCalled();
    expect(todosAPI.createProject).not.toHaveBeenCalled();
  });

  it('shows a to-do’s status, and an old note’s card column', () => {
    render(
      <MemoryRouter>
        <NoteTodoChip projectId="p" todo={{ todo_id: 't1', title: 'Ask', status: 'done' }} />
      </MemoryRouter>
    );
    expect(byText('Done')).toBeTruthy();
    expect(container.querySelector('a')!.getAttribute('href')).toBe('/projects/p/todos?todo=t1');
    render(
      <MemoryRouter>
        <NoteTodoChip projectId="p" todo={{ work_item_id: 'c1', title: 'Old', status: 'review' }} />
      </MemoryRouter>
    );
    expect(container.querySelector('a')!.getAttribute('href')).toBe('/projects/p/todos?item=c1');
  });
});
