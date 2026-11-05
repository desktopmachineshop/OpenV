// The coverage page's side column: the test run in progress and the gaps
// worth fixing first, with the agent's pending proposals beneath them.
import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import type { Artifact, Baseline, TestResult, TestRun } from '../api/client';
import { Chip } from '../components/ui';
import { runBaselineLabel } from '../utils/baselines';
import {
  StackedBar,
  cardHeadStyle,
  cardStyle,
  cardTitleStyle,
  countResults,
  refStyle,
  resultSegments,
} from './VVDashboardParts';
import { GapGroup, flattenGaps } from './VVDashboardData';
import { useFeature } from '../hooks/useFeature';
import { FLOATING_ASSISTANT_FEATURE, openAssistant } from '../components/wizard/assistantStore';
import { GapForAssistant, draftTestsMessage } from '../components/wizard/floatingAssistantRoute';

const TOP_GAPS = 5;

export const ActiveRunCard: React.FC<{
  projectId: string;
  run: TestRun;
  results?: TestResult[];
  caseCount: number;
  baselines: Baseline[];
}> = ({ projectId, run, results, caseCount, baselines }) => {
  const counts = countResults(results || []);
  return (
    <section
      aria-label="Active test run"
      data-help-key="vv.runs"
      style={{ ...cardStyle, padding: 12, display: 'flex', flexDirection: 'column', gap: 8 }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <h3 style={{ ...cardTitleStyle, flex: 1, minWidth: 0 }}>{run.name}</h3>
        <Chip>{run.status.replace('-', ' ')}</Chip>
      </div>
      <StackedBar segments={resultSegments(counts)} total={caseCount} height={8} />
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap', fontSize: 12, color: 'var(--text-muted)' }}>
        <span>
          {results ? `${counts.recorded} of ${caseCount} recorded` : 'Results loading…'}
        </span>
        <span>
          Started {run.started_at ? new Date(run.started_at).toLocaleDateString() : '—'} · {runBaselineLabel(run, baselines)}
        </span>
      </div>
      <Link
        to={`/projects/${projectId}/vv/runs/${run.id}`}
        style={{ fontSize: 13, fontWeight: 600, color: 'var(--accent-text)', alignSelf: 'flex-start', display: 'inline-flex', alignItems: 'center', minHeight: 32 }}
      >
        Continue run →
      </Link>
    </section>
  );
};

export const GapsCard: React.FC<{
  projectId: string;
  groups: GapGroup[];
  artifacts: Record<string, Artifact>;
  pendingProposals: number;
  isPhone: boolean;
  loading: boolean;
}> = ({ projectId, groups, artifacts, pendingProposals, isPhone, loading }) => {
  const [showAll, setShowAll] = useState(false);
  const items = flattenGaps(groups);
  // With the floating assistant (#728) the card starts drafting in its chat,
  // with the gaps it shows, instead of sending the reader to the Agents page.
  const assistantOn = useFeature(FLOATING_ASSISTANT_FEATURE);
  const forAssistant = (id: string, why: string): GapForAssistant => ({
    id,
    ref: artifacts[id]?.ref,
    title: artifacts[id]?.title || id,
    why,
  });
  const draftTests = (gaps: GapForAssistant[]) =>
    openAssistant({
      message: draftTestsMessage(gaps),
      context: { label: gaps.length === 1 ? `${gaps[0].ref || gaps[0].title} (V&V gap)` : `${gaps.length} V&V gaps` },
    });
  const shownGaps = showAll
    ? groups.flatMap((g) => g.ids.map((id) => forAssistant(id, g.why)))
    : items.slice(0, TOP_GAPS).map((g) => forAssistant(g.id, g.why));
  const askButton = (gap: GapForAssistant) =>
    assistantOn ? (
      <button
        type="button"
        className="vv-gap-ask"
        onClick={() => draftTests([gap])}
        aria-label={`Draft tests for ${gap.ref || gap.title} in the V&V Assistant`}
        title="Draft tests in the V&V Assistant"
        style={{
          flexShrink: 0,
          width: isPhone ? 44 : 30,
          minHeight: isPhone ? 44 : 30,
          border: 'none',
          background: 'none',
          borderRadius: 6,
          color: 'var(--purple-strong)',
          cursor: 'pointer',
          fontSize: 14,
        }}
      >
        ✦
      </button>
    ) : null;
  // A row is the link to the artifact, with the ✦ beside it rather than
  // inside it: a button cannot sit inside a link.
  const gapRow = (key: string, gap: GapForAssistant, children: React.ReactNode) =>
    assistantOn ? (
      <div key={key} className="vv-gap-row" style={{ display: 'flex', alignItems: 'center', borderBottom: '1px solid var(--border-soft)' }}>
        <Link to={link(gap.id)} className="ov-row" style={{ ...row, flex: 1, minWidth: 0, borderBottom: 'none', paddingRight: 4 }}>
          {children}
        </Link>
        {askButton(gap)}
      </div>
    ) : (
      <Link key={key} to={link(gap.id)} className="ov-row" style={row}>
        {children}
      </Link>
    );
  const row: React.CSSProperties = {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    minHeight: isPhone ? 48 : 34,
    padding: '4px 12px',
    borderBottom: '1px solid var(--border-soft)',
    color: 'var(--text)',
    textDecoration: 'none',
    fontSize: 13,
  };
  const link = (id: string) => `/projects/${projectId}/requirements?artifact=${id}`;
  const titleOf = (id: string) => artifacts[id]?.title || id;

  return (
    <section aria-label="Gaps" data-help-key="vv.gaps" style={{ ...cardStyle, display: 'flex', flexDirection: 'column' }}>
      <div style={cardHeadStyle}>
        <h3 style={cardTitleStyle}>{showAll ? 'Gaps' : 'Top gaps'}</h3>
        {items.length > TOP_GAPS && (
          <button
            type="button"
            onClick={() => setShowAll(!showAll)}
            style={{ border: 'none', background: 'none', color: 'var(--accent-text)', fontSize: 12, fontWeight: 600, cursor: 'pointer', minHeight: 32 }}
          >
            {showAll ? 'Top gaps only' : `All ${items.length}`}
          </button>
        )}
      </div>
      {items.length === 0 && !loading && (
        <div style={{ padding: 12, fontSize: 13, color: 'var(--success-text)' }}>No gaps detected — nice work.</div>
      )}
      {!showAll &&
        items.slice(0, TOP_GAPS).map((g) =>
          gapRow(
            g.id,
            forAssistant(g.id, g.why),
            <>
              <span style={{ ...refStyle, width: 64, overflow: 'hidden', textOverflow: 'ellipsis' }}>{artifacts[g.id]?.ref || ''}</span>
              <span style={{ flex: 1, minWidth: 0, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                {titleOf(g.id)}
              </span>
              <span style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>{g.why}</span>
            </>
          )
        )}
      {showAll &&
        groups.map((g) => (
          <div key={g.key}>
            <div
              style={{
                padding: '8px 12px 4px',
                fontSize: 12,
                fontWeight: 600,
                color: 'var(--text-secondary)',
                borderBottom: '1px solid var(--border-soft)',
              }}
            >
              {g.label} <span style={{ color: 'var(--danger-strong)' }}>({g.ids.length})</span>
            </div>
            {g.ids.map((id) =>
              gapRow(
                id,
                forAssistant(id, g.why),
                <>
                  <span style={{ ...refStyle, width: 64, overflow: 'hidden', textOverflow: 'ellipsis' }}>{artifacts[id]?.ref || ''}</span>
                  <span style={{ flex: 1, minWidth: 0 }}>{titleOf(id)}</span>
                </>
              )
            )}
          </div>
        ))}
      {pendingProposals > 0 ? (
        <Link
          to={`/projects/${projectId}/agent-runs`}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 8,
            padding: '10px 12px',
            background: 'var(--tint-purple)',
            borderTop: '1px solid var(--tint-purple-border)',
            borderRadius: '0 0 6px 6px',
            color: 'var(--purple-strong)',
            fontSize: 13,
            textDecoration: 'none',
          }}
        >
          <span aria-hidden="true">✦</span>
          <span style={{ flex: 1 }}>
            {pendingProposals} agent proposal{pendingProposals === 1 ? '' : 's'} waiting for review.
          </span>
          <b style={{ fontWeight: 700 }}>Review</b>
        </Link>
      ) : assistantOn ? (
        items.length > 0 && (
          <button
            type="button"
            onClick={() => draftTests(shownGaps)}
            data-help-key="vv.gaps"
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              width: '100%',
              padding: '10px 12px',
              background: 'var(--tint-purple)',
              border: 'none',
              borderRadius: '0 0 6px 6px',
              color: 'var(--purple-strong)',
              fontSize: 13,
              textAlign: 'left',
              cursor: 'pointer',
            }}
          >
            <span aria-hidden="true">✦</span>
            <span style={{ flex: 1 }}>
              The V&amp;V Assistant can draft test cases for {shownGaps.length === 1 ? 'this gap' : `these ${shownGaps.length} gaps`}.
            </span>
            <b style={{ fontWeight: 700 }}>Draft tests</b>
          </button>
        )
      ) : (
        items.length > 0 && (
          <Link
            to={`/projects/${projectId}/agents`}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              padding: '10px 12px',
              background: 'var(--tint-purple)',
              borderRadius: '0 0 6px 6px',
              color: 'var(--purple-strong)',
              fontSize: 13,
              textDecoration: 'none',
            }}
          >
            <span aria-hidden="true">✦</span>
            <span style={{ flex: 1 }}>An agent can draft test cases for these gaps.</span>
            <b style={{ fontWeight: 700 }}>Agents</b>
          </Link>
        )
      )}
    </section>
  );
};
