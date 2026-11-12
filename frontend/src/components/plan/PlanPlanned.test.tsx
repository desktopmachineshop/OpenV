import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import type { ProjectPlan } from '../../api/client';
import { PlanDefaults } from './PlanDefaults';
import { PlanPlanned } from './PlanPlanned';

// Sizing a plan before it is staffed (plan-sizing): planned resources are
// added by name with a rate and a capacity, staffed with a person or an
// agent, or removed; and the plan's defaults set the scheduling mode and
// whether a new task's effort decides its duration.

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
});

const type = (el: HTMLInputElement | HTMLSelectElement, value: string) =>
  act(async () => {
    const proto = el instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(el, value);
    el.dispatchEvent(new Event(el instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
  });

const click = (el: Element | null | undefined) =>
  act(async () => {
    el!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });

const plan = {
  scheduling_mode: 'manual',
  effort_driven_default: false,
  people: [
    { type: 'user', id: 'u1', name: 'Ann' },
    { type: 'planned', id: 'p1', name: 'Welder' },
    { type: 'agent', id: 'a1', name: 'Drafter' },
  ],
  resources: [
    { type: 'user', id: 'u1', name: 'Ann', tasks: 1, work_days: 2 },
    { type: 'planned', id: 'p1', name: 'Welder', planned_name: 'Welder', tasks: 2, work_days: 6 },
  ],
} as unknown as ProjectPlan;

describe('PlanPlanned', () => {
  it('adds a planned resource by name, with its rate and capacity', async () => {
    const onPlanned = vi.fn();
    await mount(<PlanPlanned plan={plan} onPlanned={onPlanned} />);
    const add = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === 'Add planned resource') as HTMLButtonElement;
    expect(add.disabled).toBe(true);
    await type(container.querySelector('input[aria-label="Name of the new planned resource"]')!, ' Painter ');
    await type(container.querySelector('input[aria-label="Day rate of the new planned resource"]')!, '350');
    await type(container.querySelector('input[aria-label="Capacity of the new planned resource, in percent"]')!, '50');
    await click(add);
    expect(onPlanned).toHaveBeenCalledWith({ kind: 'add', name: 'Painter', daily_rate: 350, capacity: 50 });
  });

  it('lists the planned resources with their work, and staffs or removes one', async () => {
    const onPlanned = vi.fn();
    await mount(<PlanPlanned plan={plan} onPlanned={onPlanned} />);
    const row = container.querySelector('[data-planned="p1"]')!;
    expect(row.textContent).toContain('Welder');
    expect(row.textContent).toContain('2 tasks · 6 days');
    const staff = row.querySelector('select')!;
    // A placeholder is staffed with a person or an agent, never another placeholder.
    expect(Array.from(staff.options).map((o) => o.textContent)).toEqual(['Staff with…', 'Ann', 'Drafter (agent)']);
    await type(staff, 'agent:a1');
    expect(onPlanned).toHaveBeenCalledWith({ kind: 'staff', id: 'p1', with: { type: 'agent', id: 'a1' } });
    await click(row.querySelector('button[aria-label="Remove Welder"]'));
    expect(onPlanned).toHaveBeenCalledWith({ kind: 'remove', id: 'p1' });
  });
});

describe('PlanDefaults', () => {
  it('sets the scheduling mode and whether effort decides the duration of new tasks', async () => {
    const onChange = vi.fn();
    await mount(<PlanDefaults plan={plan} onChange={onChange} onClose={() => {}} />);
    await type(container.querySelector('select[aria-label="Default scheduling mode"]')!, 'auto');
    expect(onChange).toHaveBeenCalledWith({ scheduling_mode: 'auto' });
    await click(container.querySelector('input[aria-label="Effort decides the duration of new tasks"]'));
    expect(onChange).toHaveBeenCalledWith({ effort_driven_default: true });
  });
});
