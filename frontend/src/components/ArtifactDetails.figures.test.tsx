import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { ArtifactDetails } from './ArtifactDetails';
import { Artifact, Attachment } from '../api/client';

// Figures change only while editing (REQ-54): every view that presents an
// artifact for reading, the comparison with an older version included,
// shows its figures without the controls that change them.

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    linkAPI: {
      listForArtifact: () => Promise.resolve({ data: [] }),
      listForArtifactVersion: () => Promise.resolve({ data: [] }),
    },
    attachmentAPI: {
      getDownloadUrl: (id: string, v?: number) => `/dl/${id}/${v || ''}`,
      listVersions: () => Promise.resolve({ data: [] }),
    },
  })
);

vi.mock('./ui', () => ({
  useAlert: () => () => Promise.resolve(),
  useConfirm: () => () => Promise.resolve(true),
  usePrompt: () => () => Promise.resolve(null),
}));

vi.mock('../hooks/useFeature', () => ({
  useFeature: () => true,
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const artifact = (over: Partial<Artifact> = {}): Artifact =>
  ({
    id: 'a1',
    project_id: 'p1',
    type: 'design-item',
    ref: 'DES-1',
    title: 'Bracket',
    body: 'The bracket.',
    status: 'draft',
    attributes: {},
    version: 2,
    created_at: '2026-09-01T09:00:00Z',
    updated_at: '2026-09-20T09:00:00Z',
    valid_from: '2026-09-20T09:00:00Z',
    ...over,
  }) as Artifact;

const figure: Attachment = {
  id: 'f1',
  artifact_id: 'a1',
  filename: 'DES-1-FIG-1.png',
  original_filename: 'bracket.png',
  title: '',
  mime_type: 'image/png',
  file_path: '/u/one.png',
  file_size: 10,
  kind: 'image',
  figure_ref: 'DES-1-FIG-1',
  figure_num: 1,
  version: 1,
  created_at: '2026-09-02T09:00:00Z',
};

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const render = async (el: React.ReactElement) => {
  await act(async () => {
    root.render(<MemoryRouter>{el}</MemoryRouter>);
  });
};

describe('figures in a reading view', () => {
  it('offers no way to delete a figure while comparing with an older version', async () => {
    await render(
      <ArtifactDetails
        artifact={artifact()}
        attachments={[figure]}
        previewVersion={artifact({
          version: 1,
          updated_at: '2026-09-10T09:00:00Z',
          valid_to: '2026-09-20T09:00:00Z',
        })}
      />
    );
    // Both columns show the figure: it predates the older version.
    expect(container.querySelectorAll('.gallery-filename').length).toBe(2);
    expect(container.querySelector('[aria-label="Delete figure"]')).toBeNull();
  });

  it('offers no way to delete a figure on the details view', async () => {
    await render(<ArtifactDetails artifact={artifact()} attachments={[figure]} />);
    expect(container.querySelectorAll('.gallery-filename').length).toBe(1);
    expect(container.querySelector('[aria-label="Delete figure"]')).toBeNull();
  });
});
