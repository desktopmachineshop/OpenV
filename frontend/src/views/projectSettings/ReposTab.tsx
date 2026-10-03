import React from 'react';
import { Link } from 'react-router-dom';
import { RepoConnection, RepoForm, Setter, emptyRepoForm } from './shared';

// The Repositories tab of project settings: the project's repository
// connections and each member's own local path to them.
// Props-only (refactor plan F6): the ProjectSettings shell owns the state, the
// loads and the handlers, so a tab switch keeps unsaved input.
interface ReposTabProps {
  reposLoading: boolean;
  repos: RepoConnection[];
  repoForm: RepoForm;
  setRepoForm: Setter<RepoForm>;
  showRepoForm: boolean;
  setShowRepoForm: Setter<boolean>;
  savingRepo: boolean;
  handleSaveRepo: (e: React.FormEvent) => void;
  handleDeleteRepo: (repo: RepoConnection) => void;
  myPaths: Record<string, string>;
  setMyPaths: Setter<Record<string, string>>;
  savingMyPath: string;
  handleSaveMyPath: (repo: RepoConnection) => void;
}

export const ReposTab: React.FC<ReposTabProps> = ({
  reposLoading,
  repos,
  repoForm,
  setRepoForm,
  showRepoForm,
  setShowRepoForm,
  savingRepo,
  handleSaveRepo,
  handleDeleteRepo,
  myPaths,
  setMyPaths,
  savingMyPath,
  handleSaveMyPath,
}) => (
  <>
    <div className="card">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 10 }}>
        <h3 style={{ marginBottom: 0 }}>Repository connections</h3>
        <button
          className="button-secondary"
          style={{ padding: '6px 14px', background: 'var(--accent)' }}
          onClick={() => {
            setRepoForm(emptyRepoForm);
            setShowRepoForm(!showRepoForm);
          }}
        >
          {showRepoForm ? 'Cancel' : '+ Connect repository'}
        </button>
      </div>
      <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
        Repositories give coding agents somewhere to work. The connection identifies the
        GitHub repository; each member points it at their own clone below. Credentials come
        from the host machine's git configuration.
      </p>

      {showRepoForm && (
        <form onSubmit={handleSaveRepo} style={{ background: 'var(--surface-alt)', borderRadius: 4, padding: 14, marginBottom: 14 }}>
          <div className="form-group">
            <label>Name *</label>
            <input
              value={repoForm.name}
              onChange={(e) => setRepoForm({ ...repoForm, name: e.target.value })}
              placeholder="e.g. main-app"
              autoFocus
            />
          </div>
          <div className="form-group">
            <label>Repository URL *</label>
            <input
              value={repoForm.remote_url}
              onChange={(e) => setRepoForm({ ...repoForm, remote_url: e.target.value })}
              placeholder="https://github.com/org/repo.git"
            />
          </div>
          <div className="form-group">
            <label>Default branch</label>
            <input
              value={repoForm.default_branch}
              onChange={(e) => setRepoForm({ ...repoForm, default_branch: e.target.value })}
              placeholder="main"
            />
          </div>
          <button
            type="submit"
            className="button"
            disabled={savingRepo || !repoForm.name.trim() || !repoForm.remote_url.trim()}
          >
            {savingRepo ? 'Saving…' : repoForm.id ? 'Update repository' : 'Connect repository'}
          </button>
        </form>
      )}

      {reposLoading ? (
        <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading repositories…</div>
      ) : repos.length === 0 ? (
        <div style={{ color: 'var(--neutral)', fontSize: 13 }}>No repositories connected yet.</div>
      ) : (
        repos.map((r) => (
          <div
            key={r.id}
            style={{
              border: '1px solid var(--border-soft)',
              borderRadius: 4,
              padding: '10px 12px',
              marginBottom: 8,
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontWeight: 600, fontSize: 14, color: 'var(--text)' }}>{r.name}</div>
                <div style={{ fontSize: 12, color: 'var(--text-muted)', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                  {r.remote_url || 'no repository URL — edit to add one'}
                  {r.default_branch ? ` · ${r.default_branch}` : ''}
                </div>
              </div>
              <button
                className="button-secondary"
                style={{ padding: '5px 12px', fontSize: 12 }}
                onClick={() => {
                  setRepoForm({
                    id: r.id,
                    name: r.name,
                    remote_url: r.remote_url,
                    default_branch: r.default_branch || 'main',
                  });
                  setShowRepoForm(true);
                }}
              >
                Edit
              </button>
              <button
                onClick={() => handleDeleteRepo(r)}
                style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 13, width: 'auto', padding: '6px 8px', minHeight: 36 }}
              >
                Remove
              </button>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 8 }}>
              <label style={{ fontSize: 12, color: 'var(--text-muted)', whiteSpace: 'nowrap', marginBottom: 0 }}>
                Your local path
              </label>
              <input
                value={myPaths[r.id] ?? ''}
                onChange={(e) => setMyPaths({ ...myPaths, [r.id]: e.target.value })}
                placeholder="where this repo lives on YOUR machine"
                style={{ flex: 1, padding: '5px 8px', fontSize: 12 }}
              />
              <button
                className="button-secondary"
                style={{ padding: '5px 12px', fontSize: 12, width: 'auto' }}
                onClick={() => handleSaveMyPath(r)}
                disabled={savingMyPath === r.id || (myPaths[r.id] ?? '') === (r.my_local_path || '')}
              >
                {savingMyPath === r.id ? 'Saving…' : 'Save'}
              </button>
            </div>
          </div>
        ))
      )}
      {!reposLoading && repos.length > 0 && (
        <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 10, marginBottom: 0 }}>
          Agents run on each member's own machine, so “your local path” tells your runner
          where this repo lives for <b>you</b>. Without one set, your runs clone the remote
          URL instead.
        </p>
      )}
    </div>

    <div className="card" style={{ background: 'var(--tint-blue)', border: '1px solid var(--accent)' }}>
      <p style={{ fontSize: 13, color: 'var(--text)', marginBottom: 0 }}>
        AI providers &amp; worker keys moved →{' '}
        <Link to="/org/settings">Workspace settings</Link>. They now apply to the whole
        workspace, not a single project.
      </p>
    </div>
  </>
);
