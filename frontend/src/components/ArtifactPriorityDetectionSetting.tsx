import React, { useEffect, useState } from 'react';
import { projectAPI } from '../api/client';
import { useFeature } from '../hooks/useFeature';
import { EDITOR_PICKERS_FEATURE } from './ui/Picker';

interface Props {
  projectId: string;
  canEdit: boolean;
  flash: (msg: string) => void;
}

/**
 * The project's priority-detection switch, under the quality rules because it
 * reads the same shall, should and may they judge. Off by default: a project
 * that writes every requirement with "shall" would mark them all Must.
 */
export const ArtifactPriorityDetectionSetting: React.FC<Props> = ({ projectId, canEdit, flash }) => {
  const on = useFeature(EDITOR_PICKERS_FEATURE);
  const [enabled, setEnabled] = useState<boolean | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!on || !projectId) return;
    let cancelled = false;
    projectAPI
      .priorityDetection(projectId)
      .then((res) => !cancelled && setEnabled(Boolean(res.data?.enabled)))
      .catch(() => !cancelled && setEnabled(false));
    return () => {
      cancelled = true;
    };
  }, [on, projectId]);

  if (!on || enabled === null) return null;

  const toggle = async (next: boolean) => {
    setSaving(true);
    try {
      const res = await projectAPI.setPriorityDetection(projectId, next);
      setEnabled(Boolean(res.data?.enabled));
      flash(next ? 'Priority now follows the requirement text' : 'Priority detection turned off');
    } catch {
      flash('Could not save priority detection. Try again.');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div style={{ marginTop: 20, paddingTop: 16, borderTop: '1px solid var(--border-soft)' }} data-help-key="settings.priority-detection">
      <h4 style={{ margin: '0 0 6px' }}>Priority from the requirement text</h4>
      <label style={{ display: 'flex', alignItems: 'flex-start', gap: 8, fontWeight: 400 }}>
        <input
          type="checkbox"
          id="priority-detection"
          checked={enabled}
          disabled={!canEdit || saving}
          onChange={(e) => toggle(e.target.checked)}
          style={{ width: 'auto', marginTop: 3 }}
        />
        <span>
          Fill in a requirement's Priority from its wording: <em>shall</em> or <em>must</em> gives Must,{' '}
          <em>should</em> gives Should, <em>may</em> or <em>could</em> gives Could. A priority someone picks
          by hand is kept.
          <span style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>
            Leave this off if every requirement here says “shall”, or they would all be Must.
          </span>
        </span>
      </label>
    </div>
  );
};
