// One milestone of the plan: how far each of its criteria has come, and the
// editor that sets them. A criterion is a metric read from the project's
// artifacts (user needs addressed, requirements verified, ...) and the
// percentage it must reach. A release's milestone edits its criteria here
// and its name and date on the Releases page.
import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import type { PlanCriterion, PlanMilestone, PlanMilestoneInput, ProjectPlan } from '../../api/client';
import { Chip, PanelHeader, buttonStyle, eyebrowStyle, mutedStyle, panelStyle } from '../ui';
import { shortDate } from './ganttScale';
import { milestoneColor, milestoneState } from './planModel';
import { fieldStyle, inputStyle, sectionStyle } from './TaskPanel';

/** The rows of the criteria editor: a metric, its target, and its value in the project now. */
export const CriteriaEditor: React.FC<{
  plan: ProjectPlan;
  value: PlanCriterion[];
  onChange: (next: PlanCriterion[]) => void;
}> = ({ plan, value, onChange }) => {
  const used = new Set(value.map((c) => c.metric));
  const free = plan.metric_catalogue.filter((m) => !used.has(m.key));
  const now = new Map(plan.metrics.map((m) => [m.metric, m]));
  const set = (i: number, patch: Partial<PlanCriterion>) => onChange(value.map((c, j) => (j === i ? { ...c, ...patch } : c)));
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      {value.length === 0 && <span style={mutedStyle}>No criteria: the milestone shows no progress until it has one.</span>}
      {value.map((c, i) => {
        const meta = plan.metric_catalogue.find((m) => m.key === c.metric);
        const cur = now.get(c.metric);
        return (
          <div key={c.metric} style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
              <select
                value={c.metric}
                aria-label={`Criterion ${i + 1}`}
                onChange={(e) => set(i, { metric: e.target.value })}
                style={{ ...inputStyle, flex: 1, minWidth: 0 }}
              >
                {plan.metric_catalogue
                  .filter((m) => m.key === c.metric || !used.has(m.key))
                  .map((m) => (
                    <option key={m.key} value={m.key}>
                      {m.label}
                    </option>
                  ))}
              </select>
              <input
                type="number"
                min={1}
                max={100}
                value={c.target}
                aria-label={`Target for ${meta?.label || c.metric}, percent`}
                onChange={(e) => set(i, { target: Math.min(100, Math.max(1, Number(e.target.value) || 1)) })}
                style={{ ...inputStyle, width: 64, flex: 'none' }}
              />
              <span style={mutedStyle}>%</span>
              <button
                type="button"
                aria-label={`Remove ${meta?.label || c.metric}`}
                onClick={() => onChange(value.filter((_, j) => j !== i))}
                style={buttonStyle('ghost', { compact: true })}
              >
                ✕
              </button>
            </div>
            <span style={mutedStyle}>
              {meta?.description} {cur ? `Across the project now: ${cur.percent}% (${cur.count} of ${cur.total}).` : ''}
            </span>
          </div>
        );
      })}
      {free.length > 0 && (
        <button
          type="button"
          onClick={() => onChange([...value, { metric: free[0].key, target: 100 }])}
          style={{ ...buttonStyle('secondary', { compact: true }), alignSelf: 'flex-start' }}
        >
          + Add criterion
        </button>
      )}
    </div>
  );
};

/** One criterion measured: its name, the count behind it and a bar to its target. */
const CriterionRow: React.FC<{ c: PlanMilestone['criteria'][number] }> = ({ c }) => (
  <div style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
    <div style={{ display: 'flex', gap: 6, alignItems: 'baseline' }}>
      <span style={{ flex: 1, minWidth: 0, fontSize: 13, color: 'var(--text)' }}>{c.label}</span>
      <span style={{ fontSize: 12, fontWeight: 600, color: c.met ? 'var(--success-text)' : 'var(--text-secondary)' }}>
        {c.actual}% <span style={{ fontWeight: 400, color: 'var(--text-muted)' }}>of {c.target}%</span>
      </span>
    </div>
    <span
      role="img"
      aria-label={`${c.label}: ${c.actual}% against a target of ${c.target}%, ${c.count} of ${c.total}${c.met ? ', met' : ''}`}
      style={{ position: 'relative', display: 'block', height: 6, borderRadius: 3, background: 'var(--neutral-soft)', overflow: 'hidden' }}
    >
      <span style={{ display: 'block', height: '100%', width: `${c.actual}%`, background: c.met ? 'var(--success)' : 'var(--accent)' }} />
      {c.target < 100 && <span style={{ position: 'absolute', left: `${c.target}%`, top: 0, bottom: 0, width: 2, background: 'var(--text-secondary)' }} />}
    </span>
    <span style={mutedStyle}>
      {c.total === 0 ? 'Nothing to count yet.' : `${c.count} of ${c.total}`}
      {c.met ? ' · met' : ''}
    </span>
  </div>
);

export interface MilestonePanelProps {
  plan: ProjectPlan;
  milestone: PlanMilestone;
  projectId: string;
  editable: boolean;
  busy: boolean;
  onSave: (m: PlanMilestone, change: PlanMilestoneInput) => void;
  onDelete: (m: PlanMilestone) => void;
  onClose: () => void;
}

