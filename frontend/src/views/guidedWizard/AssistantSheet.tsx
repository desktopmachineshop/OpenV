import React from 'react';
import { GuidedChatPanel, GuidedChatPanelHandle, CopilotSuggestion } from '../../components/wizard/GuidedChatPanel';
import { WizardAnswers } from './wizardAnswers';

interface AssistantSheetProps {
  assistantOpen: boolean;
  setAssistantOpen: (next: boolean) => void;
  /** The visual viewport, which the sheet follows above the keyboard. */
  visual: { height: number; keyboardInset: number };
  chatRef: React.Ref<GuidedChatPanelHandle>;
  sessionId: string;
  step: number;
  getState: () => WizardAnswers;
  applied: Record<string, boolean>;
  onApplySuggestions: (items: { suggestion: CopilotSuggestion; key: string }[]) => Promise<(string | null)[]>;
}

/**
 * The assistant on a phone or tablet: a bottom sheet behind a floating
 * button. The panel stays mounted while the sheet is closed, so its
 * conversation and the ref the steps nudge live on.
 */
export const AssistantSheet: React.FC<AssistantSheetProps> = ({
  assistantOpen,
  setAssistantOpen,
  visual,
  chatRef,
  sessionId,
  step,
  getState,
  applied,
  onApplySuggestions,
}) => {
  return (
    <>
      {assistantOpen && (
        <div
          onClick={() => setAssistantOpen(false)}
          style={{ position: 'fixed', inset: 0, background: 'var(--overlay)', zIndex: 949 }}
        />
      )}
      <div
        role="dialog"
        aria-label="V&V Assistant"
        aria-hidden={!assistantOpen}
        className="safe-area-bottom"
        style={{
          position: 'fixed',
          left: 0,
          right: 0,
          bottom: visual.keyboardInset,
          height: Math.round(visual.height * (visual.keyboardInset > 0 ? 0.95 : 0.75)),
          display: assistantOpen ? 'flex' : 'none',
          flexDirection: 'column',
          background: 'var(--surface)',
          borderTop: '1px solid var(--border)',
          borderRadius: '12px 12px 0 0',
          boxShadow: '0 -4px 16px rgba(0,0,0,0.2)',
          zIndex: 950,
        }}
      >
        <button
          type="button"
          aria-label="Close assistant"
          onClick={() => setAssistantOpen(false)}
          style={{
            alignSelf: 'flex-end',
            width: 44,
            height: 44,
            background: 'none',
            border: 'none',
            fontSize: 20,
            color: 'var(--text-muted)',
            cursor: 'pointer',
          }}
        >
          ✕
        </button>
        <div style={{ flex: 1, minHeight: 0 }}>
          <GuidedChatPanel
            ref={chatRef}
            embedded
            sessionId={sessionId}
            step={step}
            getState={getState}
            applied={applied}
            onApplySuggestions={onApplySuggestions}
          />
        </div>
      </div>
      {!assistantOpen && (
        <button
          type="button"
          onClick={() => setAssistantOpen(true)}
          title="Open the V&V Assistant"
          style={{
            position: 'fixed',
            right: 16,
            bottom: 'calc(16px + env(safe-area-inset-bottom, 0px))',
            minHeight: 48,
            padding: '0 18px',
            borderRadius: 24,
            border: 'none',
            background: 'var(--accent)',
            color: 'var(--accent-fg)',
            fontSize: 14,
            fontWeight: 600,
            boxShadow: '0 4px 12px rgba(0,0,0,0.25)',
            cursor: 'pointer',
            zIndex: 940,
          }}
        >
          💬 Assistant
        </button>
      )}
    </>
  );
};
