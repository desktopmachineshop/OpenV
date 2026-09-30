import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { ModuleView } from './ModuleView';
import { artifactAPI, attachmentAPI } from '../api/client';
import { DialogProvider } from '../components/ui';

// Adding a figure takes its artifact to a new version on the server (REQ-4),
// as a figure's new file and a rename already did, so the view reloads the
// artifact after an upload rather than keep showing the version it had.

vi.mock('../api/client', () => ({
  artifactAPI: { list: vi.fn(), getVersions: vi.fn() },
  linkAPI: { list: vi.fn(), listForArtifact: vi.fn() },
  attachmentAPI: {
    listByProject: vi.fn(),
    listByArtifact: vi.fn(),
    upload: vi.fn(),
    getDownloadUrl: (id: string) => `/dl/${id}`,
  },
  baselineAPI: { list: vi.fn() },
  qualityAPI: { project: vi.fn(), artifact: vi.fn() },
  projectAPI: { get: vi.fn(), linkedArtifacts: vi.fn(), parties: vi.fn() },
  agentsAPI: {},
  membersAPI: { list: vi.fn().mockResolvedValue({ data: [] }) },
  metaAPI: {
    attributeDefinitions: vi.fn().mockResolvedValue({ data: [] }),
    artifactTypes: vi.fn().mockResolvedValue({ data: [] }),
    linkTypes: vi.fn().mockResolvedValue({ data: [] }),
  },
}));

vi.mock('../hooks/useViewport', () => ({
  useViewport: () => ({ width: 1280, cls: 'desktop', isPhone: false, isCompact: false, coarsePointer: false }),
}));

vi.mock('../hooks/useFeature', () => ({ useFeature: () => true }));

// The editor stands in as the two actions this suite needs: add a figure,
// then leave the editor, which shows the artifact's header again.
vi.mock('../components/ArtifactEditor', () => ({
  ArtifactEditor: (props: { onUploadAttachment?: (file: File) => void; onCancel: () => void }) => (
    <div>
      <button
        type="button"
        onClick={() => props.onUploadAttachment?.(new File(['png'], 'drawing.png', { type: 'image/png' }))}
      >
        Add figure
      </button>
      <button type="button" onClick={props.onCancel}>
        Leave editor
      </button>
    </div>
  ),
}));

// A stand-in store that re-renders whoever read it on a write, as the real
// one does, with stable setters so the component's callbacks keep their deps.
let selectedArtifactId: string | null = null;
let storeArtifacts: any[] = [];
const listeners = new Set<() => void>();
const notify = () => listeners.forEach((listener) => listener());
const setArtifacts = (next: any[]) => {
  storeArtifacts = next;
  notify();
};
const setSelectedArtifactId = (id: string | null) => {
  selectedArtifactId = id;
  notify();
};
const noop = () => {};
const storeState = () => ({
  projectId: 'p1',
  artifacts: storeArtifacts,
  setArtifacts,
  addArtifact: noop,
  updateArtifact: noop,
  removeArtifact: noop,
  addLink: noop,
  selectedArtifactId,
  setSelectedArtifactId,
});
vi.mock('../state/store', () => ({
  useAppStore: (selector?: (s: any) => unknown) => {
    const [, bump] = React.useState(0);
    React.useEffect(() => {
      const listener = () => bump((n) => n + 1);
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    }, []);
    return selector ? selector(storeState()) : storeState();
  },
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const requirement = (version: number) => ({
  id: 'req-1',
  ref: 'REQ-1',
  project_id: 'p1',
  parent_id: null,
  type: 'requirement',
  title: 'Answer in time',
  body: '',
  sort_order: 1,
  status: 'draft',
  attributes: {},
  version,
  valid_from: '',
  valid_to: null,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
});

const list = vi.mocked(artifactAPI.list, { partial: true, deep: true });
const upload = vi.mocked(attachmentAPI.upload, { partial: true, deep: true });

describe('ModuleView figure upload', () => {
  let container: HTMLDivElement;
  let root: Root;

  const click = async (text: string) => {
    const target = Array.from(container.querySelectorAll('button')).find(
      (b) => (b.textContent ?? '').trim() === text
    );
    expect(target, `a "${text}" button`).toBeTruthy();
    await act(async () => {
      target!.click();
    });
  };

  beforeEach(async () => {
    selectedArtifactId = null;
    storeArtifacts = [];
    listeners.clear();
    list.mockReset();
    upload.mockReset();
    list.mockResolvedValue({ data: [requirement(1)] } as any);
    vi.mocked(artifactAPI.getVersions).mockResolvedValue({ data: [] } as any);
    const { linkAPI, baselineAPI, qualityAPI, projectAPI } = await import('../api/client');
    vi.mocked(linkAPI.list).mockResolvedValue({ data: [] } as any);
    vi.mocked(linkAPI.listForArtifact).mockResolvedValue({ data: [] } as any);
    vi.mocked(attachmentAPI.listByProject).mockResolvedValue({ data: [] } as any);
    vi.mocked(attachmentAPI.listByArtifact).mockResolvedValue({ data: [] } as any);
    vi.mocked(baselineAPI.list).mockResolvedValue({ data: [] } as any);
    vi.mocked(qualityAPI.project).mockResolvedValue({ data: { rows: [] } } as any);
    vi.mocked(qualityAPI.artifact).mockResolvedValue({ data: { findings: [] } } as any);
    vi.mocked(projectAPI.get).mockResolvedValue({ data: { id: 'p1', name: 'P' } } as any);
    vi.mocked(projectAPI.linkedArtifacts).mockResolvedValue({ data: [] } as any);
    vi.mocked(projectAPI.parties).mockResolvedValue({ data: [] } as any);

    container = document.createElement('div');
    document.body.appendChild(container);
    act(() => {
      root = createRoot(container);
    });
    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/projects/p1/requirements?artifact=req-1']}>
          <DialogProvider>
            <Routes>
              <Route path="/projects/:projectId/requirements" element={<ModuleView />} />
            </Routes>
          </DialogProvider>
        </MemoryRouter>
      );
    });
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  it('shows the version adding a figure took the artifact to', async () => {
    expect(container.textContent).toContain('Version 1');
    await click('Edit');
    // The server answers the upload with the figure and has versioned the
    // artifact; the next read of the artifacts says so.
    upload.mockResolvedValue({
      data: { id: 'att-1', artifact_id: 'req-1', filename: 'drawing.png', mime_type: 'image/png' },
    } as any);
    list.mockResolvedValue({ data: [requirement(2)] } as any);
    await click('Add figure');
    expect(upload).toHaveBeenCalledTimes(1);
    await click('Leave editor');
    expect(container.textContent).toContain('Version 2');
    expect(container.textContent).not.toContain('Version 1');
  });
});
