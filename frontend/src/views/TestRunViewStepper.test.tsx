import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import type { TestRun } from '../api/client';
import { TestRunStepper } from './TestRunViewStepper';
import type { ResultRow } from './TestRunViewCells';

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const run = { id: 'tr1', project_id: 'p1', name: 'Bench run', description: '', status: 'in-progress', started_at: '' } as TestRun;
const row = (id: string, status = 'not-run'): ResultRow => ({
  testCaseId: id,
  ref: id.toUpperCase(),
  testCaseTitle: `Case ${id}`,
  body: 'Do the thing.',
  executionMethod: 'physical',
  verifies: ['REQ-1'],
  versionTested: null,
  status,
  notes: '',
  executedAt: null,
  byAgent: false,
  resultId: status === 'not-run' ? null : `res-${id}`,
  citations: [],
});

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

const button = (name: string) =>
  Array.from(container.querySelectorAll('button')).find((b) => b.textContent === name || b.getAttribute('aria-label') === name)!;

it('records the outcome picked, then moves to the next test case', async () => {
  const onRecord = vi.fn().mockResolvedValue('res-new');
  const onIndex = vi.fn();
  await act(async () => {
    root.render(
      <TestRunStepper run={run} rows={[row('tc1'), row('tc2')]} index={0} onIndex={onIndex} readOnly={false} onClose={() => {}} onRecord={onRecord} onCite={() => {}} />
    );
  });
  expect(container.textContent).toContain('verifies REQ-1');
  await act(async () => button('Fail').click());
  expect(button('Fail').getAttribute('aria-pressed')).toBe('true');
  await act(async () => button('Record and next').click());
  expect(onRecord).toHaveBeenCalledWith(expect.objectContaining({ testCaseId: 'tc1' }), 'fail', '');
  expect(onIndex).toHaveBeenCalledWith(1);
});

it('skips an untouched test case without recording it, and is read-only on a closed run', async () => {
  const onRecord = vi.fn();
  const onIndex = vi.fn();
  await act(async () => {
    root.render(
      <TestRunStepper run={run} rows={[row('tc1'), row('tc2', 'pass')]} index={0} onIndex={onIndex} readOnly={false} onClose={() => {}} onRecord={onRecord} onCite={() => {}} />
    );
  });
  await act(async () => button('Record and next').click());
  expect(onRecord).not.toHaveBeenCalled();
  expect(onIndex).toHaveBeenCalledWith(1);

  await act(async () => {
    root.render(
      <TestRunStepper run={{ ...run, status: 'completed' }} rows={[row('tc1', 'pass')]} index={0} onIndex={onIndex} readOnly onClose={() => {}} onRecord={onRecord} onCite={() => {}} />
    );
  });
  expect((button('Fail') as HTMLButtonElement).disabled).toBe(true);
  expect(button('Done')).toBeTruthy();
});
