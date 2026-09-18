import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { ArtifactStepper } from './ArtifactStepper';

// The visible half of stepping: where the ends are dead, what a screen reader
// is told, and where focus goes when the button under the pointer disables
// itself.

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

describe('ArtifactStepper', () => {
  let container: HTMLDivElement;
  let root: Root;
  let steps: number[];

  const render = (position: number, total = 3, touch = false) => {
    act(() => {
      root.render(
        <ArtifactStepper
          position={position}
          total={total}
          label="REQ-12 Seal integrity"
          onStep={(delta) => steps.push(delta)}
          touch={touch}
          showKeyHints={!touch}
        />
      );
    });
  };

  const button = (name: string) =>
    container.querySelector<HTMLButtonElement>(`button[aria-label="${name}"]`)!;
  const previous = () => button('Previous artifact');
  const next = () => button('Next artifact');

  beforeEach(() => {
    steps = [];
    container = document.createElement('div');
    document.body.appendChild(container);
    act(() => {
      root = createRoot(container);
    });
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  it('shows the place in the document', () => {
    render(2);
    expect(container.textContent).toContain('2 of 3');
  });

  it('has nothing behind the first artifact', () => {
    render(1);
    expect(previous().disabled).toBe(true);
    expect(next().disabled).toBe(false);
  });

  it('has nothing beyond the last artifact', () => {
    render(3);
    expect(next().disabled).toBe(true);
    expect(previous().disabled).toBe(false);
  });

  it('asks for a step in the direction pressed', () => {
    render(2);
    act(() => next().click());
    act(() => previous().click());
    expect(steps).toEqual([1, -1]);
  });

  it('announces what was landed on, not only the count', () => {
    render(2);
    const live = container.querySelector('[aria-live="polite"]')!;
    expect(live.textContent).toBe('REQ-12 Seal integrity, 2 of 3');
    // The visible count would otherwise be read out twice.
    expect(live.classList.contains('sr-only')).toBe(true);
  });

  it('hands focus to the live button when the pressed one is about to die', () => {
    render(2);
    next().focus();
    act(() => next().click());
    // The caller now renders position 3, where Next is disabled; the stepper
    // moved focus before that happened so it is not lost to the document.
    expect(document.activeElement).toBe(previous());
  });

  it('keeps focus where it is while there is further to go', () => {
    render(1, 5);
    next().focus();
    act(() => next().click());
    expect(document.activeElement).toBe(next());
  });

  it('names the keys on a pointer device and stays quiet about them on touch', () => {
    render(2, 3, false);
    expect(next().title).toBe('Next artifact (J)');
    expect(previous().title).toBe('Previous artifact (K)');
    render(2, 3, true);
    expect(next().title).toBe('Next artifact');
  });

  it('gives a finger something to hit', () => {
    render(2, 3, true);
    expect(next().style.minHeight).toBe('44px');
    expect(next().style.minWidth).toBe('44px');
  });
});
