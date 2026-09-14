import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { ImageGallery, figureName, versionKind } from './ImageGallery';
import { Attachment, AttachmentVersion } from '../api/client';

// Figure titles (REQ-157): the gallery shows a figure's name, offers a
// rename where the feature is on and the gallery can change things, and
// reads the history as uploads, renames and new images.

let mockFeatureOn = true;
let mockPromptAnswer: string | null = null;
const mockPromptCalls: any[] = [];

jest.mock('../api/client', () => ({
  attachmentAPI: {
    getDownloadUrl: (id: string, v?: number) => `/dl/${id}/${v || ''}`,
    listVersions: () => Promise.resolve({ data: [] }),
  },
}));

jest.mock('./ui', () => ({
  useAlert: () => () => Promise.resolve(),
  useConfirm: () => () => Promise.resolve(true),
  usePrompt: () => (opts: any) => {
    mockPromptCalls.push(opts);
    return Promise.resolve(mockPromptAnswer);
  },
}));

jest.mock('../hooks/useFeature', () => ({
  useFeature: () => mockFeatureOn,
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const figure = (over: Partial<Attachment> = {}): Attachment => ({
  id: 'f1',
  artifact_id: 'a1',
  filename: 'REQ-1-FIG-1.png',
  original_filename: 'Screenshot 2026-09-14 at 09.12.33.png',
  title: '',
  mime_type: 'image/png',
  file_path: '/u/one.png',
  file_size: 10,
  figure_ref: 'REQ-1-FIG-1',
  figure_num: 1,
  version: 1,
  created_at: '2026-09-14T09:12:33Z',
  ...over,
});

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  mockFeatureOn = true;
  mockPromptAnswer = null;
  mockPromptCalls.length = 0;
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

const render = (props: Partial<React.ComponentProps<typeof ImageGallery>> = {}) => {
  act(() => {
    root.render(<ImageGallery artifactId="a1" attachments={[figure()]} {...props} />);
  });
};

const renameButton = () => container.querySelector('button[aria-label="Rename this figure"]') as HTMLButtonElement | null;

const clickRename = async () => {
  await act(async () => {
    renameButton()!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await Promise.resolve();
  });
};

describe('figure names', () => {
  it('falls back to the uploaded filename until a title is set', () => {
    expect(figureName(figure())).toBe('Screenshot 2026-09-14 at 09.12.33.png');
    expect(figureName(figure({ title: '  Pump curve ' }))).toBe('Pump curve');
    expect(figureName({ filename: 'x.png' })).toBe('x.png');
  });

  it('shows the name under the reference', () => {
    render({ attachments: [figure({ title: 'Pump curve' })] });
    const name = container.querySelector('.gallery-figure-name');
    expect(name?.textContent).toBe('Pump curve');
    expect(container.querySelector('.gallery-filename')?.textContent).toContain('REQ-1-FIG-1');
  });
});

describe('renaming', () => {
  it('asks for a title and hands the trimmed answer to onRename', async () => {
    const onRename = jest.fn();
    mockPromptAnswer = '  Pump curve at 50 Hz ';
    render({ onRename });
    expect(renameButton()).not.toBeNull();
    await clickRename();
    expect(mockPromptCalls[0].defaultValue).toBe('');
    expect(mockPromptCalls[0].placeholder).toBe('Screenshot 2026-09-14 at 09.12.33.png');
    expect(onRename).toHaveBeenCalledWith('f1', 'Pump curve at 50 Hz');
  });

  it('does nothing when the prompt is cancelled or the title is unchanged', async () => {
    const onRename = jest.fn();
    render({ onRename, attachments: [figure({ title: 'Pump curve' })] });
    mockPromptAnswer = null;
    await clickRename();
    mockPromptAnswer = 'Pump curve ';
    await clickRename();
    expect(onRename).not.toHaveBeenCalled();
  });

  it('is offered only while editing, with a handler, and where the feature is on', () => {
    render({ onRename: jest.fn(), readOnly: true });
    expect(renameButton()).toBeNull();
    render({});
    expect(renameButton()).toBeNull();
    mockFeatureOn = false;
    render({ onRename: jest.fn() });
    expect(renameButton()).toBeNull();
  });
});

describe('history', () => {
  const v = (version: number, file_path: string, title: string): AttachmentVersion => ({
    id: `v${version}`,
    attachment_id: 'f1',
    version,
    filename: 'REQ-1-FIG-1.png',
    original_filename: 'one.png',
    title,
    mime_type: 'image/png',
    file_path,
    file_size: 1,
    created_at: '2026-09-14T09:12:33Z',
  });

  it('reads a version as an upload, a rename or a new image', () => {
    const newest = [v(3, '/u/two.png', 'Pump curve'), v(2, '/u/one.png', 'Pump curve'), v(1, '/u/one.png', '')];
    expect(versionKind(newest[0], newest[1])).toBe('new image');
    expect(versionKind(newest[1], newest[2])).toBe('renamed');
    expect(versionKind(newest[2], undefined)).toBe('uploaded');
  });
});

describe('inside the editor form', () => {
  // The gallery sits inside the artifact editor's <form>. A button with no
  // type there is a submit button: "new version" saved the artifact and
  // closed the editor before the file was chosen, and the image never
  // changed. Every gallery button must be an explicit button.
  it('never submits the form it is rendered in', async () => {
    const onSubmit = jest.fn((e: any) => e.preventDefault());
    act(() => {
      root.render(
        <form onSubmit={onSubmit}>
          <ImageGallery
            artifactId="a1"
            attachments={[figure({ version: 2 })]}
            onUploadVersion={jest.fn()}
            onRename={jest.fn()}
            onDelete={jest.fn()}
            showUpload
          />
        </form>
      );
    });
    const buttons = Array.from(container.querySelectorAll('button'));
    expect(buttons.length).toBeGreaterThanOrEqual(4);
    for (const b of buttons) {
      expect(b.getAttribute('type')).toBe('button');
    }
    for (const label of ['Upload a new version of this figure', 'Figure history', 'Rename this figure']) {
      const b = container.querySelector(`button[aria-label="${label}"]`) as HTMLButtonElement;
      expect(b).not.toBeNull();
      await act(async () => {
        b.click();
        await Promise.resolve();
      });
    }
    expect(onSubmit).not.toHaveBeenCalled();
  });
});
