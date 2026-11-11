import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../../test/mockApi';
import { WorkItemTodos } from './WorkItemTodos';
import { planAPI, todosAPI } from '../../api/client';

// A board card's to-dos: the list the plan counts toward the task's
// progress, ticked off and added to on the card, not on the Gantt.

const ok = (data: unknown) => Promise.resolve({ data });
const todo = (id: string, text: string, done: boolean) => ({ id, text, done });

vi.mock('../../api/client', async (orig) =>
  mockApi(await orig(), {
    planAPI: {
      listTodos: () => ok([todo('t1', 'Label the loom', false), todo('t2', 'Crimp the connectors', true)]),
      addTodo: () => ok(todo('t3', 'Test continuity', false)),
    },
    todosAPI: { setDone: () => ok({}) },
  })
);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;
const onChanged = vi.fn();

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

beforeEach(async () => {
  vi.clearAllMocks();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(<WorkItemTodos workItemId="wi-1" onChanged={onChanged} />);
  });
  await flush();
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe("a board card's to-dos", () => {
  it('lists the to-dos on the task with how many are done', () => {
    expect(vi.mocked(planAPI.listTodos)).toHaveBeenCalledWith('wi-1');
    expect(container.querySelector('label')?.textContent).toBe('To-dos · 1/2');
    const boxes = Array.from(container.querySelectorAll('input[type="checkbox"]')) as HTMLInputElement[];
    expect(boxes.map((b) => b.checked)).toEqual([false, true]);
    expect(container.textContent).toContain("Ticking these off moves this task's progress on the Gantt.");
  });

  it('ticks one off at once, saves it and reads the list again', async () => {
    const box = container.querySelector('input[type="checkbox"]') as HTMLInputElement;
    await act(async () => {
      box.click();
    });
    await flush();
    expect(vi.mocked(todosAPI.setDone)).toHaveBeenCalledWith('t1', true);
    expect(vi.mocked(planAPI.listTodos)).toHaveBeenCalledTimes(2);
    expect(onChanged).toHaveBeenCalledTimes(1);
  });

  it('adds a to-do to the task', async () => {
    const input = container.querySelector('input:not([type="checkbox"])') as HTMLInputElement;
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, '  Test continuity ');
      input.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await act(async () => {
      container.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    });
    await flush();
    expect(vi.mocked(planAPI.addTodo)).toHaveBeenCalledWith('wi-1', 'Test continuity');
    expect(input.value).toBe('');
  });
});
