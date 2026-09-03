import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  qualityRulesAPI,
  QualityConvention,
  QualityRules,
  QualityRuleSet,
  QualitySeverity,
} from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { ErrorBanner } from './ui';

// The requirement quality rule set: which normative vocabulary requirements
// are written in, and how loudly each lint rule speaks. A workspace sets the
// house style; a project inherits it until it overrides it. The rules are
// advisory — they change what the linter reports, never whether a write is
// allowed.
//
// The same editor serves both levels. Neither selector carries an explicit
// "inherit" entry: a control shows what the level actually gets — its own
// override if it has one, otherwise the value it inherits — so the inherited
// value appears once, preselected. Picking it again clears the override, so
// the level goes back to tracking the one above it. At project level a value
// that differs from the workspace's is called out under the control, since
// that is the only remaining sign an override is in play.

const conventionNames: Record<QualityConvention, string> = {
  shall: 'ISO/IEC/IEEE 29148 — "shall"',
  rfc2119: 'RFC 2119 — MUST / SHOULD / MAY',
};

const ruleNames: Record<string, string> = {
  'weak-word': 'Weak wording',
  'vague-quantifier': 'Vague quantifier',
  placeholder: 'Placeholder text',
  'passive-voice': 'Passive voice',
  'long-sentence': 'Over-long sentence',
  'not-testable': 'Not testable',
  'off-convention': 'Off-convention keyword',
};

const severityNames: Record<QualitySeverity, string> = {
  error: 'Error',
  warning: 'Warning',
  info: 'Info',
  off: 'Off',
};

const label: React.CSSProperties = { fontSize: 13, color: 'var(--text)' };
const hint: React.CSSProperties = { fontSize: 12, color: 'var(--text-muted)' };
// Marks a control whose value differs from what the level above sets.
const overrideHint: React.CSSProperties = {
  fontSize: 12,
  color: 'var(--danger)',
  marginTop: 4,
};

interface Props {
  level: 'workspace' | 'project';
  // orgId at workspace level, projectId at project level.
  id: string;
  canEdit: boolean;
  onSaved?: (summary: string) => void;
}

