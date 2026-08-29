import React from 'react';
import { QualityScore, QualityFinding } from '../api/client';

/**
 * Rule-based requirement quality UI (issue #217): a compact score badge for
 * artifact rows and a findings panel for the details pane. Presentational and
 * token-styled; data is fetched by callers.
 */

type Band = 'good' | 'fair' | 'poor';

// Token-backed colors per band. Text/background use the shared tint tokens so
// the badge tracks light/dark themes.
const bandStyles: Record<Band, { bg: string; border: string; text: string; label: string }> = {
  good: { bg: 'var(--tint-green)', border: 'var(--tint-green-border)', text: 'var(--success)', label: 'Good' },
  fair: { bg: 'var(--tint-yellow)', border: 'var(--tint-yellow-border)', text: 'var(--warning-text)', label: 'Fair' },
  poor: { bg: 'var(--tint-red)', border: 'var(--tint-red-border)', text: 'var(--danger)', label: 'Poor' },
};

const severityColor: Record<QualityFinding['severity'], string> = {
  error: 'var(--danger)',
  warning: 'var(--warning-text)',
  info: 'var(--text-muted)',
};

function bandOf(band: string): Band {
  return band === 'good' || band === 'fair' || band === 'poor' ? band : 'poor';
}

interface QualityBadgeProps {
  score: number;
  band: string;
  findingCount?: number;
  title?: string;
}

/**
 * Small pill showing a 0-100 quality score, colored by band. Rendered on
 * requirement rows in the module tree.
 */
export const QualityBadge: React.FC<QualityBadgeProps> = ({ score, band, findingCount, title }) => {
  const s = bandStyles[bandOf(band)];
  const tooltip =
    title ??
    `Requirement quality: ${score}/100 (${s.label})` +
      (findingCount !== undefined ? ` · ${findingCount} finding${findingCount === 1 ? '' : 's'}` : '');
  return (
    <span
      title={tooltip}
      aria-label={tooltip}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: '4px',
        backgroundColor: s.bg,
        border: `1px solid ${s.border}`,
        color: s.text,
        borderRadius: '10px',
        padding: '1px 7px',
        fontSize: '10px',
        fontWeight: 700,
        lineHeight: 1.6,
        whiteSpace: 'nowrap',
      }}
    >
      Q {score}
    </span>
  );
};

interface QualityFindingsPanelProps {
  score: QualityScore | null;
  loading?: boolean;
  error?: string;
}

const ruleLabels: Record<string, string> = {
  'weak-word': 'Weak wording',
  'passive-voice': 'Passive voice',
  'not-testable': 'Not testable',
  'vague-quantifier': 'Vague quantifier',
  placeholder: 'Placeholder',
  'long-sentence': 'Long sentence',
};

/**
 * Findings list for the selected requirement, shown in ArtifactDetails. Renders
 * nothing for a non-requirement (score is null and no error).
 */
export const QualityFindingsPanel: React.FC<QualityFindingsPanelProps> = ({ score, loading, error }) => {
  if (loading) {
    return (
      <div style={{ marginTop: '30px', paddingTop: '15px', borderTop: '1px solid var(--border)' }}>
        <h4 style={{ marginTop: 0, marginBottom: '8px' }}>Quality</h4>
        <p style={{ fontSize: '12px', color: 'var(--text-muted)' }}>Checking…</p>
      </div>
    );
  }
  if (error) {
    return (
      <div style={{ marginTop: '30px', paddingTop: '15px', borderTop: '1px solid var(--border)' }}>
        <h4 style={{ marginTop: 0, marginBottom: '8px' }}>Quality</h4>
        <p style={{ fontSize: '12px', color: 'var(--text-muted)' }}>{error}</p>
      </div>
    );
  }
  if (!score) {
    return null;
  }

  return (
    <div style={{ marginTop: '30px', paddingTop: '15px', borderTop: '1px solid var(--border)' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '12px' }}>
        <h4 style={{ margin: 0 }}>Quality</h4>
        <QualityBadge score={score.score} band={score.band} findingCount={score.findings.length} />
        <span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
          {score.findings.length === 0
            ? 'No issues found'
            : `${score.findings.length} finding${score.findings.length === 1 ? '' : 's'}`}
        </span>
      </div>

      {score.findings.length === 0 ? (
        <p style={{ fontSize: '12px', color: 'var(--text-muted)', margin: 0 }}>
          This requirement reads clearly, uses testable phrasing, and has no weak or placeholder wording.
        </p>
      ) : (
        <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: '8px' }}>
          {score.findings.map((f, i) => (
            <li
              key={`${f.rule}-${f.start}-${i}`}
              style={{
                borderLeft: `3px solid ${severityColor[f.severity]}`,
                backgroundColor: 'var(--surface-alt)',
                borderRadius: '2px',
                padding: '8px 10px',
                fontSize: '12px',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '3px' }}>
                <strong style={{ color: severityColor[f.severity] }}>{ruleLabels[f.rule] || f.rule}</strong>
                <span
                  style={{
                    fontSize: '10px',
                    textTransform: 'uppercase',
                    letterSpacing: '0.03em',
                    color: 'var(--text-muted)',
                  }}
                >
                  {f.severity}
                </span>
              </div>
              <div style={{ color: 'var(--text-body)' }}>{f.message}</div>
              {f.match && (
                <code
                  style={{
                    display: 'inline-block',
                    marginTop: '4px',
                    fontSize: '11px',
                    backgroundColor: 'var(--surface-inset)',
                    padding: '1px 5px',
                    borderRadius: '2px',
                    color: 'var(--text)',
                  }}
                >
                  {f.match}
                </code>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
};
