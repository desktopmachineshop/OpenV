import React, { act, useState } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { TreeDiagram } from './TreeDiagram';
import type { DiagramNode, DiagramShape } from './layout';

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const n = (key: string, children: DiagramNode<string>[] = [], edge?: DiagramNode<string>['edge']): DiagramNode<string> => ({
  key,
  data: key.toUpperCase(),
  children,
  edge,
});

let container: HTMLDivElement;
let root: Root;
beforeEach(() => {
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

const Harness: React.FC<{ shape: DiagramShape<string>; initial?: string[]; onSelect?: (k: string) => void }> = ({ shape, initial = [], onSelect }) => {
  const [folded, setFolded] = useState<Set<string>>(new Set(initial));
  const [selected, setSelected] = useState<string | null>(null);
  return (
    <TreeDiagram
      shape={shape}
      folded={folded}
      onToggle={(k) =>
        setFolded((prev) => {
          const next = new Set(prev);
          if (next.has(k)) next.delete(k);
          else next.add(k);
          return next;
        })
      }
      selectedKey={selected}
      onSelect={(node) => {
        setSelected(node.key);
        onSelect?.(node.key);
      }}
      renderBox={(node) => <span>{node.data}</span>}
      labelOf={(node) => `Box ${node.data}`}
      ariaLabel="Test tree"
    />
  );
};

const items = () => Array.from(container.querySelectorAll<HTMLElement>('[role="treeitem"]'));
const item = (label: string) => container.querySelector<HTMLElement>(`[role="treeitem"][aria-label="Box ${label}"]`)!;
const key = (el: HTMLElement, k: string) => act(() => void el.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true })));

describe('the tree diagram', () => {
  const forest: DiagramShape<string> = { kind: 'forest', roots: [n('a', [n('a1', [n('a1x')]), n('a2')]), n('b')] };

  it('draws a box per unfolded node as a treeitem of a role="tree", with a connector per child', () => {
    act(() => root.render(<Harness shape={forest} initial={['a1']} />));
    expect(container.querySelector('[role="tree"]')?.getAttribute('aria-label')).toBe('Test tree');
    expect(items().map((el) => el.textContent)).toEqual(['A', 'A1', 'A2', 'B']);
    expect(item('A').getAttribute('aria-level')).toBe('1');
    expect(item('A1').getAttribute('aria-expanded')).toBe('false');
    expect(item('B').getAttribute('aria-expanded')).toBeNull();
    expect(container.querySelectorAll('svg path')).toHaveLength(2);
  });

  it('folds and unfolds from the ⊖ / ⊕ on the connector', () => {
    act(() => root.render(<Harness shape={forest} initial={['a1']} />));
    const unfold = container.querySelector<HTMLButtonElement>('button[title="Unfold (1 below)"]')!;
    act(() => unfold.click());
    expect(items()).toHaveLength(5);
    act(() => container.querySelector<HTMLButtonElement>('button[title="Fold"]')!.click());
    expect(items().map((el) => el.textContent)).toEqual(['A', 'B']);
  });

  it('selects on a click and on Enter, and walks the tree with the arrow keys', () => {
    const onSelect = vi.fn();
    act(() => root.render(<Harness shape={forest} initial={['a1']} onSelect={onSelect} />));
    act(() => item('A2').click());
    expect(onSelect).toHaveBeenCalledWith('a2');
    expect(item('A2').getAttribute('aria-selected')).toBe('true');
    // The selected box is the one Tab lands on.
    expect(item('A2').tabIndex).toBe(0);
    expect(item('A').tabIndex).toBe(-1);

    key(item('A2'), 'ArrowUp');
    expect(document.activeElement).toBe(item('A1'));
    // → on a folded box unfolds it; → again goes in.
    key(item('A1'), 'ArrowRight');
    expect(item('A1').getAttribute('aria-expanded')).toBe('true');
    key(item('A1'), 'ArrowRight');
    expect(document.activeElement).toBe(item('A1X'));
    key(item('A1X'), 'ArrowLeft');
    expect(document.activeElement).toBe(item('A1'));
    // ← on an open box folds it.
    key(item('A1'), 'ArrowLeft');
    expect(item('A1').getAttribute('aria-expanded')).toBe('false');
    key(item('A1'), 'End');
    expect(document.activeElement).toBe(item('B'));
    key(item('B'), 'Enter');
    expect(onSelect).toHaveBeenLastCalledWith('b');
  });

  it('draws a two-sided tree with labelled, coloured connectors and mirrored arrow keys on the left', () => {
    const shape: DiagramShape<string> = {
      kind: 'two-sided',
      root: n('seed'),
      left: [n('up', [n('upper')], { label: 'derives-from', color: 'var(--purple)' })],
      right: [n('down', [], { label: 'verifies', color: 'var(--success)' })],
    };
    act(() => root.render(<Harness shape={shape} />));
    expect(items().map((el) => el.textContent)).toEqual(['SEED', 'UP', 'UPPER', 'DOWN']);
    expect(Array.from(container.querySelectorAll('svg text')).map((t) => t.textContent)).toEqual(['derives-from', 'verifies']);
    expect(Array.from(container.querySelectorAll('svg path')).map((p) => p.getAttribute('stroke'))).toContain('var(--purple)');
    // The centre has no fold of its own; the left side folds outward.
    expect(container.querySelectorAll('button[title="Fold"]')).toHaveLength(1);
    key(item('UP'), 'ArrowLeft');
    expect(document.activeElement).toBe(item('UPPER'));
    key(item('UPPER'), 'ArrowRight');
    expect(document.activeElement).toBe(item('UP'));
    // Toward the centre: an open box folds first, then the focus goes to its parent.
    key(item('UP'), 'ArrowRight');
    expect(item('UP').getAttribute('aria-expanded')).toBe('false');
    key(item('UP'), 'ArrowRight');
    expect(document.activeElement).toBe(item('SEED'));
  });

  it('zooms from its buttons', () => {
    act(() => root.render(<Harness shape={forest} />));
    expect(container.textContent).toContain('100%');
    act(() => container.querySelector<HTMLButtonElement>('button[aria-label="Zoom in"]')!.click());
    expect(container.textContent).toContain('125%');
  });

  it('pans without selecting the boxes\' text', () => {
    act(() => root.render(<Harness shape={forest} />));
    const viewport = container.querySelector<HTMLElement>('[role="tree"]')!.closest<HTMLElement>('[style*="grab"]')!;
    expect(viewport.style.userSelect).toBe('none');
  });
});
