import React from 'react';
import { RunDetailPanel } from '../components/agents/RunDetailPanel';
import { Sheet } from '../components/ui';
import { useViewport } from '../hooks/useViewport';

interface AutomationRunDetailProps {
  runId: string;
  onSelectRun: (runId: string) => void;
  onClose: () => void;
}

/**
 * The detail of a run that Run now started for a whole-workspace automation,
 * beside the automations, as the Runs page shows a run beside its list (a
 * sheet over the page on a compact viewport). Such a run has no project, so
 * no project's Runs page lists it: opening it there showed it in the detail
 * panel alone, missing from the list beside it (#379 bug 141). Here it is
 * shown in full: its status, log, answer, cancel and retry.
 */
export const AutomationRunDetail: React.FC<AutomationRunDetailProps> = ({ runId, onSelectRun, onClose }) => {
  const { isCompact } = useViewport();
  const panel = <RunDetailPanel runId={runId} onSelectRun={onSelectRun} onClose={onClose} />;
  if (isCompact) {
    return (
      <Sheet label="Run detail" onClose={onClose}>
        <div style={{ flex: 1, minHeight: 0 }}>{panel}</div>
      </Sheet>
    );
  }
  return (
    <aside aria-label="Run detail" style={{ width: 460, flexShrink: 0, height: '70vh', minHeight: 360 }}>
      {panel}
    </aside>
  );
};
