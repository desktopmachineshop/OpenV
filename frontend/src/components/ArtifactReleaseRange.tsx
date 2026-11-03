// The release range in the artifact editor: the target release an artifact
// is introduced in, and the last release it is in (every later one when
// none). Left unset, the artifact inherits its nearest ancestor's range, as
// the server resolves it (internal/domain/targetreleases): the hint says
// which. It renders nothing until the project has a release.
import React, { useEffect, useMemo, useState } from 'react';
import {
  releasesAPI,
  TARGET_RELEASE_ATTR,
  TARGET_RELEASE_UNTIL_ATTR,
  type Artifact,
  type TargetRelease,
} from '../api/client';
import { useFeature } from '../hooks/useFeature';
import { TARGET_RELEASES_FEATURE } from '../features';

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
  useEffect(() => {
    if (!on || !projectId) return;
    let cancelled = false;
    releasesAPI
      .list(projectId)
      .then((res) => !cancelled && setReleases(res.data || []))
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
  if (!on || releases.length === 0) return null;
  const name = (id: string) => releases.find((r) => r.id === id)?.name || '';

  return (
    <div className="form-group" data-help-key="requirements.releases">
      <label htmlFor="target_release">Releases</label>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
        <select
          id="target_release"
          aria-label="First release"
          value={fromIndex >= 0 ? from : ''}
          onChange={(e) => {
            const next = e.target.value || undefined;
            onChange(TARGET_RELEASE_ATTR, next);
            const ni = releases.findIndex((r) => r.id === next);
            const ui = releases.findIndex((r) => r.id === until);
            if (!next || (ui >= 0 && ui < ni)) onChange(TARGET_RELEASE_UNTIL_ATTR, undefined);
          }}
        >
          <option value="">{parentId ? 'Same as its parent' : 'No release'}</option>
          {releases.map((r) => (
            <option key={r.id} value={r.id}>
              From {r.name}
            </option>
          ))}
        </select>
        {fromIndex >= 0 && (
          <select
            aria-label="Last release"
            value={releases.some((r) => r.id === until) ? until : ''}
            onChange={(e) => onChange(TARGET_RELEASE_UNTIL_ATTR, e.target.value || undefined)}
          >
            <option value="">and every later release</option>
            {releases.slice(fromIndex).map((r) => (
              <option key={r.id} value={r.id}>
                {r.id === from ? 'only' : `to ${r.name}`}
              </option>
            ))}
          </select>
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
