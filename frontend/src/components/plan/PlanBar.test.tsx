import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { PlanBar } from './PlanBar';

// The one bar every chart of the plan draws: its tone, a parent's neutral
// look, the critical ring and the selection are decided here, so the Gantt,
// the Timeline and All projects cannot drift apart.

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

const draw = (props: Partial<React.ComponentProps<typeof PlanBar>>) => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => root.render(<PlanBar left={10} width={80} top={4} height={20} tone="active" progress={40} label="Weld: 40%" {...props} />));
  return container.querySelector('button')!;
};

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe('PlanBar', () => {
  it('fills its progress in the tone of the task and says its label', () => {
    const bar = draw({ tone: 'late' });
    expect(bar.getAttribute('aria-label')).toBe('Weld: 40%');
    expect(bar.style.background).toBe('var(--tint-red)');
    const fill = bar.firstElementChild as HTMLElement;
    expect(fill.style.width).toBe('40%');
    expect(fill.style.background).toBe('var(--danger)');
    expect(bar.textContent).toBe('');
  });

  it('draws a parent in the neutral tone whatever its state, and writes a name where one is given', () => {
    const bar = draw({ tone: 'late', summary: true, text: 'Build' });
    expect(bar.style.background).toBe('var(--neutral-soft)');
    expect((bar.firstElementChild as HTMLElement).style.background).toBe('var(--text-secondary)');
    expect(bar.textContent).toBe('Build');
  });

  it('rings the critical path in amber, ahead of the selection, and carries what the chart puts on it', () => {
    const onClick = vi.fn();
    const bar = draw({ critical: true, selected: true, button: { 'data-task-bar': 't1', onClick } });
    expect(bar.style.boxShadow).toContain('var(--warning)');
    expect(bar.getAttribute('data-task-bar')).toBe('t1');
    act(() => bar.click());
    expect(onClick).toHaveBeenCalledTimes(1);
  });
});
