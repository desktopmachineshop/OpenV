import React, { useEffect, useMemo, useState } from 'react';
import { Link as RouterLink, useParams, useSearchParams } from 'react-router-dom';
import {
  Baseline,
  BaselineDiff,
  BaselineDiffArtifact,
  BaselineDiffLink,
  BaselineDiffModified,
  baselineAPI,
} from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { ErrorBanner } from '../components/ui';

const typeChipStyle: React.CSSProperties = {
  display: 'inline-block',
  padding: '2px 8px',
  borderRadius: 10,
  background: 'var(--neutral-soft)',
  color: 'var(--text)',
  fontSize: 11,
  fontWeight: 600,
  whiteSpace: 'nowrap',
  flexShrink: 0,
};

const fieldBadgeStyle: React.CSSProperties = {
  display: 'inline-block',
  padding: '1px 7px',
  borderRadius: 8,
  background: 'var(--tint-yellow)',
  border: '1px solid var(--tint-yellow-border)',
  color: 'var(--warning-text)',
  fontSize: 11,
  whiteSpace: 'nowrap',
};

const rowStyle: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: 8,
  padding: '7px 12px',
  borderTop: '1px solid var(--border-soft)',
  fontSize: 13,
  color: 'var(--text)',
  flexWrap: 'wrap',
};

/** Names of the fields a modified entry flags as changed, for the badges. */
function changedFields(entry: BaselineDiffModified): string[] {
  const fields: string[] = [];
  if (entry.title_changed) fields.push('title');
  if (entry.body_changed) fields.push('body');
  if (entry.type_changed) fields.push('type');
  if (entry.status_changed) fields.push('status');
  if (entry.parent_changed) fields.push('parent');
  return fields;
}

interface SectionProps {
  title: string;
  count: number;
  accent: string; // header tint background
  accentBorder: string;
  accentText: string;
  children: React.ReactNode;
}

/** A grouped diff section: tinted header with a count, rows beneath. */
const Section: React.FC<SectionProps> = ({ title, count, accent, accentBorder, accentText, children }) => (
  <div
    style={{
      border: '1px solid var(--border)',
      borderRadius: 6,
      overflow: 'hidden',
      background: 'var(--surface)',
      marginBottom: 16,
    }}
  >
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 8,
        padding: '8px 12px',
        background: accent,
        borderBottom: `1px solid ${accentBorder}`,
        color: accentText,
        fontSize: 13,
        fontWeight: 600,
      }}
    >
      <span>{title}</span>
      <span
        style={{
          background: 'var(--surface)',
          color: accentText,
          borderRadius: 10,
          padding: '0 8px',
          fontSize: 12,
        }}
      >
        {count}
      </span>
    </div>
    {count === 0 ? (
      <div style={{ padding: '8px 12px', fontSize: 13, color: 'var(--text-muted)' }}>None</div>
    ) : (
      children
    )}
  </div>
);

const ArtifactRows: React.FC<{ entries: BaselineDiffArtifact[] }> = ({ entries }) => (
  <div>
    {entries.map((entry) => (
      <div key={entry.id} style={rowStyle}>
        <span style={typeChipStyle}>{entry.type}</span>
        <span style={{ overflowWrap: 'anywhere' }}>{entry.title}</span>
      </div>
    ))}
  </div>
);

const ModifiedRows: React.FC<{ entries: BaselineDiffModified[] }> = ({ entries }) => (
  <div>
    {entries.map((entry) => (
      <div key={entry.id} style={rowStyle}>
        <span style={typeChipStyle}>{entry.type}</span>
        {entry.title_changed ? (
          <span style={{ overflowWrap: 'anywhere' }}>
            <span style={{ color: 'var(--text-muted)', textDecoration: 'line-through' }}>
              {entry.old_title}
            </span>
            <span style={{ color: 'var(--text-muted)' }}> → </span>
            <span>{entry.new_title}</span>
          </span>
        ) : (
          <span style={{ overflowWrap: 'anywhere' }}>{entry.new_title}</span>
        )}
        <span style={{ display: 'inline-flex', gap: 4, flexWrap: 'wrap' }}>
          {changedFields(entry).map((field) => (
            <span key={field} style={fieldBadgeStyle}>
              {field}
            </span>
          ))}
        </span>
      </div>
    ))}
  </div>
);

const LinkRows: React.FC<{ entries: BaselineDiffLink[] }> = ({ entries }) => (
  <div>
    {entries.map((entry) => (
      <div key={`${entry.from_id}|${entry.type}|${entry.to_id}`} style={rowStyle}>
        <span style={{ overflowWrap: 'anywhere' }}>{entry.from_title || entry.from_id}</span>
        <span style={typeChipStyle}>{entry.type} →</span>
        <span style={{ overflowWrap: 'anywhere' }}>{entry.to_title || entry.to_id}</span>
      </div>
    ))}
  </div>
);

