import React, { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import {
  Artifact,
  Link as TraceLink,
  SharedProject,
  authAPI,
  openSourceAPI,
  shareLinkAPI,
} from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { ArtifactList } from '../components/ArtifactList';
import { ArtifactBody } from '../components/ArtifactBody';
import { useViewport } from '../hooks/useViewport';

// SharedProjectView is a project for somebody with no account (REQ-149,
// REQ-151): what a public share link opens, and what the open-source page
// opens for a project whose workspace is on the open-source plan. It is the
// module view stripped to reading: the tree, the selected artifact with its
// description, attributes and traceability, and nothing that would need a
// session. A reviewer link lands here too, and is offered the sign-in that
// turns it into reviewer access.
//
// Everything on the page comes from one answer. Nothing here calls an
// endpoint that needs a session, and the 401 interceptor leaves these paths
// alone, so a visitor is never bounced to the sign-in form.

interface SharedProjectViewProps {
  /** Where the project comes from: a share token, or an open-source project id. */
  source: 'share' | 'open-source';
}

const TYPE_LABELS: Record<string, string> = {
  requirement: 'Requirement',
  'user-need': 'User need',
  'design-item': 'Design item',
  'test-case': 'Test case',
  hazard: 'Hazard',
  persona: 'Persona',
  heading: 'Section',
  'non-functional': 'Non-functional requirement',
};

const typeLabel = (type: string): string => TYPE_LABELS[type] || type;

const STANDARD_ATTRIBUTE_LABELS: Record<string, string> = {
  priority: 'Priority',
  verification_method: 'Verification method',
  verification_status: 'Verification status',
  owner: 'Owner',
  rationale: 'Rationale',
  execution_method: 'Execution method',
};

const humanKey = (key: string): string =>
  STANDARD_ATTRIBUTE_LABELS[key] || key.replace(/_/g, ' ').replace(/^\w/, (c) => c.toUpperCase());

const isPlain = (v: unknown): v is string | number | boolean =>
  typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean';

/** The attributes worth a row: scalars, and short lists of them. */
const attributeRows = (artifact: Artifact): [string, string][] => {
  const rows: [string, string][] = [];
  if (artifact.status) rows.push(['Status', artifact.status.replace('_', ' ')]);
  Object.entries(artifact.attributes || {})
    .filter(([k]) => k !== 'status')
    .sort(([a], [b]) => a.localeCompare(b))
    .forEach(([k, v]) => {
      if (v === '' || v === null || v === undefined) return;
      if (isPlain(v)) rows.push([humanKey(k), String(v)]);
      else if (Array.isArray(v) && v.every(isPlain) && v.length <= 8) rows.push([humanKey(k), v.join(', ')]);
    });
  return rows;
};

const badge: React.CSSProperties = {
  display: 'inline-block',
  padding: '2px 8px',
  borderRadius: 999,
  fontSize: 12,
  fontWeight: 600,
  background: 'var(--accent-soft, rgba(44,142,240,0.12))',
  color: 'var(--accent)',
};

const ReadOnlyDetails: React.FC<{
  artifact: Artifact;
  artifacts: Artifact[];
  links: TraceLink[];
  onSelect: (id: string) => void;
}> = ({ artifact, artifacts, links, onSelect }) => {
  const byId = useMemo(() => new Map(artifacts.map((a) => [a.id, a])), [artifacts]);
  const outgoing = links.filter((l) => l.from_id === artifact.id);
  const incoming = links.filter((l) => l.to_id === artifact.id);
  const rows = attributeRows(artifact);
  const refOf = (id: string) => {
    const a = byId.get(id);
    return a ? `${a.ref ? a.ref + ' ' : ''}${a.title}` : 'an artifact outside this view';
  };
  const linkRow = (l: TraceLink, otherId: string, verb: string) => (
    <li key={l.id} style={{ marginBottom: 4 }}>
      <span style={{ color: 'var(--text-muted)' }}>{verb} </span>
      {byId.has(otherId) ? (
        <button
          type="button"
          onClick={() => onSelect(otherId)}
          style={{
            background: 'none',
            border: 'none',
            padding: 0,
            color: 'var(--accent)',
            cursor: 'pointer',
            font: 'inherit',
            textAlign: 'left',
            width: 'auto',
            minHeight: 0,
          }}
        >
          {refOf(otherId)}
        </button>
      ) : (
        <span>{refOf(otherId)}</span>
      )}
    </li>
  );
  return (
    <div className="card" style={{ padding: 20 }}>
      <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
        {artifact.ref && (
          <code style={{ fontSize: 13, color: 'var(--text-muted)' }}>{artifact.ref}</code>
        )}
        <span style={badge}>{typeLabel(artifact.type)}</span>
        <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>v{artifact.version}</span>
      </div>
      <h2 style={{ margin: '8px 0 12px', fontSize: 20 }}>{artifact.title}</h2>
      {artifact.body ? (
        <ArtifactBody
          body={artifact.body}
          onReferenceClick={(ref) => {
            const target = artifacts.find((a) => a.ref === ref);
            if (target) onSelect(target.id);
          }}
        />
      ) : (
        <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>No description.</p>
      )}
      {rows.length > 0 && (
        <table style={{ width: '100%', borderCollapse: 'collapse', marginTop: 16, fontSize: 13 }}>
          <tbody>
            {rows.map(([k, v]) => (
              <tr key={k}>
                <th
                  style={{
                    textAlign: 'left',
                    padding: '4px 8px 4px 0',
                    color: 'var(--text-muted)',
                    fontWeight: 500,
                    width: 160,
                    verticalAlign: 'top',
                  }}
                >
                  {k}
                </th>
                <td style={{ padding: '4px 0' }}>{v}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {(outgoing.length > 0 || incoming.length > 0) && (
        <div style={{ marginTop: 16 }}>
          <h3 style={{ fontSize: 14, margin: '0 0 6px' }}>Traceability</h3>
          <ul style={{ margin: 0, paddingLeft: '1.2em', fontSize: 13 }}>
            {outgoing.map((l) => linkRow(l, l.to_id, l.type.replace(/[_-]/g, ' ')))}
            {incoming.map((l) => linkRow(l, l.from_id, `${l.type.replace(/[_-]/g, ' ')} by`))}
          </ul>
        </div>
      )}
    </div>
  );
};

export const SharedProjectView: React.FC<SharedProjectViewProps> = ({ source }) => {
  const { token = '', id = '' } = useParams<{ token: string; id: string }>();
  const navigate = useNavigate();
  const { isPhone } = useViewport();
  const [shared, setShared] = useState<SharedProject | null>(null);
  const [error, setError] = useState('');
  const [selectedId, setSelectedId] = useState<string>('');
  const [signedIn, setSignedIn] = useState<boolean | null>(null);
  const [joining, setJoining] = useState(false);
  const [pane, setPane] = useState<'tree' | 'detail'>('tree');

  useEffect(() => {
    let cancelled = false;
    const load = source === 'share' ? shareLinkAPI.open(token) : openSourceAPI.get(id);
    load
      .then((res) => {
        if (!cancelled) setShared(res.data);
      })
      .catch((err) => {
        if (!cancelled)
          setError(
            apiErrorMessage(
              err,
              source === 'share'
                ? 'This link does not open anything. It may have been revoked or expired.'
                : 'This project is not published.'
            )
          );
      });
    return () => {
      cancelled = true;
    };
  }, [source, token, id]);

  // A reviewer link needs an account. Find out whether this browser has one
  // so the page can offer "open as reviewer" instead of a sign-in it does
  // not need.
  useEffect(() => {
    if (shared?.role !== 'reviewer') return;
    authAPI
      .me()
      .then(() => setSignedIn(true))
      .catch(() => setSignedIn(false));
  }, [shared?.role]);

  const artifacts = useMemo(() => shared?.snapshot?.artifacts || [], [shared]);
  const links = useMemo(() => shared?.snapshot?.links || [], [shared]);
  const selected = artifacts.find((a) => a.id === selectedId) || null;
  const select = (artifactId: string) => {
    setSelectedId(artifactId);
    setPane('detail');
  };

  const join = async () => {
    setJoining(true);
    setError('');
    try {
      const res = await authAPI.acceptShareLink(token);
      navigate(`/projects/${res.data.project_id}/requirements`);
    } catch (err: any) {
      setError(apiErrorMessage(err, 'Could not open the project for review'));
    } finally {
      setJoining(false);
    }
  };

  const title = shared?.project.name || (source === 'share' ? 'Shared project' : 'Open-source project');

  return (
    <div style={{ height: '100vh', background: 'var(--bg-app)', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      <header
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 12,
          padding: isPhone ? '10px 12px' : '12px 20px',
          background: 'var(--sidebar-bg)',
          color: 'var(--sidebar-text, #fff)',
          flexWrap: 'wrap',
        }}
      >
        <Link to="/" style={{ color: 'inherit', textDecoration: 'none', fontWeight: 700, fontSize: 18 }}>
          OpenV
        </Link>
        <span style={{ opacity: 0.6 }}>/</span>
        <span style={{ fontWeight: 600, flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis' }}>
          {title}
          {shared?.workspace && <span style={{ opacity: 0.7, fontWeight: 400 }}> · {shared.workspace}</span>}
        </span>
        {shared && (
          <span style={{ ...badge, background: 'rgba(255,255,255,0.15)', color: 'inherit' }}>
            {shared.baseline ? `Snapshot: ${shared.baseline.name}` : shared.role === 'reviewer' ? 'For review' : 'View only'}
          </span>
        )}
        <Link to="/login" style={{ color: 'inherit', fontSize: 13, opacity: 0.85 }}>
          Sign in
        </Link>
      </header>

      {error && (
        <div style={{ maxWidth: 720, margin: '40px auto', padding: '0 16px', overflowY: 'auto' }}>
          <div className="card" style={{ padding: 24 }}>
            <h1 style={{ fontSize: 20, marginTop: 0 }}>Nothing to show</h1>
            <p style={{ color: 'var(--text-muted)' }}>{error}</p>
            <Link to="/">About OpenV</Link>
          </div>
        </div>
      )}

      {!error && !shared && (
        <p style={{ padding: 24, color: 'var(--text-muted)', fontSize: 13 }}>Loading…</p>
      )}

      {shared && shared.role === 'reviewer' && (
        <div style={{ maxWidth: 720, margin: '40px auto', padding: '0 16px', width: '100%', boxSizing: 'border-box', overflowY: 'auto' }}>
          <div className="card" style={{ padding: 24 }}>
            <h1 style={{ fontSize: 22, marginTop: 0 }}>You are invited to review {shared.project.name}</h1>
            {shared.project.description && <p>{shared.project.description}</p>}
            <p style={{ color: 'var(--text-muted)', fontSize: 14 }}>
              A reviewer reads the whole project and adds notes, comments and mentions, but cannot
              change the text. Reviewing needs an account, so your comments carry your name.
            </p>
            {signedIn ? (
              <button type="button" className="button" onClick={join} disabled={joining} style={{ width: 'auto' }}>
                {joining ? 'Opening…' : 'Open as reviewer'}
              </button>
            ) : (
              <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
                <Link className="button" to={`/login?share=${encodeURIComponent(token)}`} style={{ width: 'auto' }}>
                  Sign in to review
                </Link>
                <Link
                  className="button button-secondary"
                  to={`/login?mode=register&share=${encodeURIComponent(token)}`}
                  style={{ width: 'auto' }}
                >
                  Create an account
                </Link>
              </div>
            )}
          </div>
        </div>
      )}

      {shared && shared.snapshot && (
        <div
          style={{
            display: 'flex',
            flex: 1,
            minHeight: 0,
            gap: isPhone ? 0 : 16,
            padding: isPhone ? 0 : 16,
            flexDirection: isPhone ? 'column' : 'row',
          }}
        >
          {isPhone && (
            <div style={{ display: 'flex', borderBottom: '1px solid var(--border-soft)', background: 'var(--surface)' }}>
              {(['tree', 'detail'] as const).map((p) => (
                <button
                  key={p}
                  type="button"
                  onClick={() => setPane(p)}
                  style={{
                    flex: 1,
                    background: 'none',
                    border: 'none',
                    borderBottom: pane === p ? '2px solid var(--accent)' : '2px solid transparent',
                    padding: 10,
                    fontWeight: pane === p ? 600 : 400,
                    color: pane === p ? 'var(--text)' : 'var(--text-muted)',
                  }}
                >
                  {p === 'tree' ? 'Tree' : 'Details'}
                </button>
              ))}
            </div>
          )}
          {(!isPhone || pane === 'tree') && (
            <div
              className={isPhone ? '' : 'card'}
              style={{ width: isPhone ? '100%' : 360, flexShrink: 0, overflowY: 'auto', padding: isPhone ? 12 : 16 }}
            >
              <ArtifactList
                artifacts={artifacts}
                allArtifacts={artifacts}
                selectedId={selectedId || undefined}
                onSelect={select}
                onReorder={() => undefined}
                readOnly
                hideHeading={isPhone}
              />
            </div>
          )}
          {(!isPhone || pane === 'detail') && (
            <div style={{ flex: 1, minWidth: 0, overflowY: 'auto', padding: isPhone ? 12 : 0 }}>
              {selected ? (
                <ReadOnlyDetails artifact={selected} artifacts={artifacts} links={links} onSelect={select} />
              ) : (
                <div className="card" style={{ padding: 20 }}>
                  <h2 style={{ fontSize: 18, marginTop: 0 }}>{shared.project.name}</h2>
                  {shared.project.description && <p>{shared.project.description}</p>}
                  <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>
                    {Object.entries(shared.counts)
                      .map(([t, n]) => `${n} ${typeLabel(t).toLowerCase()}${n === 1 ? '' : 's'}`)
                      .join(' · ')}
                    {shared.baseline
                      ? ` · snapshot "${shared.baseline.name}" from ${new Date(shared.baseline.created_at).toLocaleDateString()}`
                      : ' · live, read only'}
                  </p>
                  <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>
                    Pick an artifact from the tree to read it. Attachments and comments are not part of a shared view.
                  </p>
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
};