export const QualityRulesEditor: React.FC<Props> = ({ level, id, canEdit, onSaved }) => {
  const [rules, setRules] = useState<QualityRules | null>(null);
  const [draft, setDraft] = useState<QualityRuleSet>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const res =
        level === 'workspace'
          ? await qualityRulesAPI.forWorkspace(id)
          : await qualityRulesAPI.forProject(id);
      setRules(res.data);
      const own = level === 'workspace' ? res.data.workspace : res.data.project;
      setDraft(own ? { convention: own.convention, severities: { ...(own.severities || {}) } } : {});
    } catch (err) {
      setError(apiErrorMessage(err, 'Failed to load quality rules'));
    } finally {
      setLoading(false);
    }
  }, [id, level]);

  useEffect(() => {
    void load();
  }, [load]);

  // What this level inherits when it sets nothing: the workspace's rules at
  // project level, the platform defaults at workspace level.
  const inherited = useMemo(() => {
    if (!rules) return null;
    if (level === 'project' && rules.workspace) {
      return {
        convention: rules.workspace.convention || rules.catalog.defaults.convention,
        severities: rules.workspace.severities || {},
      };
    }
    return { convention: rules.catalog.defaults.convention, severities: {} };
  }, [rules, level]);

  const inheritedSeverity = (rule: string): QualitySeverity => {
    if (inherited?.severities?.[rule]) return inherited.severities[rule];
    return (rules?.catalog.defaults.severities?.[rule] || 'warning') as QualitySeverity;
  };

  const dirty = useMemo(() => {
    if (!rules) return false;
    const own = (level === 'workspace' ? rules.workspace : rules.project) || {};
    const sameConvention = (own.convention || '') === (draft.convention || '');
    const ownSeverities = own.severities || {};
    const draftSeverities = draft.severities || {};
    const keys = new Set([...Object.keys(ownSeverities), ...Object.keys(draftSeverities)]);
    const sameSeverities = [...keys].every((k) => ownSeverities[k] === draftSeverities[k]);
    return !sameConvention || !sameSeverities;
  }, [draft, rules, level]);

  // Choosing the value this level already inherits drops the override instead
  // of storing a copy of it, so the rule keeps following the level above and a
  // later change there still reaches this level.
  const setSeverity = (rule: string, value: string) => {
    setDraft((prev) => {
      const severities = { ...(prev.severities || {}) };
      if (value === inheritedSeverity(rule)) delete severities[rule];
      else severities[rule] = value as QualitySeverity;
      return { ...prev, severities };
    });
  };

  const setConvention = (value: string) => {
    setDraft((prev) => ({
      ...prev,
      convention: (value === inherited?.convention ? undefined : value) as
        | QualityConvention
        | undefined,
    }));
  };

  const save = async () => {
    setSaving(true);
    setError('');
    try {
      // Only what this level actually sets is sent; the rest inherits.
      const payload: QualityRuleSet = {};
      if (draft.convention) payload.convention = draft.convention;
      if (draft.severities && Object.keys(draft.severities).length > 0) {
        payload.severities = draft.severities;
      }
      const res =
        level === 'workspace'
          ? await qualityRulesAPI.setForWorkspace(id, payload)
          : await qualityRulesAPI.setForProject(id, payload);
      setRules(res.data);
      const own = level === 'workspace' ? res.data.workspace : res.data.project;
      setDraft(own ? { convention: own.convention, severities: { ...(own.severities || {}) } } : {});
      onSaved?.(res.data.summary);
    } catch (err) {
      setError(apiErrorMessage(err, 'Failed to save quality rules'));
    } finally {
      setSaving(false);
    }
  };

  if (loading) return <div style={hint}>Loading quality rules…</div>;
  if (!rules || !inherited) return <ErrorBanner message={error} onDismiss={() => setError('')} />;

  // A workspace is the top level anyone sets, so its choices are the house
  // style rather than a deviation; only a project flags what it overrides.
  const overrideNote = (inheritedName: string) =>
    level === 'project' ? (
      <div style={overrideHint}>Workspace default is {inheritedName}</div>
    ) : null;

  const conventionValue = (draft.convention || inherited.convention) as QualityConvention;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      {error && <ErrorBanner message={error} onDismiss={() => setError('')} />}

      <div>
        <h4 style={{ margin: '0 0 4px' }}>Normative convention</h4>
        <p style={{ ...hint, margin: '0 0 10px' }}>
          Which keywords state a binding requirement. The linter judges wording against this, and
          agents read it before drafting — so a project keeps one vocabulary instead of mixing two.
        </p>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {rules.catalog.conventions.map((value) => {
            const selected = conventionValue === value;
            return (
              <label
                key={value}
                style={{
                  display: 'flex',
                  gap: 10,
                  alignItems: 'flex-start',
                  border: '1px solid',
                  borderColor: selected ? 'var(--accent)' : 'var(--border-soft)',
                  borderRadius: 4,
                  padding: '10px 12px',
                  cursor: canEdit ? 'pointer' : 'default',
                  marginBottom: 0,
                  fontWeight: 400,
                }}
              >
                <input
                  type="radio"
                  name={`quality-convention-${level}`}
                  style={{ width: 'auto', marginTop: 3 }}
                  checked={selected}
                  disabled={!canEdit || saving}
                  onChange={() => setConvention(value)}
                />
                <span style={label}>
                  <b>{conventionNames[value]}</b>
                  <br />
                  <span style={hint}>{rules.catalog.labels[value]}</span>
                </span>
              </label>
            );
          })}
        </div>
        {conventionValue !== inherited.convention &&
          overrideNote(conventionNames[inherited.convention as QualityConvention])}
      </div>

      <div>
        <h4 style={{ margin: '0 0 4px' }}>Rules</h4>
        <p style={{ ...hint, margin: '0 0 10px' }}>
          How loudly each check speaks, or Off to silence it. Findings are advisory: they score a
          requirement and explain themselves, and never block a save.
        </p>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
          {rules.catalog.rules.map((rule) => {
            const severity = draft.severities?.[rule] ?? inheritedSeverity(rule);
            return (
              <div
                key={rule}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  gap: 12,
                  padding: '6px 0',
                  borderBottom: '1px solid var(--surface-inset)',
                }}
              >
                <span style={label}>
                  {ruleNames[rule] || rule}
                  <br />
                  <span style={hint}>{rules.catalog.labels[rule]}</span>
                </span>
                <div style={{ width: 160, flex: 'none' }}>
                  <select
                    value={severity}
                    disabled={!canEdit || saving}
                    style={{ width: '100%' }}
                    onChange={(e) => setSeverity(rule, e.target.value)}
                  >
                    {rules.catalog.severities.map((severity) => (
                      <option key={severity} value={severity}>
                        {severityNames[severity]}
                      </option>
                    ))}
                  </select>
                  {severity !== inheritedSeverity(rule) &&
                    overrideNote(severityNames[inheritedSeverity(rule)])}
                </div>
              </div>
            );
          })}
        </div>
      </div>

      <div
        style={{
          background: 'var(--surface-inset)',
          borderRadius: 4,
          padding: '10px 12px',
          fontSize: 12,
          color: 'var(--text-muted)',
        }}
      >
        <b style={{ color: 'var(--text)' }}>In effect now:</b> {rules.summary}
      </div>

      {canEdit && (
        <div style={{ display: 'flex', gap: 8 }}>
          <button onClick={save} disabled={!dirty || saving}>
            {saving ? 'Saving…' : 'Save rules'}
          </button>
          <button className="secondary" onClick={() => void load()} disabled={!dirty || saving}>
            Discard changes
          </button>
          <button
            className="secondary"
            onClick={() => setDraft({})}
            disabled={saving || (!draft.convention && !Object.keys(draft.severities || {}).length)}
            title={`Clear this ${level}'s overrides so everything follows the ${
              level === 'project' ? 'workspace' : 'platform default'
            }`}
          >
            {level === 'project' ? 'Reset to workspace defaults' : 'Reset to platform defaults'}
          </button>
        </div>
      )}
    </div>
  );
};
