import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import type { AgentRun, Proposal } from '../../api/client';
import { ProposalCard } from './ProposalCard';
import { RunStatusFilter, RunTable } from './RunTable';
import { useAppStore } from '../../state/store';

// Deciding from a phone, in the refreshed interface (ui-refresh): a
// proposal's Approve is one press and Reject asks why first; the card's tick
// shows only while several are being chosen; a run is a row of two lines, and
// the status filter a row of chips. Without the key each is as it was.

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const initialWidth = window.innerWidth;
let container: HTMLDivElement;
let root: Root;

const gates = (refresh: boolean) =>
  useAppStore.setState({
    features: {
      channel: 'nightly' as const,
      stable_release: '',
      preview: false,
      features: { 'compact-layout': true, 'ui-refresh': refresh },
    },
  } as never);

const mount = async (ui: React.ReactElement, width = 390) => {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: width });
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => root.render(ui));
};

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: initialWidth });
});

const proposal = {
  id: 'pr1',
  run_id: 'r1',
  project_id: 'p1',
  op: 'update_artifact',
  payload: { title: 'Positioning accuracy' },
  status: 'pending',
  review_note: '',
  created_at: '2026-09-01T09:00:00Z',
} as unknown as Proposal;

const buttons = () => Array.from(container.querySelectorAll('button')).map((b) => (b.textContent ?? '').trim());
const click = async (name: string) => {
  const b = Array.from(container.querySelectorAll('button')).find((x) => (x.textContent ?? '').trim() === name);
  await act(async () => {
    (b as HTMLElement).click();
  });
};

const Card: React.FC<{ onApprove?: () => void; onReject?: () => void; selecting?: boolean }> = ({
  onApprove = () => {},
  onReject = () => {},
  selecting,
}) => {
  const [note, setNote] = React.useState('');
  return (
    <ProposalCard
      proposal={proposal}
      ctx={{ artifacts: {}, links: {} }}
      selected={false}
      onSelect={() => {}}
      note={note}
      onNote={setNote}
      busy={false}
      onApprove={onApprove}
      onReject={onReject}
      selecting={selecting}
    />
  );
};

describe('a proposal card in the refreshed interface', () => {
  it('approves in one press, with no note field and no tick in the way', async () => {
    gates(true);
    const onApprove = vi.fn();
    await mount(<Card onApprove={onApprove} />);
    expect(container.querySelector('input[aria-label="Review note"]')).toBeNull();
    expect(container.querySelector('input[type="checkbox"]')).toBeNull();
    // Reject, then Approve: the decision forward is the last, under the thumb.
    expect(buttons().slice(-2)).toEqual(['Reject', 'Approve']);
    await click('Approve');
    expect(onApprove).toHaveBeenCalledTimes(1);
  });

  it('asks why on the first press of Reject, and rejects on the second', async () => {
    gates(true);
    const onReject = vi.fn();
    await mount(<Card onReject={onReject} />);
    await click('Reject');
    expect(onReject).not.toHaveBeenCalled();
    expect(container.querySelector('input[aria-label="Review note"]')).not.toBeNull();
    await click('Reject');
    expect(onReject).toHaveBeenCalledTimes(1);
  });

  it('opens the note for an approval on request, and shows the tick while several are chosen', async () => {
    gates(true);
    await mount(<Card selecting />);
    expect(container.querySelector('input[type="checkbox"]')).not.toBeNull();
    await click('Add a note');
    expect(container.querySelector('input[aria-label="Review note"]')).not.toBeNull();
    expect(buttons()).not.toContain('Add a note');
  });

  it('is the card as it was without the key', async () => {
    gates(false);
    const onReject = vi.fn();
    await mount(<Card onReject={onReject} />);
    expect(container.querySelector('input[aria-label="Review note"]')).not.toBeNull();
    expect(container.querySelector('input[type="checkbox"]')).not.toBeNull();
    expect(buttons().slice(-2)).toEqual(['Approve', 'Reject']);
    await click('Reject');
    expect(onReject).toHaveBeenCalledTimes(1);
  });
});

const run = (id: string, status: string): AgentRun =>
  ({
    id,
    agent_id: 'a1',
    agent_name: 'Requirements Analyst',
    status,
    tokens_in: 10,
    tokens_out: 5,
    cost_usd: 0.12,
    started_at: '2026-09-01T09:00:00Z',
    finished_at: '2026-09-01T09:01:30Z',
  }) as unknown as AgentRun;

describe('the runs list in the refreshed interface', () => {
  it('lists a run as a row of two lines that opens it', async () => {
    const onSelect = vi.fn();
    await mount(<RunTable runs={[run('r1', 'succeeded'), run('r2', 'failed')]} selectedRunId="r2" onSelect={onSelect} isPhone emptyText="No runs" rows />);
    expect(container.querySelector('table')).toBeNull();
    const rows = Array.from(container.querySelectorAll('[role="listitem"] button'));
    expect(rows).toHaveLength(2);
    expect(rows[0].textContent).toContain('Requirements Analyst');
    expect(rows[0].textContent).toContain('1m 30s');
    expect(rows[0].textContent).toContain('$0.12');
    expect(rows[0].textContent).toContain('succeeded');
    expect(rows[1].getAttribute('aria-current')).toBe('true');
    await act(async () => {
      (rows[0] as HTMLElement).click();
    });
    expect(onSelect).toHaveBeenCalledWith('r1');
  });

  it('says so when there is no run, and is the table as it was without rows', async () => {
    await mount(<RunTable runs={[]} selectedRunId={null} onSelect={() => {}} isPhone emptyText="No runs yet" rows />);
    expect(container.textContent).toContain('No runs yet');
    act(() => root.render(<RunTable runs={[run('r1', 'succeeded')]} selectedRunId={null} onSelect={() => {}} isPhone emptyText="No runs" />));
    expect(container.querySelector('table')).not.toBeNull();
  });

  it('filters by status with chips, All runs first', async () => {
    const onChange = vi.fn();
    await mount(<RunStatusFilter value="all" onChange={onChange} chips />);
    const chips = Array.from(container.querySelectorAll('[role="group"] button'));
    expect(chips[0].textContent).toBe('All runs');
    expect(chips[0].getAttribute('aria-pressed')).toBe('true');
    expect(chips.every((c) => !(c.textContent ?? '').includes('_'))).toBe(true);
    await act(async () => {
      (chips[1] as HTMLElement).click();
    });
    expect(onChange).toHaveBeenCalledTimes(1);
    act(() => root.render(<RunStatusFilter value="all" onChange={onChange} />));
    expect(container.querySelector('select')).not.toBeNull();
  });
});
