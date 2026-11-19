import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../test/mockApi';
import { projectAPI, type Project } from '../api/client';
import { ProductOverviewCover } from './ProductOverviewCover';
import { ProjectCardWithCover, ProjectCoverImage } from '../components/ProjectListCover';
import { useAppStore } from '../state/store';

// A project's cover photo: added, changed and removed at the top of its
// Overview, and drawn on its card in All projects, behind project-covers.

vi.mock('../api/client', async (importOriginal) => mockApi(await importOriginal<typeof import('../api/client')>()));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

const project = (cover?: string): Project =>
  ({ id: 'p1', name: 'Rover', org_id: 'o1', parent_project_id: '', cover_updated_at: cover }) as unknown as Project;

const gates = (on: boolean) =>
  useAppStore.setState({ features: { channel: 'nightly' as const, stable_release: '', preview: false, features: { 'project-covers': on } } } as never);

const mount = async (ui: React.ReactElement) => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => root.render(ui));
  await act(async () => {});
};

const buttons = () => Array.from(container.querySelectorAll('button')).map((b) => b.textContent);
const click = (label: string) =>
  act(async () => {
    Array.from(container.querySelectorAll('button')).find((b) => b.textContent === label)!.click();
  });

afterEach(() => {
  vi.clearAllMocks();
  act(() => root.unmount());
  container.remove();
});

describe('ProductOverviewCover', () => {
  beforeEach(() => {
    vi.mocked(projectAPI.coverUrl).mockImplementation((id: string, v: string) => `/api/v1/projects/${id}/cover?v=${v}`);
    useAppStore.setState({ projects: [project()] } as never);
  });

  it('shows nothing while the feature is off', async () => {
    gates(false);
    await mount(<ProductOverviewCover projectId="p1" />);
    expect(container.innerHTML).toBe('');
    expect(projectAPI.get).not.toHaveBeenCalled();
  });

  it('offers to add a cover, uploads the chosen file and shows the photo', async () => {
    gates(true);
    vi.mocked(projectAPI.get).mockResolvedValue({ data: project() } as never);
    vi.mocked(projectAPI.uploadCover).mockResolvedValue({ data: project('2026-11-19T10:00:00Z') } as never);
    await mount(<ProductOverviewCover projectId="p1" />);
    expect(buttons()).toEqual(['Add a cover photo']);
    expect(container.querySelector('img')).toBeNull();

    const input = container.querySelector('input[type="file"]') as HTMLInputElement;
    expect(input.accept).toBe('image/png,image/jpeg,image/gif,image/webp');
    // A GIF goes up as it is: prepareCover leaves it unscaled.
    const file = new File(['GIF89a'], 'rover.gif', { type: 'image/gif' });
    Object.defineProperty(input, 'files', { value: [file] });
    await act(async () => {
      input.dispatchEvent(new Event('change', { bubbles: true }));
    });
    await act(async () => {});

    expect(projectAPI.uploadCover).toHaveBeenCalledWith('p1', file, 'rover.gif');
    const img = container.querySelector('img')!;
    expect(img.getAttribute('src')).toBe('/api/v1/projects/p1/cover?v=2026-11-19T10:00:00Z');
    expect(img.alt).toBe('Rover cover photo');
    expect(buttons()).toEqual(['Change cover', 'Remove']);
    // The projects list in the store follows, so All projects shows it too.
    expect(useAppStore.getState().projects[0].cover_updated_at).toBe('2026-11-19T10:00:00Z');
  });

  it('removes the cover', async () => {
    gates(true);
    vi.mocked(projectAPI.get).mockResolvedValue({ data: project('2026-11-19T10:00:00Z') } as never);
    vi.mocked(projectAPI.removeCover).mockResolvedValue({ data: project() } as never);
    await mount(<ProductOverviewCover projectId="p1" />);
    expect(container.querySelector('img')).not.toBeNull();
    await click('Remove');
    expect(projectAPI.removeCover).toHaveBeenCalledWith('p1');
    expect(container.querySelector('img')).toBeNull();
    expect(buttons()).toEqual(['Add a cover photo']);
  });

  it('offers a place on the photo for the figures only while there is a photo', async () => {
    gates(true);
    vi.mocked(projectAPI.get).mockResolvedValue({ data: project('2026-11-19T10:00:00Z') } as never);
    vi.mocked(projectAPI.removeCover).mockResolvedValue({ data: project() } as never);
    const slots: (HTMLDivElement | null)[] = [];
    await mount(<ProductOverviewCover projectId="p1" figuresSlot={(el) => slots.push(el)} />);
    const slot = slots[slots.length - 1];
    expect(slot).not.toBeNull();
    expect(container.querySelector('.ov-overview-cover.has-photo')!.contains(slot)).toBe(true);
    await click('Remove');
    // The photo gone, the slot goes with it, and the figures return to their column.
    expect(slots[slots.length - 1]).toBeNull();
    expect(container.querySelector('.ov-overview-cover.has-photo')).toBeNull();
  });

  it('says why an upload was refused', async () => {
    gates(true);
    vi.mocked(projectAPI.get).mockResolvedValue({ data: project() } as never);
    vi.mocked(projectAPI.uploadCover).mockRejectedValue({
      isAxiosError: true,
      response: { status: 413, data: { error: 'Cover photo is larger than 5 MB' } },
    } as never);
    await mount(<ProductOverviewCover projectId="p1" />);
    const input = container.querySelector('input[type="file"]') as HTMLInputElement;
    Object.defineProperty(input, 'files', { value: [new File(['x'], 'big.gif', { type: 'image/gif' })] });
    await act(async () => {
      input.dispatchEvent(new Event('change', { bubbles: true }));
    });
    await act(async () => {});
    expect(container.querySelector('[role="alert"]')!.textContent).toContain('Cover photo is larger than 5 MB');
    expect(buttons()).toEqual(['Add a cover photo']);
  });
});

