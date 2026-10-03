import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../../test/mockApi';
import { RunDetailPanel } from './RunDetailPanel';
import { agentRunsAPI } from '../../api/client';

// A marker in a run's log is a notice about the run, not its agent's output:
// the worker's own, or a note the server keeps on a finished crew run (a
// hand-off refused, successors the budget did not launch; OpenV REQ-23 and
// REQ-76). The panel shows its message, where it showed the raw payload.

vi.mock('../../api/client', async (orig) =>
  mockApi(await orig(), {
    agentRunsAPI: { streamUrl: (id: string, afterSeq = 0) => `/stream/${id}?after_seq=${afterSeq}` },
  })
);

const api = vi.mocked(agentRunsAPI);

// The smallest EventSource the panel opens: the test hands it log events.
class StubEventSource {
  static last: StubEventSource | null = null;
  listeners: Record<string, ((e: MessageEvent) => void)[]> = {};
  onerror: ((e: MessageEvent) => void) | null = null;
  constructor(public url: string) {
    StubEventSource.last = this;
  }
  addEventListener(type: string, fn: (e: MessageEvent) => void) {
    (this.listeners[type] = this.listeners[type] || []).push(fn);
  }
  close() {}
  emit(type: string, data: unknown) {
    (this.listeners[type] || []).forEach((fn) => fn({ type, data: JSON.stringify(data) } as MessageEvent));
  }
}

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
(globalThis as any).EventSource = StubEventSource as any;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.clearAllMocks();
  api.get.mockResolvedValue({
    data: { id: 'run-1', agent_id: 'a', agent_name: 'Lead', status: 'succeeded', prompt: 'Plan.', tokens_in: 0, tokens_out: 0 },
  } as any);
  api.tree.mockResolvedValue({ data: [] } as any);
  api.logs.mockResolvedValue({ data: [] } as any);
  (Element.prototype as any).scrollIntoView = vi.fn();
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  vi.useRealTimers();
});

const marker = (seq: number, payload: Record<string, unknown>) => ({
  run_id: 'run-1',
  seq,
  kind: 'marker',
  payload,
  created_at: '2026-09-30T10:00:00Z',
});

describe('RunDetailPanel log markers', () => {
  it("shows a marker's message, not its payload", async () => {
    await act(async () => {
      root.render(<RunDetailPanel runId="run-1" onSelectRun={() => undefined} onClose={() => undefined} />);
    });
    const message =
      'Checker and Critic were not launched: this workspace has reached its $1.00 monthly budget ($5.00 spent)';
    await act(async () => {
      StubEventSource.last!.emit(
        'log',
        marker(1, { marker: 'successors_skipped', message, successors: ['Checker', 'Critic'] })
      );
    });
    const text = container.textContent || '';
    expect(text).toContain(message);
    expect(text).not.toContain('successors_skipped');
  });

  it('shows the payload of a marker with no message', async () => {
    await act(async () => {
      root.render(<RunDetailPanel runId="run-1" onSelectRun={() => undefined} onClose={() => undefined} />);
    });
    await act(async () => {
      StubEventSource.last!.emit('log', marker(1, { marker: 'events_dropped', dropped: 3 }));
    });
    expect(container.textContent).toContain('"dropped":3');
  });

  // The server appends its note just after the run's terminal status, on the
  // stream the panel closes at that status, so a panel already open catches
  // the log up when the run finishes: at once, and again a moment later for
  // a note the server was still writing.
  it('shows a note the server keeps once the run has finished, in a panel already open', async () => {
    vi.useFakeTimers();
    api.get.mockResolvedValue({
      data: { id: 'run-1', agent_id: 'a', agent_name: 'Lead', status: 'running', prompt: 'Plan.', tokens_in: 0, tokens_out: 0 },
    } as any);
    await act(async () => {
      root.render(<RunDetailPanel runId="run-1" onSelectRun={() => undefined} onClose={() => undefined} />);
    });
    await act(async () => {
      StubEventSource.last!.emit('log', { run_id: 'run-1', seq: 3, kind: 'text', payload: { text: 'Plan drafted.' } });
    });
    const message =
      'Hand-off to Mia refused: Mia has no role in this project and could not open the card. Give them a role in the project to hand work to them.';
    // The note is not there yet when the status arrives; it is a moment later.
    api.logs
      .mockResolvedValueOnce({ data: [] } as any)
      .mockResolvedValueOnce({ data: [marker(4, { marker: 'handoff_refused', message })] } as any);
    api.get.mockResolvedValue({
      data: { id: 'run-1', agent_id: 'a', agent_name: 'Lead', status: 'succeeded', prompt: 'Plan.', tokens_in: 0, tokens_out: 0 },
    } as any);
    await act(async () => {
      StubEventSource.last!.emit('status', { run_id: 'run-1', status: 'succeeded' });
    });
    expect(api.logs).toHaveBeenCalledTimes(1);
    expect(api.logs).toHaveBeenLastCalledWith('run-1', 3);
    expect(container.textContent).not.toContain(message);

    await act(async () => {
      vi.advanceTimersByTime(2000);
    });
    expect(api.logs).toHaveBeenCalledTimes(2);
    expect(api.logs).toHaveBeenLastCalledWith('run-1', 3);
    expect(container.textContent).toContain(message);

    // Nothing more once the log has settled.
    await act(async () => {
      vi.advanceTimersByTime(60000);
    });
    expect(api.logs).toHaveBeenCalledTimes(2);
  });
});
