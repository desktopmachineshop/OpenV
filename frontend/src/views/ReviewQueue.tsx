import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { Artifact, linkAPI, reviewAPI, SuspectLink } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';
import { ErrorBanner, SegmentedControl, useConfirm } from '../components/ui';

type Section = 'all' | 'links' | 'artifacts';

// Small pill for an artifact type/kind, matching the V&V chip styling.
const typeChip = (label: string): React.CSSProperties => ({
  display: 'inline-block',
  padding: '1px 8px',
  borderRadius: 10,
  background: 'var(--surface-alt, var(--surface))',
  border: '1px solid var(--border)',
  color: 'var(--text-muted)',
  fontSize: 11,
  fontWeight: 600,
  whiteSpace: 'nowrap',
});

const cardStyle: React.CSSProperties = {
  background: 'var(--surface)',
  border: '1px solid var(--border)',
  borderRadius: 6,
  padding: 16,
  marginBottom: 24,
};

/**
 * ReviewQueue is the reviewer's daily driver (issue #183): the suspect links
 * whose meaning may no longer hold and the artifacts sitting in review, in one
 * place. Suspect links can be cleared one at a time or in bulk (both reuse
 * linkAPI.confirm); in-review artifacts deep-link into the requirements module
 * via ?artifact= so the reviewer can open and sign them off.
 */
