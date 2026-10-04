import React from 'react';
import { Link } from 'react-router-dom';
import { Avatar } from '../../components/Avatar';
import { OrgTeam, ProjectMember, Setter, ShareLink, ShareLinkRole, TeamGrant, User, td, th } from './shared';

// The Access tab of project settings: the project's members, adding one, the
// workspace teams granted access, and the share links (REQ-149) behind their
// feature key.
// Props-only (refactor plan F6): the ProjectSettings shell owns the state, the
// loads and the handlers, so a tab switch keeps unsaved input.
interface ShareLinksCardProps {
  shareLinksLoading: boolean;
  shareLinks: ShareLink[];
  handleRevokeShareLink: (link: ShareLink) => void;
  freshLink: ShareLink | null;
  copied: boolean;
  copyFreshLink: () => void;
  handleCreateShareLink: (e: React.FormEvent) => void;
  shareRole: ShareLinkRole;
  setShareRole: Setter<ShareLinkRole>;
  shareLabel: string;
  setShareLabel: Setter<string>;
  shareExpires: string;
  setShareExpires: Setter<string>;
  sharing: boolean;
}

interface MembersTabProps extends ShareLinksCardProps {
  currentUser: User | null;
  membersLoading: boolean;
  members: ProjectMember[];
  handleSetRole: (member: ProjectMember, role: string) => void;
  handleRemoveMember: (member: ProjectMember) => void;
  handleAddMember: (e: React.FormEvent) => void;
  addEmail: string;
  setAddEmail: Setter<string>;
  addRole: string;
  setAddRole: Setter<string>;
  addingMember: boolean;
  teamGrantsLoading: boolean;
  teamGrants: TeamGrant[];
  handleSetTeamRole: (grant: TeamGrant, role: string) => void;
  handleRevokeTeam: (grant: TeamGrant) => void;
  handleGrantTeam: (e: React.FormEvent) => void;
  grantableTeams: OrgTeam[];
  grantTeamId: string;
  setGrantTeamId: Setter<string>;
  grantRole: string;
  setGrantRole: Setter<string>;
  granting: boolean;
  shareLinksOn: boolean;
}

