// The Releases page: a project's target releases (MVP, V1.0, ...) in order,
// each with its progress against the requirements it holds, and one
// release's detail: its counts, a burn-up of verification over time and its
// requirements. A requirement is in the release it is introduced in and every
// later one, until its last release when it has one; see
// internal/domain/targetreleases.
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { releasesAPI, ReleaseOverview, ReleaseProgress, ReleaseSummaryCounts, TargetRelease } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { ErrorBanner, useConfirm } from '../components/ui';
import {
  chip,
  emptyForm,
  ReleaseDetail,
  ReleaseForm,
  ReleaseFormValue,
  RollupBar,
  STATUS_COLOR,
  STATUS_LABEL,
  SummaryLine,
} from './ReleasesParts';

/** One release in the list: its name, status, counts, and its controls. */
const ReleaseRow: React.FC<{
  release: TargetRelease;
  summary: ReleaseSummaryCounts;
  active: boolean;
  first: boolean;
  last: boolean;
  busy: boolean;
  onSelect: () => void;
  onMove: (delta: number) => void;
  onEdit: () => void;
  onDelete: () => void;
}> = ({ release: r, summary: s, active, first, last, busy, onSelect, onMove, onEdit, onDelete }) => (
  <li
    className="card"
    style={{
      display: 'flex',
      gap: 16,
      alignItems: 'center',
      flexWrap: 'wrap',
      borderColor: active ? 'var(--accent)' : undefined,
    }}
  >
    <div style={{ flex: '1 1 260px', minWidth: 0 }}>
      <div
        style={{
          display: 'flex',
          gap: 8,
          alignItems: 'center',
          flexWrap: 'wrap',
        }}
      >
        <button
          onClick={onSelect}
          aria-current={active ? 'true' : undefined}
          style={{
            fontSize: 16,
            fontWeight: 600,
            padding: '4px 0',
            minHeight: 32,
            background: 'none',
            border: 'none',
            color: 'var(--accent)',
            cursor: 'pointer',
            textDecoration: 'underline',
          }}
        >
          {r.name}
        </button>
        <span style={chip(STATUS_COLOR[r.status])}>{STATUS_LABEL[r.status]}</span>
        {r.target_date && <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Target {r.target_date}</span>}
      </div>
      {r.description && (
        <div
          style={{
            fontSize: 13,
            color: 'var(--text-muted)',
            marginTop: 2,
          }}
        >
          {r.description}
        </div>
      )}
      <SummaryLine s={s} />
    </div>
    <div style={{ flex: '1 1 200px', maxWidth: 320 }}>
      <RollupBar counts={s.by_rollup} total={s.total} />
    </div>
    <div style={{ display: 'flex', gap: 4 }}>
      <button
        className="button-secondary"
        onClick={() => onMove(-1)}
        disabled={busy || first}
        aria-label={`Move ${r.name} earlier`}
      >
        ↑
      </button>
      <button
        className="button-secondary"
        onClick={() => onMove(1)}
        disabled={busy || last}
        aria-label={`Move ${r.name} later`}
      >
        ↓
      </button>
      <button className="button-secondary" onClick={onEdit}>
        Edit
      </button>
      <button className="button-secondary" onClick={onDelete} disabled={busy}>
        Delete
      </button>
    </div>
  </li>
);