/**
 * Baseline compare view: shows what changed from the baseline in the URL
 * ("base") to a selectable target — another baseline or the live project.
 */
export const BaselineCompare: React.FC = () => {
  const { projectId, baselineId } = useParams<{ projectId: string; baselineId: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const against = searchParams.get('against') || 'live';

  const [baselines, setBaselines] = useState<Baseline[]>([]);
  const [diff, setDiff] = useState<BaselineDiff | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!projectId) return;
    baselineAPI
      .list(projectId)
      .then((res) => setBaselines(res.data || []))
      .catch(() => setBaselines([]));
  }, [projectId]);

  useEffect(() => {
    if (!baselineId) return;
    setLoading(true);
    baselineAPI
      .diff(baselineId, against)
      .then((res) => {
        setDiff(res.data);
        setError('');
      })
      .catch((err) => {
        setDiff(null);
        setError(`Failed to compare: ${apiErrorMessage(err)}`);
      })
      .finally(() => setLoading(false));
  }, [baselineId, against]);

  const baseName = useMemo(
    () => diff?.base.name || baselines.find((b) => b.id === baselineId)?.name || baselineId,
    [diff, baselines, baselineId]
  );

  const changeCount = diff
    ? diff.added.length +
      diff.removed.length +
      diff.modified.length +
      diff.links_added.length +
      diff.links_removed.length
    : 0;

  if (!projectId || !baselineId) {
    return (
      <div className="card">
        <h3>No Baseline Selected</h3>
        <p>Open a baseline from the Requirements view to compare it.</p>
      </div>
    );
  }

  return (
    <div style={{ padding: 24, maxWidth: 960 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 14, marginBottom: 6, flexWrap: 'wrap' }}>
        <h2 style={{ color: 'var(--text)', margin: 0 }}>Compare Baseline</h2>
        <div style={{ flex: 1 }} />
        <RouterLink
          to={`/projects/${projectId}/requirements`}
          style={{ fontSize: 13, color: 'var(--accent)' }}
        >
          ← Back to Requirements
        </RouterLink>
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: 10, margin: '10px 0 18px', flexWrap: 'wrap' }}>
        <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>Changes from</span>
        <strong style={{ fontSize: 14, color: 'var(--text)' }}>{baseName}</strong>
        <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>to</span>
        <select
          value={against}
          onChange={(e) => setSearchParams({ against: e.target.value }, { replace: true })}
          title="Compare against"
          style={{ width: 220, padding: '6px 10px' }}
        >
          <option value="live">Live Project</option>
          {baselines
            .filter((b) => b.id !== baselineId)
            .map((b) => (
              <option key={b.id} value={b.id}>
                {b.name}
              </option>
            ))}
        </select>
      </div>

      <ErrorBanner message={error} onDismiss={() => setError('')} />
      {loading && <div style={{ color: 'var(--text-muted)', marginBottom: 10 }}>Comparing…</div>}

      {!loading && diff && changeCount === 0 && (
        <div
          style={{
            background: 'var(--surface-alt)',
            border: '1px solid var(--border)',
            borderRadius: 6,
            padding: '14px 16px',
            fontSize: 13,
            color: 'var(--text-muted)',
          }}
        >
          No differences — these two snapshots are identical.
        </div>
      )}

      {!loading && diff && changeCount > 0 && (
        <>
          <Section
            title="Added"
            count={diff.added.length}
            accent="var(--tint-green)"
            accentBorder="var(--tint-green-border)"
            accentText="var(--success-text)"
          >
            <ArtifactRows entries={diff.added} />
          </Section>

          <Section
            title="Removed"
            count={diff.removed.length}
            accent="var(--tint-red)"
            accentBorder="var(--tint-red-border)"
            accentText="var(--danger-strong)"
          >
            <ArtifactRows entries={diff.removed} />
          </Section>

          <Section
            title="Modified"
            count={diff.modified.length}
            accent="var(--tint-yellow)"
            accentBorder="var(--tint-yellow-border)"
            accentText="var(--warning-text)"
          >
            <ModifiedRows entries={diff.modified} />
          </Section>

          <h3 style={{ color: 'var(--text)', fontSize: 15, margin: '22px 0 12px' }}>Link changes</h3>

          <Section
            title="Links added"
            count={diff.links_added.length}
            accent="var(--tint-green)"
            accentBorder="var(--tint-green-border)"
            accentText="var(--success-text)"
          >
            <LinkRows entries={diff.links_added} />
          </Section>

          <Section
            title="Links removed"
            count={diff.links_removed.length}
            accent="var(--tint-red)"
            accentBorder="var(--tint-red-border)"
            accentText="var(--danger-strong)"
          >
            <LinkRows entries={diff.links_removed} />
          </Section>
        </>
      )}
    </div>
  );
};
