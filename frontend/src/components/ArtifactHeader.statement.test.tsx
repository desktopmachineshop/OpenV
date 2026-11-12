import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import type { Artifact } from '../api/client';
import { ArtifactHeader } from './ArtifactHeader';
import { ArtifactStatement } from './ArtifactStatement';
import { DialogProvider } from './ui';

// The refreshed artifact pane (ui-refresh): the artifact's text straight
// under its title, its facts folded to one line with the table behind
// Details, and History and Archive behind a ⋯ menu beside Edit. Without
// statementFirst the header is as it was.

vi.mock('../api/client', async (orig) => mockApi(await orig(), {}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const artifact = (over: Partial<Artifact> = {}): Artifact =>
  ({
    id: 'req-2',
    project_id: 'p1',
    parent_id: null,
    type: 'requirement',
    ref: 'REQ-2',
    title: 'Positioning accuracy',
    body: 'The mill shall position the tool within 0.02 mm.',
    sort_order: 1,
    status: 'in_review',
    attributes: { priority: 'must', verification_method: 'test' },
    version: 2,
    valid_from: '2026-09-01T09:00:00Z',
    valid_to: null,
    created_at: '2026-09-01T09:00:00Z',
    updated_at: '2026-09-01T09:00:00Z',
    ...over,
  }) as Artifact;

let container: HTMLDivElement;
let root: Root;

const mount = async (props: Partial<React.ComponentProps<typeof ArtifactHeader>> = {}, a = artifact()) => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <DialogProvider>
        <MemoryRouter>
          <ArtifactHeader artifact={a} onEdit={() => {}} onDelete={() => {}} {...props}>
            <ArtifactStatement artifact={a} />
          </ArtifactHeader>
        </MemoryRouter>
      </DialogProvider>
    );
  });
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const buttons = () => Array.from(container.querySelectorAll('button')).map((b) => (b.textContent ?? '').trim());
const click = async (name: string) => {
  const b = Array.from(container.querySelectorAll('button')).find((x) => (x.textContent ?? '').trim() === name);
  await act(async () => {
    (b as HTMLElement).click();
  });
};

describe('ArtifactHeader, statement first', () => {
  it('puts the text under the title, ahead of the facts and the decision', async () => {
    await mount({ statementFirst: true });
    const text = container.textContent ?? '';
    const at = (s: string) => text.indexOf(s);
    expect(at('Positioning accuracy')).toBeGreaterThan(-1);
    expect(at('The mill shall position the tool')).toBeGreaterThan(at('Positioning accuracy'));
    expect(at('Requirement · verify by test · version 2')).toBeGreaterThan(at('The mill shall position the tool'));
    expect(at('Approve')).toBeGreaterThan(at('version 2'));
    expect(container.querySelector('a')?.getAttribute('href')).toBe('/projects/p1/impact?artifact=req-2');
  });

  it('folds the facts to one line and opens the table on Details', async () => {
    await mount({ statementFirst: true });
    expect(container.querySelector('dl')).toBeNull();
    await click('Details');
    const labels = Array.from(container.querySelectorAll('dt')).map((d) => d.textContent);
    expect(labels).toEqual(['Type', 'Priority', 'Verify by', 'Version', 'Updated', 'UID']);
  });

  it('keeps Edit as a button and moves History and Archive behind the ⋯ menu', async () => {
    const onDelete = vi.fn();
    await mount({ statementFirst: true, onDelete });
    expect(buttons()).toContain('Edit');
    expect(buttons()).not.toContain('History');
    expect(buttons()).not.toContain('Archive');
    await act(async () => {
      (container.querySelector('button[aria-label="More actions"]') as HTMLElement).click();
    });
    expect(Array.from(container.querySelectorAll('[role="menuitem"]')).map((e) => e.textContent)).toEqual(['History', 'Archive']);
    await click('Archive');
    expect(onDelete).toHaveBeenCalledWith('req-2');
  });

  it('keeps History a button on an archived artifact, which is read through its history', async () => {
    await mount({ statementFirst: true, archivedAt: '2026-09-02T09:00:00Z' });
    expect(buttons()).toContain('History');
    expect(container.querySelector('button[aria-label="More actions"]')).toBeNull();
  });

  it('is the header as it was without statementFirst', async () => {
    await mount();
    expect(buttons()).toEqual(expect.arrayContaining(['Edit', 'History', 'Archive']));
    expect(container.querySelector('dl')).not.toBeNull();
    expect(container.textContent).not.toContain('The mill shall position the tool');
  });
});