export const ReviewQueue: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;
  const confirm = useConfirm();

  const [suspectLinks, setSuspectLinks] = useState<SuspectLink[]>([]);
  const [inReview, setInReview] = useState<Artifact[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [section, setSection] = useState<Section>('all');
  // Ids currently being confirmed, so their row buttons disable individually.
  const [confirming, setConfirming] = useState<Record<string, boolean>>({});
  const [selected, setSelected] = useState<Record<string, boolean>>({});

  const load = useCallback(() => {
    if (!projectId) return;
    setLoading(true);
    reviewAPI
      .get(projectId)
      .then((res) => {
        setSuspectLinks(res.data.suspect_links || []);
        setInReview(res.data.in_review_artifacts || []);
        setError('');
      })
      .catch((err) => setError(apiErrorMessage(err, 'Failed to load the review queue')))
      .finally(() => setLoading(false));
  }, [projectId]);

  useEffect(() => {
    load();
  }, [load]);

  const selectedIds = useMemo(
    () => suspectLinks.filter((l) => selected[l.id]).map((l) => l.id),
    [suspectLinks, selected],
  );
  const allSelected = suspectLinks.length > 0 && selectedIds.length === suspectLinks.length;

  // Clear one link's suspect flag and drop it from the list on success.
  const confirmLink = useCallback(async (id: string) => {
    setConfirming((c) => ({ ...c, [id]: true }));
    try {
      await linkAPI.confirm(id);
      setSuspectLinks((links) => links.filter((l) => l.id !== id));
      setSelected((s) => {
        const next = { ...s };
        delete next[id];
        return next;
      });
      setError('');
    } catch (err) {
      setError(apiErrorMessage(err, 'Failed to confirm link'));
    } finally {
      setConfirming((c) => {
        const next = { ...c };
        delete next[id];
        return next;
      });
    }
  }, []);

  const confirmSelected = useCallback(async () => {
    if (selectedIds.length === 0) return;
    const ok = await confirm({
      title: 'Confirm selected links',
      message: `Clear the suspect flag on ${selectedIds.length} link${
        selectedIds.length === 1 ? '' : 's'
      }? This vouches that each still holds after its artifact changed.`,
      confirmLabel: 'Confirm links',
    });
    if (!ok) return;

    const results = await Promise.allSettled(selectedIds.map((id) => linkAPI.confirm(id)));
    const cleared = new Set<string>();
    let failures = 0;
    results.forEach((res, i) => {
      if (res.status === 'fulfilled') cleared.add(selectedIds[i]);
      else failures += 1;
    });
    if (cleared.size > 0) {
      setSuspectLinks((links) => links.filter((l) => !cleared.has(l.id)));
      setSelected((s) => {
        const next = { ...s };
        cleared.forEach((id) => delete next[id]);
        return next;
      });
    }
    setError(failures > 0 ? `${failures} link${failures === 1 ? '' : 's'} could not be confirmed.` : '');
  }, [confirm, selectedIds]);

  const toggleAll = () => {
    if (allSelected) {
      setSelected({});
    } else {
      const next: Record<string, boolean> = {};
      suspectLinks.forEach((l) => {
        next[l.id] = true;
      });
      setSelected(next);
    }
  };

  const showLinks = section === 'all' || section === 'links';
  const showArtifacts = section === 'all' || section === 'artifacts';

  return (
    <div style={{ padding: 24 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 16, marginBottom: 16, flexWrap: 'wrap' }}>
        <h2 style={{ color: 'var(--text)', margin: 0 }}>Review Queue</h2>
        <SegmentedControl<Section>
          aria-label="Filter review queue"
          value={section}
          onChange={setSection}
          options={[
            { value: 'all', label: 'All' },
            { value: 'links', label: `Suspect links (${suspectLinks.length})` },
            { value: 'artifacts', label: `In review (${inReview.length})` },
          ]}
        />
        <div style={{ flex: 1 }} />
        <button className="button-secondary" onClick={load} disabled={loading}>
          Refresh
        </button>
      </div>

      <ErrorBanner message={error} onDismiss={() => setError('')} />

      {loading ? (
        <div style={{ color: 'var(--text-muted)' }}>Loading…</div>
      ) : (
        <>
          {showLinks && (
            <section style={cardStyle}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12, flexWrap: 'wrap' }}>
                <h3 style={{ margin: 0, color: 'var(--text)', fontSize: 16 }}>
                  Suspect links
                </h3>
                <span style={{ color: 'var(--text-muted)', fontSize: 13 }}>
                  Links flagged because a linked artifact changed. Confirm each that still holds.
                </span>
                <div style={{ flex: 1 }} />
                {suspectLinks.length > 0 && (
                  <button
                    className="button-secondary"
                    onClick={confirmSelected}
                    disabled={selectedIds.length === 0}
                  >
                    Confirm selected ({selectedIds.length})
                  </button>
                )}
              </div>

              {suspectLinks.length === 0 ? (
                <div style={{ color: 'var(--text-muted)', fontSize: 14 }}>
                  No suspect links. Traceability is trusted.
                </div>
              ) : (
                <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
                  <thead>
                    <tr style={{ textAlign: 'left', color: 'var(--text-muted)', fontSize: 12 }}>
                      <th style={{ padding: '6px 8px', width: 28 }}>
                        <input
                          type="checkbox"
                          checked={allSelected}
                          aria-label="Select all suspect links"
                          onChange={toggleAll}
                        />
                      </th>
                      <th style={{ padding: '6px 8px' }}>From</th>
                      <th style={{ padding: '6px 8px' }}>Link</th>
                      <th style={{ padding: '6px 8px' }}>To</th>
                      <th style={{ padding: '6px 8px', width: 100 }} />
                    </tr>
                  </thead>
                  <tbody>
                    {suspectLinks.map((l) => (
                      <tr key={l.id} style={{ borderTop: '1px solid var(--border)' }}>
                        <td style={{ padding: '8px' }}>
                          <input
                            type="checkbox"
                            checked={!!selected[l.id]}
                            aria-label={`Select link ${l.from_title} to ${l.to_title}`}
                            onChange={(e) =>
                              setSelected((s) => ({ ...s, [l.id]: e.target.checked }))
                            }
                          />
                        </td>
                        <td style={{ padding: '8px' }}>
                          <div style={{ color: 'var(--text)' }}>{l.from_title}</div>
                          <span style={typeChip(l.from_type)}>{l.from_type}</span>
                        </td>
                        <td style={{ padding: '8px', color: 'var(--text-muted)' }}>{l.type}</td>
                        <td style={{ padding: '8px' }}>
                          <div style={{ color: 'var(--text)' }}>{l.to_title}</div>
                          <span style={typeChip(l.to_type)}>{l.to_type}</span>
                        </td>
                        <td style={{ padding: '8px', textAlign: 'right' }}>
                          <button
                            className="button-secondary"
                            onClick={() => confirmLink(l.id)}
                            disabled={!!confirming[l.id]}
                          >
                            {confirming[l.id] ? 'Confirming…' : 'Confirm'}
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </section>
          )}

          {showArtifacts && (
            <section style={cardStyle}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12, flexWrap: 'wrap' }}>
                <h3 style={{ margin: 0, color: 'var(--text)', fontSize: 16 }}>
                  In review
                </h3>
                <span style={{ color: 'var(--text-muted)', fontSize: 13 }}>
                  Artifacts submitted for review. Open one to approve or send it back.
                </span>
              </div>

              {inReview.length === 0 ? (
                <div style={{ color: 'var(--text-muted)', fontSize: 14 }}>
                  Nothing is in review right now.
                </div>
              ) : (
                <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
                  {inReview.map((a) => (
                    <li
                      key={a.id}
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 10,
                        padding: '8px 0',
                        borderTop: '1px solid var(--border)',
                      }}
                    >
                      <span style={typeChip(a.type)}>{a.type}</span>
                      <Link
                        to={`../requirements?artifact=${a.id}`}
                        style={{ color: 'var(--accent)', textDecoration: 'none', flex: 1 }}
                      >
                        {a.title || '(untitled)'}
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </section>
          )}
        </>
      )}
    </div>
  );
};

export default ReviewQueue;