describe('ProjectCoverImage', () => {
  beforeEach(() => {
    vi.mocked(projectAPI.coverUrl).mockImplementation((id: string, v: string) => `/api/v1/projects/${id}/cover?v=${v}`);
  });

  it('draws the cover of a project that has one, and nothing otherwise', async () => {
    await mount(
      <div>
        <ProjectCoverImage project={project('t1')} className="project-card-cover" />
        <ProjectCoverImage project={project()} className="project-card-cover" />
      </div>
    );
    const imgs = container.querySelectorAll('img.project-card-cover');
    expect(imgs).toHaveLength(1);
    expect(imgs[0].getAttribute('src')).toBe('/api/v1/projects/p1/cover?v=t1');
    expect(imgs[0].getAttribute('alt')).toBe('');
  });

  it('hides an image that fails to load', async () => {
    await mount(<ProjectCoverImage project={project('t1')} className="project-card-thumb" />);
    await act(async () => {
      container.querySelector('img')!.dispatchEvent(new Event('error'));
    });
    expect(container.querySelector('img')).toBeNull();
  });
});

describe('ProjectCardWithCover', () => {
  beforeEach(() => {
    vi.mocked(projectAPI.coverUrl).mockImplementation((id: string, v: string) => `/api/v1/projects/${id}/cover?v=${v}`);
  });

  it('puts the photo on the left and the content beside it', async () => {
    await mount(
      <ProjectCardWithCover project={project('t1')} side>
        <h3>Rover</h3>
      </ProjectCardWithCover>
    );
    const [side, body] = Array.from(container.children);
    expect(side.tagName).toBe('IMG');
    expect(side.className).toBe('project-card-side');
    expect(body.className).toBe('project-card-body');
    expect(body.querySelector('h3')!.textContent).toBe('Rover');
  });

  it("keeps the column with the project's initial when it has no photo", async () => {
    await mount(
      <ProjectCardWithCover project={{ id: 'p2', name: ' ground station', cover_updated_at: undefined }} side>
        <h3>Ground station</h3>
      </ProjectCardWithCover>
    );
    const tile = container.querySelector('.project-card-side.is-empty')!;
    expect(tile.textContent).toBe('G');
    expect(container.querySelector('img')).toBeNull();
  });

  it('draws the content alone, as before, when no project in the list has a photo', async () => {
    await mount(
      <ProjectCardWithCover project={project('t1')} side={false}>
        <h3>Rover</h3>
      </ProjectCardWithCover>
    );
    expect(container.innerHTML).toBe('<h3>Rover</h3>');
  });
});