export const MilestonePanel: React.FC<MilestonePanelProps> = ({ plan, milestone: m, projectId, editable, busy, onSave, onDelete, onClose }) => {
  const stored = (): PlanCriterion[] => m.criteria.map(({ metric, target }) => ({ metric, target }));
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(m.name);
  const [date, setDate] = useState(m.date || '');
  const [description, setDescription] = useState(m.description);
  const [criteria, setCriteria] = useState<PlanCriterion[]>(stored);
  useEffect(() => {
    setEditing(false);
    setName(m.name);
    setDate(m.date || '');
    setDescription(m.description);
    setCriteria(stored());
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [m.key, m.name, m.date, m.description, JSON.stringify(m.criteria)]);

  const release = m.kind === 'release';
  const state = milestoneState(m);
  const save = () =>
    onSave(m, release ? { criteria } : { name: name.trim(), target_date: date, description, criteria });

  return (
    <aside aria-label={`Milestone ${m.name}`} data-help-key="gantt.milestone" style={{ ...panelStyle, display: 'flex', flexDirection: 'column', flex: '1 0 auto' }}>
      <PanelHeader
        title={release ? 'Release milestone' : 'Milestone'}
        action={
          <button type="button" onClick={onClose} aria-label="Close milestone" style={buttonStyle('ghost', { compact: true })}>
            ✕
          </button>
        }
      />
      <div style={sectionStyle}>
        <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--text)', overflowWrap: 'anywhere' }}>{m.name}</div>
        <div style={{ display: 'flex', gap: 6, alignItems: 'center', flexWrap: 'wrap' }}>
          <Chip tone={state.tone}>{state.label}</Chip>
          <span style={mutedStyle}>{m.date ? shortDate(m.date, plan.today) : 'No date'}</span>
          {release && (
            <Link to={`/projects/${projectId}/releases?release=${m.release_id}`} style={{ fontSize: 12, marginLeft: 'auto' }}>
              Open the release
            </Link>
          )}
        </div>
        {m.description && <span style={{ fontSize: 13, color: 'var(--text-body)', overflowWrap: 'anywhere' }}>{m.description}</span>}
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ flex: 1, height: 8, borderRadius: 4, background: 'var(--neutral-soft)', overflow: 'hidden', display: 'flex' }}>
            <span style={{ width: `${m.progress}%`, background: milestoneColor(m) }} />
          </span>
          <b style={{ fontSize: 14 }}>{m.progress}%</b>
        </div>
        {release && (
          <span style={mutedStyle}>
            {m.release_status === 'released'
              ? 'The release is released, so the milestone is met.'
              : "Its name and date are the release's. Its requirement criteria count the requirements in the release."}
          </span>
        )}
      </div>

      {!editing && (
        <div style={{ ...sectionStyle, borderBottom: 'none' }}>
          <h3 style={eyebrowStyle}>
            Criteria · {m.criteria.filter((c) => c.met).length}/{m.criteria.length} met{m.default_criteria ? ' · defaults' : ''}
          </h3>
          {m.criteria.length === 0 && <span style={mutedStyle}>No criteria set, so there is no progress to show.</span>}
          {m.criteria.map((c) => (
            <CriterionRow key={c.metric} c={c} />
          ))}
          {editable && (
            <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', marginTop: 4 }}>
              <button type="button" onClick={() => setEditing(true)} style={buttonStyle('secondary')}>
                {release ? 'Edit criteria' : 'Edit milestone'}
              </button>
              {(!release || !m.default_criteria) && (
                <button type="button" disabled={busy} onClick={() => onDelete(m)} style={buttonStyle('danger', { disabled: busy })}>
                  {release ? 'Reset to defaults' : 'Delete'}
                </button>
              )}
            </div>
          )}
        </div>
      )}

      {editing && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (release || name.trim()) save();
          }}
          style={{ ...sectionStyle, borderBottom: 'none' }}
        >
          {!release && (
            <>
              <label style={fieldStyle}>
                Name
                <input value={name} onChange={(e) => setName(e.target.value)} required style={inputStyle} />
              </label>
              <label style={fieldStyle}>
                Date
                <input type="date" value={date} onChange={(e) => setDate(e.target.value)} style={inputStyle} />
              </label>
              <label style={fieldStyle}>
                Description
                <input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="What reaching it means" style={inputStyle} />
              </label>
            </>
          )}
          <h3 style={eyebrowStyle}>Criteria</h3>
          <CriteriaEditor plan={plan} value={criteria} onChange={setCriteria} />
          <div style={{ display: 'flex', gap: 6 }}>
            <button type="submit" disabled={busy || (!release && !name.trim())} style={buttonStyle('primary', { disabled: busy || (!release && !name.trim()) })}>
              Save
            </button>
            <button type="button" onClick={() => setEditing(false)} style={buttonStyle('secondary')}>
              Cancel
            </button>
          </div>
        </form>
      )}
    </aside>
  );
};