export const Releases: React.FC = () => {
  const { projectId = '' } = useParams<{ projectId: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedId = searchParams.get('release') || '';
  const confirm = useConfirm();

  const [overview, setOverview] = useState<ReleaseOverview | null>(null);
  const [progress, setProgress] = useState<ReleaseProgress | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [adding, setAdding] = useState(false);
  const [editingId, setEditingId] = useState('');
  const [busy, setBusy] = useState(false);

  const releases = useMemo(() => (overview?.releases || []).map((r) => r.release), [overview]);
  const names = useMemo(() => Object.fromEntries(releases.map((r) => [r.id, r.name])), [releases]);

  const loadOverview = useCallback(() => {
    if (!projectId) return;
    setLoading(true);
    releasesAPI
      .overview(projectId)
      .then((res) => setOverview(res.data))
      .catch((err) => setError(apiErrorMessage(err, 'Failed to load the releases')))
      .finally(() => setLoading(false));
  }, [projectId]);

  useEffect(() => {
    loadOverview();
  }, [loadOverview]);

  // The selected release, or the first that is not released yet.
  const activeId = useMemo(() => {
    if (selectedId && releases.some((r) => r.id === selectedId)) return selectedId;
    return (releases.find((r) => r.status !== 'released') || releases[0])?.id || '';
  }, [selectedId, releases]);

  useEffect(() => {
    if (!projectId || !activeId) {
      setProgress(null);
      return;
    }
    releasesAPI
      .progress(projectId, activeId)
      .then((res) => setProgress(res.data))
      .catch((err) => setError(apiErrorMessage(err, 'Failed to load the release')));
  }, [projectId, activeId, overview]);

  const run = async (fn: () => Promise<unknown>, fallback: string) => {
    setBusy(true);
    setError('');
    try {
      await fn();
      loadOverview();
      return true;
    } catch (err) {
      setError(apiErrorMessage(err, fallback));
      return false;
    } finally {
      setBusy(false);
    }
  };

  const create = async (v: ReleaseFormValue) => {
    if (
      await run(
        () =>
          releasesAPI.create(projectId, {
            name: v.name.trim(),
            description: v.description,
            status: v.status,
            ...(v.target_date ? { target_date: v.target_date } : {}),
          }),
        'Failed to add the release',
      )
    )
      setAdding(false);
  };

  const update = async (id: string, v: ReleaseFormValue) => {
    if (
      await run(
        () =>
          releasesAPI.update(projectId, id, {
            name: v.name.trim(),
            description: v.description,
            status: v.status,
            target_date: v.target_date,
          }),
        'Failed to save the release',
      )
    )
      setEditingId('');
  };

  const move = (index: number, delta: number) => {
    const ids = releases.map((r) => r.id);
    const to = index + delta;
    if (to < 0 || to >= ids.length) return;
    [ids[index], ids[to]] = [ids[to], ids[index]];
    void run(() => releasesAPI.reorder(projectId, ids), 'Failed to reorder the releases');
  };

  const remove = async (r: TargetRelease) => {
    const ok = await confirm({
      title: 'Delete release',
      message: `Delete ${r.name}? A release that requirements still target can't be deleted: move them to another release first.`,
      confirmLabel: 'Delete release',
      danger: true,
    });
    if (ok) await run(() => releasesAPI.remove(projectId, r.id), 'Failed to delete the release');
  };

  const select = (id: string) => {
    const next = new URLSearchParams(searchParams);
    next.set('release', id);
    setSearchParams(next, { replace: true });
  };

  const formFor = (r: TargetRelease): ReleaseFormValue => ({
    name: r.name,
    description: r.description,
    target_date: r.target_date || '',
    status: r.status,
  });

  return (
    <div style={{ padding: 24 }}>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 16,
          marginBottom: 16,
          flexWrap: 'wrap',
        }}
      >
        <h2 style={{ color: 'var(--text)', margin: 0 }}>Releases</h2>
        <div style={{ flex: 1 }} />
        {!adding && (
          <button className="button" onClick={() => setAdding(true)} data-help-key="releases.add">
            Add release
          </button>
        )}
      </div>
      <p
        style={{
          color: 'var(--text-muted)',
          fontSize: 13,
          marginTop: 0,
          marginBottom: 16,
          maxWidth: 760,
        }}
      >
        Releases run in order. A requirement is in the release it is introduced in and every later one, until its last
        release if it has one. Set a requirement's releases in its editor; a heading's releases apply to everything
        under it.
      </p>

      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 12 }} />
      {adding && (
        <ReleaseForm
          initial={emptyForm}
          submitLabel="Add release"
          busy={busy}
          onSubmit={create}
          onCancel={() => setAdding(false)}
        />
      )}
      {loading && !overview && <div style={{ color: 'var(--text-muted)' }}>Loading…</div>}
      {overview && releases.length === 0 && !adding && (
        <div className="card" style={{ color: 'var(--text-muted)' }}>
          No releases yet. Add one, such as MVP, then target requirements at it from their editor.
        </div>
      )}

      <ol
        aria-label="Releases in order"
        style={{ listStyle: 'none', padding: 0, margin: 0 }}
        data-help-key="releases.list"
      >
        {(overview?.releases || []).map(({ release: r, summary: s }, i) =>
          editingId === r.id ? (
            <li key={r.id}>
              <ReleaseForm
                initial={formFor(r)}
                submitLabel="Save"
                busy={busy}
                onSubmit={(v) => update(r.id, v)}
                onCancel={() => setEditingId('')}
              />
            </li>
          ) : (
            <ReleaseRow
              key={r.id}
              release={r}
              summary={s}
              active={r.id === activeId}
              first={i === 0}
              last={i === releases.length - 1}
              busy={busy}
              onSelect={() => select(r.id)}
              onMove={(delta) => move(i, delta)}
              onEdit={() => setEditingId(r.id)}
              onDelete={() => remove(r)}
            />
          ),
        )}
      </ol>
      {overview && overview.unassigned > 0 && (
        <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
          {overview.unassigned} requirement
          {overview.unassigned === 1 ? ' targets' : 's target'} no release.{' '}
          <Link to={`/projects/${projectId}/requirements`}>Open the requirements</Link> to plan{' '}
          {overview.unassigned === 1 ? 'it' : 'them'}.
        </p>
      )}

      {progress && progress.release.id === activeId && (
        <ReleaseDetail progress={progress} projectId={projectId} names={names} />
      )}
    </div>
  );
};
