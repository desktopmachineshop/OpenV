import React from 'react';

interface StepShellProps {
  steps: string[];
  /** 1-based current step */
  current: number;
  /** highest step the user has reached (for enabling rail clicks) */
  maxReached: number;
  onSelectStep?: (step: number) => void;
  onBack?: () => void;
  onNext?: () => void;
  nextLabel?: string;
  backDisabled?: boolean;
  nextDisabled?: boolean;
  busy?: boolean;
  /** hide the Back/Next footer entirely (e.g. review step provides its own actions) */
  hideNav?: boolean;
  /** optional extra action rendered between Back and Next (e.g. Skip) */
  extraAction?: React.ReactNode;
  children: React.ReactNode;
}

// Left progress rail + step content + Back/Next navigation for the guided wizard.
export const StepShell: React.FC<StepShellProps> = ({
  steps,
  current,
  maxReached,
  onSelectStep,
  onBack,
  onNext,
  nextLabel,
  backDisabled,
  nextDisabled,
  busy,
  hideNav,
  extraAction,
  children,
}) => {
  return (
    <div style={{ display: 'flex', gap: 24, alignItems: 'flex-start' }}>
      <div
        style={{
          width: 220,
          minWidth: 220,
          background: '#fff',
          border: '1px solid #ddd',
          borderRadius: 4,
          padding: '12px 0',
          position: 'sticky',
          top: 20,
        }}
      >
        {steps.map((label, i) => {
          const stepNum = i + 1;
          const isCurrent = stepNum === current;
          const isDone = stepNum < current;
          const clickable = onSelectStep && stepNum <= maxReached && !busy;
          return (
            <div
              key={label}
              onClick={() => clickable && onSelectStep(stepNum)}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 10,
                padding: '8px 14px',
                cursor: clickable ? 'pointer' : 'default',
                background: isCurrent ? '#eaf4fc' : 'transparent',
                borderLeft: isCurrent ? '3px solid #3498db' : '3px solid transparent',
              }}
            >
              <div
                style={{
                  width: 24,
                  height: 24,
                  minWidth: 24,
                  borderRadius: '50%',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  fontSize: 12,
                  fontWeight: 700,
                  color: isCurrent || isDone ? '#fff' : '#7f8c8d',
                  background: isDone ? '#27ae60' : isCurrent ? '#3498db' : '#ecf0f1',
                }}
              >
                {isDone ? '✓' : stepNum}
              </div>
              <div
                style={{
                  fontSize: 13,
                  color: isCurrent ? '#2c3e50' : isDone ? '#2c3e50' : '#7f8c8d',
                  fontWeight: isCurrent ? 600 : 400,
                }}
              >
                {label}
              </div>
            </div>
          );
        })}
      </div>
      <div style={{ flex: 1, minWidth: 0 }}>
        {children}
        {!hideNav && (
          <div
            style={{
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'center',
              marginTop: 20,
              gap: 10,
            }}
          >
            <button
              className="button-secondary"
              onClick={onBack}
              disabled={backDisabled || busy}
              style={{ opacity: backDisabled || busy ? 0.5 : 1 }}
            >
              ← Back
            </button>
            <div style={{ display: 'flex', gap: 10 }}>
              {extraAction}
              <button
                className="button"
                onClick={onNext}
                disabled={nextDisabled || busy}
                style={{
                  background: '#3498db',
                  opacity: nextDisabled || busy ? 0.5 : 1,
                }}
              >
                {busy ? 'Saving…' : nextLabel || 'Next →'}
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
};
