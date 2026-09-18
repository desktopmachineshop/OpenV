import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { ReviewQueue } from './ReviewQueue';
import { artifactAPI, attachmentAPI, reviewAPI } from '../api/client';
import { postNote } from '../components/NoteComposer';

vi.mock('../api/client', () => ({
  reviewAPI: { get: vi.fn(), startRound: vi.fn() },
  linkAPI: { confirm: vi.fn() },
  artifactAPI: { changeStatus: vi.fn() },
  attachmentAPI: {
    listByProject: vi.fn(),
    getDownloadUrl: (id: string, v?: number) => `/dl/${id}/${v ?? ''}`,
  },
}));

vi.mock('../state/store', () => ({ useAppStore: (sel: any) => sel({ projectId: 'p1' }) }));
let decisionsOn = true;
vi.mock('../hooks/useFeature', () => ({
  useFeature: (key: string) => (key === 'review-queue-decisions' ? decisionsOn : true),
}));

let confirmAnswer = true;
vi.mock('../components/ui', async () => {
  const actual = await vi.importActual<any>('../components/ui');
  return { ...actual, useConfirm: () => async () => confirmAnswer };
});

// The composer is the notes panel's own, exercised in its own suite. Here it
// stands in as a plain textarea so a rejection reason can be typed.
vi.mock('../components/NoteComposer', () => ({
  NoteComposer: ({ value, onChange, ariaLabel }: any) => (
    <textarea aria-label={ariaLabel} value={value} onChange={(e) => onChange(e.target.value)} />
  ),
  postNote: vi.fn(),
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const get = vi.mocked(reviewAPI.get, { partial: true, deep: true });
const changeStatus = vi.mocked(artifactAPI.changeStatus, { partial: true, deep: true });
const listAttachments = vi.mocked(attachmentAPI.listByProject, { partial: true, deep: true });
const note = vi.mocked(postNote, { partial: true, deep: true });

const ARTIFACTS = [
  {
    id: 'a1',
    ref: 'REQ-12',
    type: 'requirement',
    title: 'Seal integrity',
    body: '## Heading\n\nThe **seal** shall hold 6 bar for [ten minutes](http://x).',
    status: 'in_review',
  },
  { id: 'a2', ref: 'REQ-13', type: 'requirement', title: 'Purge cycle', body: '', status: 'in_review' },
  { id: 'a3', ref: 'HDG-1', type: 'heading', title: 'Vacuum system', body: '', status: 'in_review' },
];

const FIGURES = [
  { id: 'f1', artifact_id: 'a1', kind: 'image', version: 2, figure_ref: 'REQ-12-FIG-1', title: 'Seal detail' },
  { id: 'f2', artifact_id: 'a1', kind: 'cad', version: 1, figure_ref: 'REQ-12-FIG-2', title: 'Body' },
];

describe('ReviewQueue decisions', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    confirmAnswer = true;
    decisionsOn = true;
    [get, changeStatus, listAttachments, note].forEach((m) => m.mockReset());
    get.mockResolvedValue({
      data: { suspect_links: [], in_review_artifacts: ARTIFACTS },
    } as any);
    listAttachments.mockResolvedValue({ data: FIGURES } as any);
    changeStatus.mockResolvedValue({ data: {} } as any);
    note.mockResolvedValue({ id: 'c1' } as any);
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
          <ReviewQueue />
        </MemoryRouter>,
      );
    });
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
  };

  // "Send back" is on every row AND inside the bulk bar AND in the dialog, so
  // every lookup here says exactly which one it means.
  const exact = (root: ParentNode, text: string) =>
    Array.from(root.querySelectorAll('button')).filter(
      (b) => (b.textContent || '').trim() === text,
    );
  const buttons = (text: string) =>
    Array.from(container.querySelectorAll('button')).filter((b) =>
      (b.textContent || '').includes(text),
    );
  /** A row's own Approve / Send back, in table order. */
  const rowButton = (row: number, text: string) => {
    const rows = Array.from(container.querySelectorAll('tbody tr'));
    return exact(rows[row], text)[0];
  };
  const dialog = () => container.querySelector('[role="dialog"]');
  const dialogButton = (text: string) => exact(dialog()!, text)[0];

  // React tracks a controlled textarea's value on the node, so a plain
  // assignment is ignored; go through the native setter and fire "input".
  const setValue = Object.getOwnPropertyDescriptor(
    window.HTMLTextAreaElement.prototype,
    'value',
  )!.set!;
  const typeReason = async (text: string) => {
    const el = container.querySelector<HTMLTextAreaElement>(
      'textarea[aria-label="Reason for sending back"]',
    )!;
    await act(async () => {
      setValue.call(el, text);
      el.dispatchEvent(new Event('input', { bubbles: true }));
    });
    return el;
  };

  const checkbox = (label: string) =>
    container.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!;

  const click = async (el: Element) => {
    await act(async () => {
      el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
  };

  it('shows each artifact with its description and figure previews', async () => {
    await mount();
    // The body is markdown; a reviewer scanning the table wants the sentence.
    expect(container.textContent).toContain('The seal shall hold 6 bar for ten minutes');
    expect(container.textContent).not.toContain('**');
    expect(container.textContent).toContain('REQ-12');
    // An image is shown as one; a CAD file gets a chip, not a broken thumbnail.
    const img = container.querySelector<HTMLImageElement>('img[alt="REQ-12-FIG-1"]');
    expect(img?.getAttribute('src')).toBe('/dl/f1/2');
    expect(container.textContent).toContain('REQ-12-FIG-2');
    // An artifact with no body says so rather than showing an empty cell.
    expect(container.textContent).toContain('No description.');
  });

  it('approves one artifact inline and drops it from the queue', async () => {
    await mount();
    await click(rowButton(0, 'Approve'));
    expect(changeStatus).toHaveBeenCalledWith('a1', 'approved');
    expect(container.textContent).not.toContain('Seal integrity');
    // The others are untouched.
    expect(container.textContent).toContain('Purge cycle');
  });

  it('asks for a reason before sending one back, then posts it and drafts it', async () => {
    await mount();
    await click(rowButton(0, 'Send back'));

    expect(
      container.querySelector('textarea[aria-label="Reason for sending back"]'),
    ).toBeTruthy();
    // The reason is required: the dialog's own button stays disabled until
    // one is typed.
    expect(dialogButton('Send back').disabled).toBe(true);

    await typeReason('Needs a tolerance. @dana');
    await click(dialogButton('Send back'));

    // The reason goes in as an ordinary note, through the notes path, so
    // mentions and to-dos work exactly as they do in the panel.
    expect(note).toHaveBeenCalledWith(
      expect.objectContaining({ artifactId: 'a1', message: 'Needs a tolerance. @dana' }),
    );
    expect(changeStatus).toHaveBeenCalledWith('a1', 'draft');
    expect(container.textContent).not.toContain('Seal integrity');
  });

  it('posts the reason before moving the status, so a rejection is never silent', async () => {
    note.mockRejectedValue(new Error('note failed'));
    await mount();
    await click(rowButton(0, 'Send back'));
    await typeReason('No good');
    await click(dialogButton('Send back'));

    // The note failed, so nothing was rejected: the author never gets an
    // artifact back in draft with no word on why.
    expect(changeStatus).not.toHaveBeenCalled();
    expect(container.textContent).toContain('could not be sent back');
    expect(container.textContent).toContain('Seal integrity');
  });

  it('select-all ticks every row and bulk approve applies to the selection', async () => {
    await mount();
    await act(async () => {
      checkbox('Select all artifacts in review').dispatchEvent(
        new MouseEvent('click', { bubbles: true }),
      );
    });
    expect(buttons('Approve selected (3)').length).toBe(1);

    await click(buttons('Approve selected (3)')[0]);
    expect(changeStatus).toHaveBeenCalledTimes(3);
    ['a1', 'a2', 'a3'].forEach((id) =>
      expect(changeStatus).toHaveBeenCalledWith(id, 'approved'),
    );
    expect(container.textContent).toContain('Nothing is in review right now.');
  });

  it('bulk send-back posts the one reason on every selected artifact', async () => {
    await mount();
    await act(async () => {
      checkbox('Select Seal integrity').dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await act(async () => {
      checkbox('Select Purge cycle').dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    await click(buttons('Send back selected (2)')[0]);
    await typeReason('Both need units.');
    await click(dialogButton('Send back'));

    expect(note).toHaveBeenCalledTimes(2);
    expect(changeStatus).toHaveBeenCalledWith('a1', 'draft');
    expect(changeStatus).toHaveBeenCalledWith('a2', 'draft');
    // The unselected one is still waiting.
    expect(container.textContent).toContain('Vacuum system');
  });

  it('declining the bulk approve confirmation changes nothing', async () => {
    confirmAnswer = false;
    await mount();
    await act(async () => {
      checkbox('Select all artifacts in review').dispatchEvent(
        new MouseEvent('click', { bubbles: true }),
      );
    });
    await click(buttons('Approve selected (3)')[0]);
    expect(changeStatus).not.toHaveBeenCalled();
  });

  it('reports a refused approval and keeps the artifact in the queue', async () => {
    changeStatus.mockRejectedValue({ response: { status: 403 } });
    await mount();
    await click(rowButton(0, 'Approve'));
    expect(container.textContent).toContain('could not be approved');
    expect(container.textContent).toContain('Seal integrity');
  });

  it('still lists the queue when the figures cannot be read', async () => {
    listAttachments.mockRejectedValue(new Error('nope'));
    await mount();
    expect(container.textContent).toContain('Seal integrity');
  });

  // A workspace the controls have not reached yet keeps the list it had, with
  // the artifacts still reachable — a gate must not hide a colleague's work.
  it('falls back to the plain list before the feature reaches the workspace', async () => {
    decisionsOn = false;
    await mount();
    expect(container.textContent).toContain('Seal integrity');
    expect(container.querySelector('input[aria-label="Select all artifacts in review"]')).toBeNull();
    expect(exact(container, 'Approve').length).toBe(0);
    expect(buttons('Approve selected').length).toBe(0);
  });
});
