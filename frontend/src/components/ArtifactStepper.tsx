import React, { useRef } from 'react';

// Stepping between artifacts is gated like any other new control. What it
// changes is navigation — the same selection a tap on the tree makes, written
// to the same place — so nothing it does becomes unreadable in a workspace that
// has not received the controls yet.
export const ARTIFACT_STEPPING_FEATURE = 'artifact-stepping';

export interface ArtifactStepperProps {
  /** 1-based place in the document, and how many artifacts there are. */
  position: number;
  total: number;
  /** What the artifact is, for the announcement a screen reader hears. */
  label: string;
  onStep: (delta: 1 | -1) => void;
  /** Bigger controls where a finger has to hit them. */
  touch?: boolean;
  /** Shown in the tooltip on a pointer device, where the keys apply. */
  showKeyHints?: boolean;
}

/**
 * The ‹ 12 of 148 › control above an artifact.
 *
 * It is the discoverable half of the feature: J and K are faster once known,
 * and a swipe is faster still on a phone, but neither announces itself, and the
 * count is the part that answers "how much of this is left".
 */
export const ArtifactStepper: React.FC<ArtifactStepperProps> = ({
  position,
  total,
  label,
  onStep,
  touch = false,
  showKeyHints = false,
}) => {
  const previousRef = useRef<HTMLButtonElement>(null);
  const nextRef = useRef<HTMLButtonElement>(null);
  const hasPrevious = position > 1;
  const hasNext = position < total;

  // A button that disables itself under the pointer drops focus to the
  // document, which strands anyone working from the keyboard at the end of the
  // list. Hand focus to the button that is still live instead.
  const step = (delta: 1 | -1) => {
    onStep(delta);
    const reachedEnd = delta === 1 ? position + 1 >= total : position - 1 <= 1;
    if (reachedEnd) (delta === 1 ? previousRef : nextRef).current?.focus();
  };

  const size = touch ? 44 : 28;
  const buttonStyle: React.CSSProperties = {
    minWidth: size,
    minHeight: size,
    padding: 0,
    lineHeight: 1,
    fontSize: touch ? 20 : 16,
  };

  return (
    <div
      style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 8 }}
      aria-label="Artifact navigation"
      role="group"
    >
      <button
        type="button"
        ref={previousRef}
        className="button-secondary"
        style={buttonStyle}
        aria-label="Previous artifact"
        title={showKeyHints ? 'Previous artifact (K)' : 'Previous artifact'}
        disabled={!hasPrevious}
        onClick={() => step(-1)}
      >
        ‹
      </button>
      <button
        type="button"
        ref={nextRef}
        className="button-secondary"
        style={buttonStyle}
        aria-label="Next artifact"
        title={showKeyHints ? 'Next artifact (J)' : 'Next artifact'}
        disabled={!hasNext}
        onClick={() => step(1)}
      >
        ›
      </button>
      {/* Tabular figures so "9 of 148" becoming "10 of 148" does not shift the
          buttons under a thumb that is about to press again. */}
      <span
        aria-hidden="true"
        style={{
          color: 'var(--text-muted)',
          fontSize: 12,
          fontVariantNumeric: 'tabular-nums',
        }}
      >
        {position} of {total}
      </span>
      {/* Stepping replaces the document without moving focus, so a screen
          reader would otherwise say nothing at all. The announcement names what
          was landed on as well as the count: "13 of 148" alone tells a reader
          the one thing they could already work out. */}
      <span className="sr-only" aria-live="polite" aria-atomic="true">
        {`${label}, ${position} of ${total}`}
      </span>
    </div>
  );
};

export default ArtifactStepper;
