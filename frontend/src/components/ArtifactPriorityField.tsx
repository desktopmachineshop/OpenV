import React, { useEffect, useMemo, useState } from 'react';
import { projectAPI } from '../api/client';
import { useFeature } from '../hooks/useFeature';
import { EDITOR_PICKERS_FEATURE, Picker } from './ui/Picker';
import { PRIORITY_OPTIONS, detectPriority, priorityLabel } from './artifactPriority';

interface Props {
  projectId?: string;
  /** The requirement's title and description, read for its keyword. */
  text: string;
  value: string;
  /**
   * The artifact already had a priority when the form opened: it was chosen
   * (or detected and saved) before, so detection leaves it alone.
   */
  initiallySet: boolean;
  onChange: (value: string | undefined) => void;
}

/**
 * The requirement's built-in Priority field (MoSCoW). When the project has
 * priority detection on, the value follows the strongest shall, should or may
 * in the text until someone picks one; the hint says where it came from and
 * offers to follow the text again after a pick that disagrees with it.
 */
export const ArtifactPriorityField: React.FC<Props> = ({ projectId, text, value, initiallySet, onChange }) => {
  const on = useFeature(EDITOR_PICKERS_FEATURE);
  const [detectOn, setDetectOn] = useState(false);
  const [manual, setManual] = useState(initiallySet);

  useEffect(() => {
    if (!on || !projectId) return;
    let cancelled = false;
    projectAPI
      .priorityDetection(projectId)
      .then((res) => !cancelled && setDetectOn(Boolean(res.data?.enabled)))
      .catch(() => !cancelled && setDetectOn(false));
    return () => {
      cancelled = true;
    };
  }, [on, projectId]);

  const detected = useMemo(() => (detectOn ? detectPriority(text) : null), [detectOn, text]);
  const following = detectOn && !manual;

  useEffect(() => {
    if (!following) return;
    const next = detected?.priority || '';
    if (next !== value) onChange(next || undefined);
  }, [following, detected, value, onChange]);

  if (!on) return null;

  let hint: React.ReactNode = null;
  if (following) {
    hint = detected ? (
      <>From <strong>“{detected.word}”</strong> in the text. Pick another to override it.</>
    ) : (
      'No shall, should or may in the text yet, so there is nothing to detect.'
    );
  } else if (detectOn && detected && value !== detected.priority) {
    hint = (
      <>
        Set by hand. The text says <strong>“{detected.word}”</strong>.{' '}
        <button type="button" onClick={() => setManual(false)} style={linkButton}>
          Use {priorityLabel(detected.priority)}
        </button>
      </>
    );
  }

  return (
    <div className="form-group" data-help-key="requirements.priority">
      <label htmlFor="priority" style={{ display: 'flex', alignItems: 'baseline', gap: 8 }}>
        Priority
        {following && detected && <span style={autoChip}>auto</span>}
      </label>
      <Picker
        id="priority"
        value={value}
        noneLabel="Not set"
        options={PRIORITY_OPTIONS}
        aria-describedby={hint ? 'priority-hint' : undefined}
        onChange={(v) => {
          setManual(!(detectOn && detected && v === detected.priority));
          onChange(v || undefined);
        }}
      />
      {hint && (
        <small id="priority-hint" aria-live="polite" style={{ display: 'block', marginTop: 4, fontSize: 12, color: 'var(--text-muted)' }}>
          {hint}
        </small>
      )}
    </div>
  );
};

const autoChip: React.CSSProperties = {
  fontSize: 10, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '.06em', color: 'var(--accent-text)',
  background: 'color-mix(in srgb, var(--accent) 14%, transparent)', borderRadius: 4, padding: '1px 6px',
};
const linkButton: React.CSSProperties = {
  border: 0, background: 'none', padding: 0, font: 'inherit', color: 'var(--accent-text)',
  textDecoration: 'underline', textUnderlineOffset: 2, cursor: 'pointer',
};
