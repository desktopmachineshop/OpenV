import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../test/mockApi';
import { interviewsAPI, type Interview } from '../api/client';
import { InterviewEmailInvite } from './InterviewEmailInvite';
import { useAppStore } from '../state/store';

// Email invite on an interview (email-invites): asks for an address and has
// the server email the link, or shows the link when it cannot.

vi.mock('../api/client', async (importOriginal) => mockApi(await importOriginal<typeof import('../api/client')>()));

const prompt = vi.fn();
vi.mock('../components/ui', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../components/ui')>()),
  usePrompt: () => prompt,
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

const interview = (status = 'open'): Interview =>
  ({ id: 'iv-1', project_id: 'p1', name: 'Brake pedal feel', status }) as unknown as Interview;

const gates = (on: boolean) =>
  useAppStore.setState({ features: { channel: 'nightly' as const, stable_release: '', preview: false, features: { 'email-invites': on } } } as never);

const mount = async (ui: React.ReactElement) => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => root.render(ui));
};

const button = () => container.querySelector('button');

afterEach(() => {
  vi.clearAllMocks();
  act(() => root.unmount());
  container.remove();
});

describe('InterviewEmailInvite', () => {
  it('is hidden while the feature is off, and on a closed interview', async () => {
    gates(false);
    await mount(<InterviewEmailInvite interview={interview()} onError={vi.fn()} />);
    expect(button()).toBeNull();
    act(() => root.unmount());
    container.remove();
    gates(true);
    await mount(<InterviewEmailInvite interview={interview('closed')} onError={vi.fn()} />);
    expect(button()).toBeNull();
  });

  it('sends the address to the server and says it was emailed', async () => {
    gates(true);
    prompt.mockResolvedValueOnce(' pat@example.com ');
    vi.mocked(interviewsAPI.createInvite).mockResolvedValue({ data: { path: '/interview/t', emailed: true } } as never);
    await mount(<InterviewEmailInvite interview={interview()} onError={vi.fn()} />);
    expect(button()!.textContent).toBe('Email invite');
    await act(async () => button()!.click());
    expect(interviewsAPI.createInvite).toHaveBeenCalledWith('iv-1', '', 'pat@example.com');
    expect(button()!.textContent).toBe('✓ Emailed');
    expect(prompt).toHaveBeenCalledTimes(1);
  });

  it('shows the link when the server cannot email', async () => {
    gates(true);
    prompt.mockResolvedValueOnce('pat@example.com').mockResolvedValueOnce('');
    vi.mocked(interviewsAPI.createInvite).mockResolvedValue({ data: { path: '/interview/t', emailed: false } } as never);
    await mount(<InterviewEmailInvite interview={interview()} onError={vi.fn()} />);
    await act(async () => button()!.click());
    expect(prompt).toHaveBeenCalledTimes(2);
    expect(prompt.mock.calls[1][0].defaultValue).toBe(`${window.location.origin}/interview/t`);
  });

  it('asks nothing of the server when the dialog is cancelled', async () => {
    gates(true);
    prompt.mockResolvedValueOnce(null);
    await mount(<InterviewEmailInvite interview={interview()} onError={vi.fn()} />);
    await act(async () => button()!.click());
    expect(interviewsAPI.createInvite).not.toHaveBeenCalled();
  });
});
