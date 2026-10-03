import React from 'react';
import { Modal } from '../../components/ui';
import type { Setter } from './shared';

// Stacked, the toolbar folds into this sheet behind the ⋯ button. Every item
// in it is a one-shot action, so a click that lands inside a button closes it.
// Props-only (refactor plan F7): the ModuleView shell owns the state, the
// effects, the loads and the handlers.
interface PhoneActionsSheetProps {
  setToolsOpen: Setter<boolean>;
  toolbarActions: React.ReactNode;
}

export const PhoneActionsSheet: React.FC<PhoneActionsSheetProps> = ({ setToolsOpen, toolbarActions }) => (
  <Modal title="Requirements" width={400} onClose={() => setToolsOpen(false)}>
    <div className="action-sheet" onClick={(e) => {
      // Any button in the sheet is a one-shot action: close on use.
      if ((e.target as HTMLElement).closest('button')) setToolsOpen(false);
    }}>
      <label style={{ fontSize: 12 }}>Baseline</label>
      {toolbarActions}
    </div>
  </Modal>
);
