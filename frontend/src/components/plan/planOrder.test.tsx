import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import type { PlanTask, ProjectPlan } from '../../api/client';
import { PlanViewBar } from './PlanViewBar';
import { OrderField } from './TaskPanel';
import { DEFAULT_VIEW, initialView } from './planView';

// The outline's order is a stored number on each task, set from its panel,
// and "Dated only" leaves the tasks with no dates out of the chart and is
// remembered.

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

const mount = async (ui: React.ReactElement) => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => root.render(ui));
};

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  localStorage.clear();
});

const type = (el: HTMLInputElement, value: string) =>
  act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(el, value);
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });

const blur = (el: HTMLElement) =>
  act(async () => {
    el.dispatchEvent(new FocusEvent('focusout', { bubbles: true }));
  });

describe('OrderField', () => {
  const task = { id: 't', order: 3 } as unknown as PlanTask;

  it('shows the task\'s place and moves it to the place typed', async () => {
    const onCommit = vi.fn();
    await mount(<OrderField task={task} disabled={false} onCommit={onCommit} />);
    const input = container.querySelector('input[aria-label="Order in the outline"]') as HTMLInputElement;
    expect(input.value).toBe('3');
    await type(input, '1');
    await blur(input);
    expect(onCommit).toHaveBeenCalledWith(1);
  });

  it('ignores the same place, and puts back one that is not a place', async () => {
    const onCommit = vi.fn();
    await mount(<OrderField task={task} disabled={false} onCommit={onCommit} />);
    const input = container.querySelector('input[aria-label="Order in the outline"]') as HTMLInputElement;
    await type(input, '3');
    await blur(input);
    await type(input, '0');
    await blur(input);
    expect(onCommit).not.toHaveBeenCalled();
    expect(input.value).toBe('3');
  });
});

describe('Dated only', () => {
  const plan = { tasks: [] } as unknown as ProjectPlan;

  it('is a checkbox beside the filter that sets the view and is remembered', async () => {
    const onView = vi.fn();
    expect(initialView().dated).toBe(false);
    await mount(<PlanViewBar plan={plan} view={DEFAULT_VIEW} onView={onView} selectedTaskId="" />);
    const box = Array.from(container.querySelectorAll('label')).find((l) => l.textContent === 'Dated only')!.querySelector('input') as HTMLInputElement;
    expect(box.checked).toBe(false);
    await act(async () => {
      box.click();
    });
    expect(onView).toHaveBeenCalledWith({ ...DEFAULT_VIEW, dated: true });
    expect(initialView().dated).toBe(true);
  });
});
