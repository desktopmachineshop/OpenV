import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { GuidedWizard } from './GuidedWizard';
import { artifactAPI, guidedAPI } from '../api/client';

vi.mock('../api/client', async (orig) => mockApi(await orig()));

// The assistant column talks to its own endpoints; the review step does not
// need it.
vi.mock('../components/wizard/GuidedChatPanel', () => ({
  GuidedChatPanel: React.forwardRef(() => null),
}));

vi.mock('../hooks/useFeature', () => ({ useFeature: () => false }));

// Nothing here asks a question; the wizard only needs the hook to exist.
vi.mock('../components/ui', async () => {
  const actual = await vi.importActual<any>('../components/ui');
  return { ...actual, useConfirm: () => async () => true };
});

// The wizard reads the open project from the store when the route has none.
vi.mock('../state/store', () => ({ useAppStore: (sel: any) => sel({ projectId: 'p1' }) }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const list = vi.mocked(guidedAPI.list, { partial: true, deep: true });
const commit = vi.mocked(guidedAPI.commit, { partial: true, deep: true });
const listArtifacts = vi.mocked(artifactAPI.list, { partial: true, deep: true });

// A session reopened with *Modify guided definition* and taken to its last
// step: the entries it was seeded with were materialised by the earlier
// commit, which approved them, so nothing it holds is a draft.
const reopened = {
  id: 'gs-2',
  project_id: 'p1',
  status: 'in-progress',
  current_step: 8,
  answers: {},
  draft_artifact_ids: [],
};

const artifact = (id: string, title: string, status: string) => ({
  id,
  project_id: 'p1',
  type: 'requirement',
  title,
  body: '',
  status,
  attributes: { status },
  version: 3,
});

describe('GuidedWizard review step', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    list.mockReset();
    commit.mockReset();
    listArtifacts.mockReset();
    list.mockResolvedValue({ data: [reopened] } as any);
    commit.mockResolvedValue({ data: { ...reopened, status: 'committed' } } as any);
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  const mount = async () => {
    await act(async () => {
      root.render(
        <MemoryRouter>
          <GuidedWizard />
        </MemoryRouter>,
      );
    });
    // The session list, then the step's artifact list.
    for (let i = 0; i < 3; i++) {
      await act(async () => {
        await Promise.resolve();
      });
    }
  };

  const commitButton = () =>
    Array.from(container.querySelectorAll('button')).find((b) => (b.textContent || '').startsWith('Commit'));

  // A reopened definition that adds nothing new still has to be committable:
  // its framing and choices are the edit, and the commit is what closes the
  // session. Its earlier drafts are approved now, so the step lists none.
  it('commits a reopened definition that has no new drafts', async () => {
    listArtifacts.mockResolvedValue({ data: [artifact('a1', 'Warn before the spindle starts', 'approved')] } as any);
    await mount();

    const button = commitButton();
    expect(button?.disabled).toBe(false);
    expect(button?.textContent).toBe('Commit definition');
    expect(container.textContent).toContain('No new drafts to approve');

    await act(async () => {
      button!.click();
    });
    await act(async () => {
      await Promise.resolve();
    });
    expect(commit).toHaveBeenCalledWith('gs-2');
    expect(container.textContent).toContain('Requirements committed');
  });

  it('counts the drafts it will approve', async () => {
    listArtifacts.mockResolvedValue({
      data: [
        artifact('a1', 'Warn before the spindle starts', 'approved'),
        artifact('a2', 'Stop within 1 s', 'draft'),
      ],
    } as any);
    await mount();

    const button = commitButton();
    expect(button?.textContent).toBe('Commit 1 artifact');
    expect(button?.disabled).toBe(false);
  });
});
