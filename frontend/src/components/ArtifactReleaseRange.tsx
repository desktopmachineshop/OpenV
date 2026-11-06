// The release range in the artifact editor: the target release an artifact
// is introduced in, and the last release it is in (every later one when
// none). Left unset, the artifact inherits its nearest ancestor's range, as
// the server resolves it (internal/domain/targetreleases): the hint says
// which. Without the editor-pickers gate it renders nothing until the
// project has a release; with it, a project with none says so and links to
// the Releases page, so the field is never missing without a reason.
import React, { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  releasesAPI,
  TARGET_RELEASE_ATTR,
  TARGET_RELEASE_UNTIL_ATTR,
  type Artifact,
  type TargetRelease,
} from '../api/client';
import { useFeature } from '../hooks/useFeature';
import { TARGET_RELEASES_FEATURE } from '../features';
import { projectPath } from '../pages';
import { EDITOR_PICKERS_FEATURE, Picker, type PickerOption } from './ui/Picker';

interface Props {
  projectId?: string;
  parentId?: string | null;
  attributes: Record<string, unknown>;
  /** The project's artifacts, for the inherited range's ancestors. */
  artifacts?: Artifact[];
  onChange: (key: string, value: string | undefined) => void;
}

const str = (v: unknown) => (typeof v === 'string' ? v : '');

/** The nearest ancestor's own range, walking parent_id up the tree. */
export const inheritedRange = (
  parentId: string | null | undefined,
  artifacts: Artifact[],
  releases: TargetRelease[]
): { from: string; until: string; source: Artifact } | null => {
  const byId = new Map(artifacts.map((a) => [a.id, a]));
  const known = new Set(releases.map((r) => r.id));
  const seen = new Set<string>();
  for (let id = parentId || ''; id && !seen.has(id); ) {
    seen.add(id);
    const a = byId.get(id);
    if (!a) return null;
    const from = str(a.attributes?.[TARGET_RELEASE_ATTR]);
    if (known.has(from)) {
      const until = str(a.attributes?.[TARGET_RELEASE_UNTIL_ATTR]);
      return { from, until: known.has(until) ? until : '', source: a };
    }
    id = a.parent_id || '';
  }
  return null;
};

export const ArtifactReleaseRange: React.FC<Props> = ({ projectId, parentId, attributes, artifacts, onChange }) => {
  const on = useFeature(TARGET_RELEASES_FEATURE);
  const [releases, setReleases] = useState<TargetRelease[]>([]);
  // Whether the list has answered, so "no releases yet" never flashes up
  // while it loads, nor shows when it failed.
  const [loaded, setLoaded] = useState(false);
  useEffect(() => {
    if (!on || !projectId) return;
    let cancelled = false;
    setLoaded(false);
    releasesAPI
      .list(projectId)
      .then((res) => {
        if (cancelled) return;
        setReleases(res.data || []);
        setLoaded(true);
      })
      .catch(() => !cancelled && setReleases([]));
    return () => {
      cancelled = true;
    };
  }, [on, projectId]);

  const from = str(attributes[TARGET_RELEASE_ATTR]);
  const until = str(attributes[TARGET_RELEASE_UNTIL_ATTR]);
  const fromIndex = releases.findIndex((r) => r.id === from);
  const inherited = useMemo(
    () => (from ? null : inheritedRange(parentId, artifacts || [], releases)),
    [from, parentId, artifacts, releases]
  );
  const pickers = useFeature(EDITOR_PICKERS_FEATURE);
  if (!on) return null;
  if (releases.length === 0) {
    if (!pickers || !projectId || !loaded) return null;
    return (
      <div className="form-group" data-help-key="requirements.releases">
        <label>Releases</label>
        <div style={emptyStyle}>
          <span>This project has no releases yet, so this artifact can’t be planned into one.</span>
          <Link to={projectPath(projectId, 'releases')} className="button" style={{ whiteSpace: 'nowrap', textDecoration: 'none' }}>
            Create a release
          </Link>
        </div>
      </div>
    );
  }
  const name = (id: string) => releases.find((r) => r.id === id)?.name || '';
  const option = (r: TargetRelease, label: string): PickerOption => ({
    value: r.id,
    label,
    meta: r.target_date ? formatDate(r.target_date) : undefined,
    keywords: r.name,
  });
  const chooseFrom = (value: string) => {
    const next = value || undefined;
    onChange(TARGET_RELEASE_ATTR, next);
    const ni = releases.findIndex((r) => r.id === next);
    const ui = releases.findIndex((r) => r.id === until);
    if (!next || (ui >= 0 && ui < ni)) onChange(TARGET_RELEASE_UNTIL_ATTR, undefined);
  };

  return (
    <div className="form-group" data-help-key="requirements.releases">
      <label htmlFor="target_release">Releases</label>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
        <div style={{ flex: '1 1 200px', minWidth: 0 }}>
          <Picker
            id="target_release"
            aria-label="First release"
            value={fromIndex >= 0 ? from : ''}
            noneLabel={parentId ? 'Same as its parent' : 'No release'}
            options={releases.map((r) => option(r, `From ${r.name}`))}
            onChange={chooseFrom}
          />
        </div>
        {fromIndex >= 0 && (
          <div style={{ flex: '1 1 200px', minWidth: 0 }}>
            <Picker
              id="target_release_until"
              aria-label="Last release"
              value={releases.some((r) => r.id === until) ? until : ''}
              noneLabel="and every later release"
              options={releases.slice(fromIndex).map((r) => option(r, r.id === from ? 'only' : `to ${r.name}`))}
              onChange={(v) => onChange(TARGET_RELEASE_UNTIL_ATTR, v || undefined)}
            />
          </div>
        )}
      </div>
      {inherited && (
        <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>
          Inherited from {inherited.source.ref || inherited.source.title}: {name(inherited.from)}
          {inherited.until ? (inherited.until === inherited.from ? ' only' : ` to ${name(inherited.until)}`) : ' onwards'}
        </div>
      )}
    </div>
  );
};

const formatDate = (iso: string) => {
  const d = new Date(`${iso}T00:00:00`);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleDateString(undefined, { month: 'short', year: 'numeric' });
};

const emptyStyle: React.CSSProperties = {
  display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap',
  border: '1px dashed var(--border)', borderRadius: 6, padding: '10px 12px',
  background: 'var(--surface-alt)', color: 'var(--text-body)',
};
