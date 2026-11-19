import React, { useEffect, useRef, useState } from 'react';
import { projectAPI, Project } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';
import { useFeature } from '../hooks/useFeature';
import { useViewport } from '../hooks/useViewport';
import { PROJECT_COVERS_FEATURE } from '../features';
import { COVER_ACCEPT, prepareCover } from '../components/ProjectListCover';

const coverButton: React.CSSProperties = { padding: '6px 14px', width: 'auto' };

/**
 * The project's cover photo at the top of its Overview (project-covers):
 * the photo across the page with **Change cover** and **Remove** in its top
 * corner, or, before there is one, a slim box with **Add a cover photo**.
 * It spans the Overview's two columns (ov-overview-cover). figuresSlot, when
 * given, receives the place along the photo's foot where the project's
 * figures go (ProjectHomeStatus's figuresTarget); it is null while there is
 * no photo, and the figures then stay in their column. A large photo is
 * scaled down in the browser before it is uploaded; the same photo shows on
 * the project's row in All projects.
 */
export const ProductOverviewCover: React.FC<{ projectId: string; figuresSlot?: (el: HTMLDivElement | null) => void }> = ({
  projectId,
  figuresSlot,
}) => {
  const on = useFeature(PROJECT_COVERS_FEATURE);
  const { isPhone } = useViewport();
  const fromStore = useAppStore((s) => s.projects.find((p) => p.id === projectId));
  const updateProject = useAppStore((s) => s.updateProject);
  const [project, setProject] = useState<Project | undefined>(fromStore);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const fileInput = useRef<HTMLInputElement>(null);

  // A cover changed elsewhere in the app, such as by the assistant's card,
  // reaches the store's copy first.
  const storeVersion = fromStore?.cover_updated_at;
  useEffect(() => {
    if (fromStore) setProject(fromStore);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storeVersion]);

  // The project as the server has it now: the store's copy may predate a
  // cover changed elsewhere.
  useEffect(() => {
    if (!on) return;
    let live = true;
    projectAPI
      .get(projectId)
      .then((res) => live && setProject(res.data))
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [on, projectId]);

  if (!on) return null;

  const done = (next: Project) => {
    setProject(next);
    updateProject(next);
    setError('');
  };

  const choose = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (!file) return;
    setBusy(true);
    try {
      const { blob, name } = await prepareCover(file);
      done((await projectAPI.uploadCover(projectId, blob, name)).data);
    } catch (err) {
      setError(`Failed to save the cover photo: ${apiErrorMessage(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    setBusy(true);
    try {
      done((await projectAPI.removeCover(projectId)).data);
    } catch (err) {
      setError(`Failed to remove the cover photo: ${apiErrorMessage(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const version = project?.cover_updated_at;
  const pick = () => fileInput.current?.click();
  const input = (
    <input ref={fileInput} type="file" accept={COVER_ACCEPT} style={{ display: 'none' }} onChange={choose} aria-label="Cover photo file" />
  );
  const errorLine = error && (
    <div role="alert" style={{ color: 'var(--danger)', fontSize: 13, padding: '6px 2px 0' }}>
      {error}
    </div>
  );

  if (!version) {
    return (
      <div className="ov-overview-cover" data-help-key="overview.cover" style={{ marginBottom: 16 }}>
        <div
          style={{
            display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10, flexWrap: 'wrap',
            border: '1px dashed var(--border)', borderRadius: 6, padding: '10px 14px', color: 'var(--text-muted)', fontSize: 13,
          }}
        >
          <span>Give the project a cover photo: it shows here and on the project in All projects.</span>
          <button className="button-secondary" style={coverButton} onClick={pick} disabled={busy}>
            {busy ? 'Uploading…' : 'Add a cover photo'}
          </button>
        </div>
        {input}
        {errorLine}
      </div>
    );
  }

  return (
    <div className="ov-overview-cover has-photo" data-help-key="overview.cover" style={{ marginBottom: figuresSlot ? 0 : 16 }}>
      <div style={{ position: 'relative', borderRadius: 6, overflow: 'hidden', border: '1px solid var(--border)', background: 'var(--surface-hover)' }}>
        <img
          src={projectAPI.coverUrl(projectId, version)}
          alt={project?.name ? `${project.name} cover photo` : 'Project cover photo'}
          style={{ display: 'block', width: '100%', height: isPhone ? 190 : 260, objectFit: 'cover' }}
        />
        {figuresSlot && (
          <>
            {/* A shade along the foot, so the figures stand out from a bright photo. */}
            <div
              aria-hidden
              style={{ position: 'absolute', inset: 'auto 0 0 0', height: '55%', background: 'linear-gradient(transparent, rgba(0, 0, 0, 0.35))', pointerEvents: 'none' }}
            />
            <div ref={figuresSlot} style={{ position: 'absolute', left: 12, right: 12, bottom: 12, maxWidth: isPhone ? undefined : 480 }} />
          </>
        )}
        <div style={{ position: 'absolute', right: 10, top: 10, display: 'flex', gap: 8 }}>
          <button className="button-secondary" style={coverButton} onClick={pick} disabled={busy}>
            {busy ? 'Saving…' : 'Change cover'}
          </button>
          <button className="button-secondary" style={coverButton} onClick={remove} disabled={busy}>
            Remove
          </button>
        </div>
      </div>
      {input}
      {errorLine}
    </div>
  );
};
