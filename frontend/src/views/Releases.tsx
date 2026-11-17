// The Releases page: a project's target releases (MVP, V1.0, ...) in order,
// each with its progress against the requirements it holds, and one
// release's detail: its counts, a burn-up of verification over time and its
// requirements. A requirement is in the release it is introduced in and every
// later one, until its last release when it has one; see
// internal/domain/targetreleases.
//
// In the redesign kit (components/ui): the releases are one panel of rows,
// the current one marked with the accent edge; the detail heads with its
// figures inline, then the burn-up and the requirements as panels. The top
// bar names the page, so the page has no heading of its own. On a phone the
// page leads with the current release's progress (its figures, then what is
// failing) and keeps the list one tab away (ReleasesPhone.tsx). The look
// does not depend on the redesign-pages key: the page shows wherever
// target-releases is on, in whichever frame the workspace has.

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { releasesAPI, ReleaseOverview, ReleaseProgress, ReleaseSummaryCounts, TargetRelease } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { Chip, ErrorBanner, PanelHeader, buttonStyle, ellipsis, mutedStyle, pageType, panelStyle, useConfirm } from '../components/ui';
import { useViewport } from '../hooks/useViewport';
import { ReleasesPhone } from './ReleasesPhone';
import {
  emptyForm,
  ReleaseDetail,
  ReleaseForm,
  ReleaseFormValue,
  RollupBar,
  STATUS_TONE,
  STATUS_LABEL,
  SummaryLine,
} from './ReleasesParts';

/** One release in the list: its name, status, counts, and its controls. */
export const ReleaseRow: React.FC<{
  release: TargetRelease;
  summary: ReleaseSummaryCounts;
  active: boolean;
  first: boolean;
  last: boolean;
  busy: boolean;
  phone?: boolean;
  onSelect: () => void;
  onMove: (delta: number) => void;
  onEdit: () => void;
  onDelete: () => void;
}> = ({ release: r, summary: s, active, first, last, busy, phone, onSelect, onMove, onEdit, onDelete }) => {
  const small = { compact: !phone };
  return (
    <li
      style={{
        display: 'flex',
        gap: '6px 16px',
        alignItems: 'center',
        flexWrap: 'wrap',
        padding: '8px 12px',
        borderBottom: '1px solid var(--border-soft)',
        background: active ? 'var(--tint-blue)' : 'var(--surface)',
        boxShadow: active ? 'inset 3px 0 0 var(--accent)' : undefined,
      }}
    >
      <div style={{ flex: '1 1 260px', minWidth: 0 }}>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
          <button
            type="button"
            onClick={onSelect}
            aria-current={active ? 'true' : undefined}
            style={{
              minHeight: 32,
              padding: 0,
              background: 'none',
              border: 'none',
              font: 'inherit',
              fontSize: 14,
              fontWeight: 600,
              color: 'var(--accent-text)',
              cursor: 'pointer',
              textAlign: 'left',
            }}
          >
            {r.name}
          </button>
          <Chip tone={STATUS_TONE[r.status]}>{STATUS_LABEL[r.status]}</Chip>
          {r.target_date && <span style={mutedStyle}>Target {r.target_date}</span>}
        </div>
        {r.description && <div style={{ ...mutedStyle, ...ellipsis }}>{r.description}</div>}
        <SummaryLine s={s} />
      </div>
      <div style={{ flex: '1 1 160px', maxWidth: 240 }}>
        <RollupBar counts={s.by_rollup} total={s.total} />
      </div>
      <div style={{ display: 'flex', gap: 4 }}>
        <button
          type="button"
          onClick={() => onMove(-1)}
          disabled={busy || first}
          aria-label={`Move ${r.name} earlier`}
          style={buttonStyle('ghost', { ...small, disabled: busy || first })}
        >
          ↑
        </button>
        <button
          type="button"
          onClick={() => onMove(1)}
          disabled={busy || last}
          aria-label={`Move ${r.name} later`}
          style={buttonStyle('ghost', { ...small, disabled: busy || last })}
        >
          ↓
        </button>
        <button type="button" onClick={onEdit} style={buttonStyle('secondary', small)}>
          Edit
        </button>
        <button type="button" onClick={onDelete} disabled={busy} style={buttonStyle('danger', { ...small, disabled: busy })}>
          Delete
        </button>
      </div>
    </li>
  );
};

export const Releases: React.FC = () => {
  const { projectId = '' } = useParams<{ projectId: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedId = searchParams.get('release') || '';
  const confirm = useConfirm();
  const { isPhone } = useViewport();

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

  const list = (
    <section aria-label="Releases" style={panelStyle}>
      <PanelHeader title="Releases" count={releases.length} />
      {editingId === '' && overview && releases.length === 0 && !adding && (
        <p style={{ ...mutedStyle, margin: 0, padding: 12 }}>
          No releases yet. Add one, such as MVP, then target requirements at it from their editor.
        </p>
      )}
      <ol aria-label="Releases in order" style={{ listStyle: 'none', padding: 0, margin: 0 }} data-help-key="releases.list">
        {(overview?.releases || []).map(({ release: r, summary: s }, i) =>
          editingId === r.id ? (
            <li key={r.id} style={{ padding: 8, borderBottom: '1px solid var(--border-soft)' }}>
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
              phone={isPhone}
              onSelect={() => select(r.id)}
              onMove={(delta) => move(i, delta)}
              onEdit={() => setEditingId(r.id)}
              onDelete={() => remove(r)}
            />
          ),
        )}
      </ol>
    </section>
  );

  const head = (
    <div style={{ display: 'flex', alignItems: 'center', gap: '6px 12px', flexWrap: 'wrap' }}>
      <p style={{ ...mutedStyle, margin: 0, flex: '1 1 320px', maxWidth: 760 }}>
        Releases run in order. A requirement is in the release it is introduced in and every later one, until its last
        release if it has one. Set a requirement's releases in its editor; a heading's releases apply to everything
        under it.
      </p>
      {!adding && (
        <button type="button" onClick={() => setAdding(true)} data-help-key="releases.add" style={buttonStyle('primary')}>
          Add release
        </button>
      )}
    </div>
  );

  const unassigned = overview && overview.unassigned > 0 && (
    <p style={{ ...mutedStyle, margin: 0 }}>
      {overview.unassigned} requirement
      {overview.unassigned === 1 ? ' targets' : 's target'} no release.{' '}
      <Link to={`/projects/${projectId}/requirements`}>Open the requirements</Link> to plan{' '}
      {overview.unassigned === 1 ? 'it' : 'them'}.
    </p>
  );

  const form = adding && (
    <ReleaseForm initial={emptyForm} submitLabel="Add release" busy={busy} onSubmit={create} onCancel={() => setAdding(false)} />
  );
  const detail = progress && progress.release.id === activeId ? progress : null;

  if (isPhone) {
    return (
      <ReleasesPhone
        head={head}
        form={form}
        list={list}
        unassigned={unassigned}
        error={<ErrorBanner message={error} onDismiss={() => setError('')} />}
        loading={loading && !overview}
        progress={detail}
        projectId={projectId}
        names={names}
      />
    );
  }

  return (
    <div className="ov-gutter" style={{ ...pageType(false), padding: 16, display: 'flex', flexDirection: 'column', gap: 12 }}>
      {head}
      <ErrorBanner message={error} onDismiss={() => setError('')} />
      {form}
      {loading && !overview && <div style={mutedStyle}>Loading…</div>}
      {list}
      {unassigned}
      {detail && <ReleaseDetail progress={detail} projectId={projectId} names={names} />}
    </div>
  );
};