export const MembersTab: React.FC<MembersTabProps> = ({
  currentUser,
  membersLoading,
  members,
  handleSetRole,
  handleRemoveMember,
  handleAddMember,
  addEmail,
  setAddEmail,
  addRole,
  setAddRole,
  addingMember,
  teamGrantsLoading,
  teamGrants,
  handleSetTeamRole,
  handleRevokeTeam,
  handleGrantTeam,
  grantableTeams,
  grantTeamId,
  setGrantTeamId,
  grantRole,
  setGrantRole,
  granting,
  shareLinksOn,
  ...shareLinkCard
}) => (
  <>
    <div className="card">
      <h3>People</h3>
      {membersLoading ? (
        <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading members…</div>
      ) : (
        <div className="table-scroll">
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr>
              <th style={th}>Member</th>
              <th style={th}>Email</th>
              <th style={{ ...th, width: 140 }}>Role</th>
              <th style={{ ...th, width: 80 }}></th>
            </tr>
          </thead>
          <tbody>
            {members.map((m) => (
              <tr key={m.user_id}>
                <td style={td}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <Avatar src={m.avatar_url} name={m.user_name || m.user_email} />
                    <span>
                      {m.user_name || '—'}
                      {currentUser && m.user_id === currentUser.id && (
                        <span style={{ color: 'var(--text-muted)', fontSize: 12 }}> (you)</span>
                      )}
                    </span>
                  </div>
                </td>
                <td style={td}>{m.user_email || '—'}</td>
                <td style={td}>
                  <select
                    value={m.role}
                    onChange={(e) => handleSetRole(m, e.target.value)}
                    style={{ padding: '5px 8px', fontSize: 13 }}
                  >
                    <option value="owner">owner</option>
                    <option value="editor">editor</option>
                    <option value="reviewer">reviewer</option>
                    <option value="viewer">viewer</option>
                  </select>
                </td>
                <td style={{ ...td, textAlign: 'right' }}>
                  <button
                    onClick={() => handleRemoveMember(m)}
                    style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 13, width: 'auto', padding: '6px 8px', minHeight: 36 }}
                  >
                    Remove
                  </button>
                </td>
              </tr>
            ))}
            {members.length === 0 && (
              <tr>
                <td style={{ ...td, color: 'var(--neutral)' }} colSpan={4}>
                  No members found.
                </td>
              </tr>
            )}
          </tbody>
        </table>
        </div>
      )}
    </div>

    <div className="card">
      <h3>Add member</h3>
      <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
        The person must already have an OpenV account — invite them to sign up first, then add
        their email here.
      </p>
      <form onSubmit={handleAddMember} style={{ display: 'flex', gap: 10, alignItems: 'flex-end', flexWrap: 'wrap' }}>
        <div style={{ flex: 1, minWidth: 220 }}>
          <label style={{ fontSize: 12 }}>Email</label>
          <input
            type="email"
            value={addEmail}
            onChange={(e) => setAddEmail(e.target.value)}
            placeholder="teammate@example.com"
          />
        </div>
        <div style={{ width: 130 }}>
          <label style={{ fontSize: 12 }}>Role</label>
          <select value={addRole} onChange={(e) => setAddRole(e.target.value)}>
            <option value="owner">owner</option>
            <option value="editor">editor</option>
            <option value="reviewer">reviewer</option>
            <option value="viewer">viewer</option>
          </select>
        </div>
        <button type="submit" className="button" disabled={addingMember || !addEmail.trim()}>
          {addingMember ? 'Adding…' : 'Add member'}
        </button>
      </form>
    </div>

    <div className="card">
      <h3>Teams</h3>
      <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
        Grant a workspace team access to this project. Manage teams themselves in{' '}
        <Link to="/org/settings">Workspace settings</Link>.
      </p>
      {teamGrantsLoading ? (
        <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading team access…</div>
      ) : (
        <div className="table-scroll">
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr>
              <th style={th}>Team</th>
              <th style={{ ...th, width: 140 }}>Role</th>
              <th style={{ ...th, width: 80 }}></th>
            </tr>
          </thead>
          <tbody>
            {teamGrants.map((g) => (
              <tr key={g.org_team_id}>
                <td style={{ ...td, fontWeight: 600 }}>{g.team_name}</td>
                <td style={td}>
                  <select
                    value={g.role}
                    onChange={(e) => handleSetTeamRole(g, e.target.value)}
                    style={{ padding: '5px 8px', fontSize: 13 }}
                  >
                    <option value="owner">owner</option>
                    <option value="editor">editor</option>
                    <option value="reviewer">reviewer</option>
                    <option value="viewer">viewer</option>
                  </select>
                </td>
                <td style={{ ...td, textAlign: 'right' }}>
                  <button
                    onClick={() => handleRevokeTeam(g)}
                    style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 12, width: 'auto', padding: 2 }}
                  >
                    Revoke
                  </button>
                </td>
              </tr>
            ))}
            {teamGrants.length === 0 && (
              <tr>
                <td style={{ ...td, color: 'var(--neutral)' }} colSpan={3}>
                  No teams have access to this project yet.
                </td>
              </tr>
            )}
          </tbody>
        </table>
        </div>
      )}

      <form
        onSubmit={handleGrantTeam}
        style={{ display: 'flex', gap: 10, alignItems: 'flex-end', flexWrap: 'wrap', marginTop: 14 }}
      >
        <div style={{ flex: 1, minWidth: 220 }}>
          <label style={{ fontSize: 12 }}>Team</label>
          <select
            value={grantTeamId}
            onChange={(e) => setGrantTeamId(e.target.value)}
            disabled={grantableTeams.length === 0}
          >
            <option value="">
              {grantableTeams.length === 0
                ? 'No workspace teams left to add'
                : '-- Select a team --'}
            </option>
            {grantableTeams.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
          </select>
        </div>
        <div style={{ width: 130 }}>
          <label style={{ fontSize: 12 }}>Role</label>
          <select value={grantRole} onChange={(e) => setGrantRole(e.target.value)}>
            <option value="owner">owner</option>
            <option value="editor">editor</option>
            <option value="reviewer">reviewer</option>
            <option value="viewer">viewer</option>
          </select>
        </div>
        <button type="submit" className="button" disabled={granting || !grantTeamId}>
          {granting ? 'Granting…' : 'Grant access'}
        </button>
      </form>

      <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 14, marginBottom: 0 }}>
        Workspace admins always have owner access. A person's effective role is the highest of
        their direct grant and any team grants. A reviewer reads everything and adds notes,
        comments and mentions, but cannot change the text.
      </p>
    </div>

    {shareLinksOn && <ShareLinksCard {...shareLinkCard} />}
  </>
);

