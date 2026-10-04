import React from 'react';
import { Link } from 'react-router-dom';
import { Party, Project, Setter, td, th } from './shared';

// The General tab of project settings: the parent project (REQ-144) and the
// reference parties (REQ-147), each behind its feature key, and a note on the
// stable channel while both are off.
// Props-only (refactor plan F6): the ProjectSettings shell owns the state, the
// loads and the handlers, so a tab switch keeps unsaved input.
interface GeneralTabProps {
  flowDown: boolean;
  ownersOn: boolean;
  project: Project | null;
  parentCandidates: Project[];
  childProjects: Project[];
  savingParent: boolean;
  handleSetParent: (parentId: string) => void;
  parties: Party[];
  savingParties: boolean;
  saveParties: (next: Party[]) => void;
  handleAddParty: (e: React.FormEvent) => void;
  partyName: string;
  setPartyName: Setter<string>;
  partyNote: string;
  setPartyNote: Setter<string>;
}

export const GeneralTab: React.FC<GeneralTabProps> = ({
  flowDown,
  ownersOn,
  project,
  parentCandidates,
  childProjects,
  savingParent,
  handleSetParent,
  parties,
  savingParties,
  saveParties,
  handleAddParty,
  partyName,
  setPartyName,
  partyNote,
  setPartyNote,
}) => (
  <>
    {!flowDown && !ownersOn && (
      <div className="card">
        <p style={{ fontSize: 13, color: 'var(--text-muted)', margin: 0 }}>
          Parent projects and reference parties reach stable-channel workspaces at their next
          stable release. Switch the workspace to nightly, or preview the next release, in
          workspace settings to use them now.
        </p>
      </div>
    )}

    {flowDown && (
        <div className="card">
          <h3>Parent project</h3>
          <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
            A subsystem or supplier project sits under the system it belongs to. Requirements
            here can then <em>refine</em> the parent's requirements, and the parent's V&amp;V
            rolls those refinements up. A supplier works in the child project with editor
            rights there and viewer rights on the parent.
          </p>
          {!project ? (
            <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading project…</div>
          ) : (
            <div className="form-group" style={{ maxWidth: 420 }}>
              <label htmlFor="parent-project">This project refines</label>
              <select
                id="parent-project"
                value={project.parent_project_id || ''}
                disabled={savingParent}
                onChange={(e) => handleSetParent(e.target.value)}
              >
                <option value="">None (top-level project)</option>
                {parentCandidates.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </div>
          )}
          {childProjects.length > 0 && (
            <div style={{ fontSize: 13, color: 'var(--text)' }}>
              <div style={{ color: 'var(--text-muted)', marginBottom: 4 }}>Child projects</div>
              <ul style={{ margin: 0, paddingLeft: '1.2em' }}>
                {childProjects.map((p) => (
                  <li key={p.id}>
                    <Link to={`/projects/${p.id}/settings?tab=general`}>{p.name}</Link>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
    )}

    {ownersOn && (
        <div className="card">
          <h3>Reference parties</h3>
          <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
            Who can own an artifact here besides the members: the organisations, teams and
            suppliers this project works with. The workspace's own company is always first.
            An artifact's owner is picked from this list and the members, and a download can be
            narrowed to one owner's share of the project.
          </p>
          <table style={{ width: '100%', borderCollapse: 'collapse', marginBottom: 12 }}>
            <thead>
              <tr>
                <th style={th}>Party</th>
                <th style={th}>Note</th>
                <th style={th} />
              </tr>
            </thead>
            <tbody>
              {parties.map((p) => (
                <tr key={p.name}>
                  <td style={td}>
                    {p.name}
                    {p.default && (
                      <span style={{ marginLeft: 8, fontSize: 11, color: 'var(--text-muted)' }}>
                        this workspace
                      </span>
                    )}
                  </td>
                  <td style={td}>{p.note || ''}</td>
                  <td style={{ ...td, textAlign: 'right' }}>
                    {!p.default && (
                      <button
                        type="button"
                        className="button-secondary"
                        style={{ width: 'auto', padding: '4px 10px', fontSize: 12 }}
                        disabled={savingParties}
                        onClick={() => saveParties(parties.filter((q) => q.name !== p.name))}
                      >
                        Remove
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <form onSubmit={handleAddParty} style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            <input
              placeholder="Party name, e.g. Landing gear supplier"
              value={partyName}
              onChange={(e) => setPartyName(e.target.value)}
              style={{ flex: '1 1 200px' }}
            />
            <input
              placeholder="Note (optional)"
              value={partyNote}
              onChange={(e) => setPartyNote(e.target.value)}
              style={{ flex: '2 1 240px' }}
            />
            <button type="submit" className="button" style={{ width: 'auto' }} disabled={savingParties || !partyName.trim()}>
              Add party
            </button>
          </form>
        </div>
    )}
  </>
);