const ShareLinksCard: React.FC<ShareLinksCardProps> = ({
  shareLinksLoading,
  shareLinks,
  handleRevokeShareLink,
  freshLink,
  copied,
  copyFreshLink,
  handleCreateShareLink,
  shareRole,
  setShareRole,
  shareLabel,
  setShareLabel,
  shareExpires,
  setShareExpires,
  sharing,
}) => (
  <div className="card" style={{ marginTop: 16 }}>
    <h3>Share links</h3>
    <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 0 }}>
      A <strong>public</strong> link opens the live project, read only, for anyone who holds
      it, with no account. A <strong>reviewer</strong> link asks the holder to sign in and
      makes them a reviewer. Links unfurl with a preview when pasted into Slack, Discord,
      LinkedIn and the like. Revoke a link to close it.
    </p>
    {freshLink?.url && (
      <div
        style={{
          border: '1px solid var(--accent)',
          borderRadius: 6,
          padding: 12,
          marginBottom: 14,
          background: 'var(--accent-soft, rgba(44,142,240,0.08))',
        }}
      >
        <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 6 }}>
          Your {freshLink.role} link is ready. Copy it now: it is not shown again.
        </div>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
          <input
            readOnly
            value={freshLink.url}
            onFocus={(e) => e.currentTarget.select()}
            style={{ flex: 1, minWidth: 220, fontSize: 12 }}
            aria-label="Share link"
          />
          <button type="button" className="button" onClick={copyFreshLink} style={{ width: 'auto' }}>
            {copied ? 'Copied' : 'Copy link'}
          </button>
        </div>
      </div>
    )}
    <form
      onSubmit={handleCreateShareLink}
      style={{ display: 'flex', gap: 10, alignItems: 'flex-end', flexWrap: 'wrap', marginBottom: 14 }}
    >
      <div style={{ width: 150 }}>
        <label style={{ fontSize: 12 }}>Access</label>
        <select value={shareRole} onChange={(e) => setShareRole(e.target.value as ShareLinkRole)}>
          <option value="public">public, view only</option>
          <option value="reviewer">reviewer</option>
        </select>
      </div>
      <div style={{ flex: 1, minWidth: 160 }}>
        <label style={{ fontSize: 12 }}>Label</label>
        <input
          value={shareLabel}
          onChange={(e) => setShareLabel(e.target.value)}
          placeholder="Who this link is for"
        />
      </div>
      <div style={{ width: 160 }}>
        <label style={{ fontSize: 12 }}>Expires (optional)</label>
        <input type="date" value={shareExpires} onChange={(e) => setShareExpires(e.target.value)} />
      </div>
      <button type="submit" className="button" disabled={sharing} style={{ width: 'auto' }}>
        {sharing ? 'Creating…' : 'Create link'}
      </button>
    </form>
    {shareLinksLoading ? (
      <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading share links…</div>
    ) : shareLinks.length === 0 ? (
      <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>No share links yet.</div>
    ) : (
      <div className="table-scroll">
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr>
              <th style={th}>Label</th>
              <th style={{ ...th, width: 100 }}>Access</th>
              <th style={{ ...th, width: 120 }}>Created</th>
              <th style={{ ...th, width: 120 }}>Expires</th>
              <th style={{ ...th, width: 90 }}></th>
            </tr>
          </thead>
          <tbody>
            {shareLinks.map((l) => {
              const revoked = !!l.revoked_at;
              const expired = !!l.expires_at && new Date(l.expires_at) < new Date();
              return (
                <tr key={l.id} style={{ opacity: revoked || expired ? 0.55 : 1 }}>
                  <td style={td}>{l.label || '—'}</td>
                  <td style={td}>{l.role}</td>
                  <td style={td}>{new Date(l.created_at).toLocaleDateString()}</td>
                  <td style={td}>
                    {revoked
                      ? 'revoked'
                      : l.expires_at
                        ? `${expired ? 'expired ' : ''}${new Date(l.expires_at).toLocaleDateString()}`
                        : 'never'}
                  </td>
                  <td style={{ ...td, textAlign: 'right' }}>
                    {/* An expired link opens nothing already, as a revoked one does. */}
                    {!revoked && !expired && (
                      <button
                        onClick={() => handleRevokeShareLink(l)}
                        style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 13, width: 'auto', padding: '6px 8px', minHeight: 36 }}
                      >
                        Revoke
                      </button>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    )}
  </div>
);
