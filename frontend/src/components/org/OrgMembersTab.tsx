import React, { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Org, OrgInvitation, OrgMember, User, orgsAPI } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { ErrorBanner, useConfirm } from '../ui';
import { Avatar } from '../Avatar';

const th: React.CSSProperties = {
  textAlign: 'left',
  fontSize: 12,
  color: 'var(--text-muted)',
  padding: '8px 10px',
  borderBottom: '1px solid var(--border-soft)',
};

const td: React.CSSProperties = {
  padding: '8px 10px',
  fontSize: 13,
  color: 'var(--text)',
  borderBottom: '1px solid var(--surface-inset)',
};

interface OrgMembersTabProps {
  org: Org;
  isAdmin: boolean;
  currentUser: User | null;
}

export const OrgMembersTab: React.FC<OrgMembersTabProps> = ({ org, isAdmin, currentUser }) => {
  const navigate = useNavigate();
  const confirm = useConfirm();
  const [members, setMembers] = useState<OrgMember[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [inviteEmail, setInviteEmail] = useState('');
  const [inviteRole, setInviteRole] = useState('member');
  const [inviting, setInviting] = useState(false);
  const [invitations, setInvitations] = useState<OrgInvitation[]>([]);
  // The one-time link for the invitation just created. The server stores
  // only its hash, so this is the single moment it can be copied — which is
  // the only way to invite anyone on a deployment with no SMTP.
  const [inviteLink, setInviteLink] = useState('');

  const flash = (msg: string) => {
    setNotice(msg);
    window.setTimeout(() => setNotice(''), 2500);
  };

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await orgsAPI.members.list(org.id);
      setMembers(res.data || []);
    } catch (err: any) {
      setError(`Failed to load members: ${apiErrorMessage(err)}`);
    } finally {
      setLoading(false);
    }
  }, [org.id]);

  const loadInvitations = useCallback(async () => {
    if (!isAdmin) return;
    try {
      const res = await orgsAPI.invitations.list(org.id);
      setInvitations(res.data || []);
    } catch {
      // Non-fatal: the members list is the point of this tab, and an older
      // server has no invitations endpoint at all.
      setInvitations([]);
    }
  }, [org.id, isAdmin]);

  useEffect(() => {
    load();
    loadInvitations();
  }, [load, loadInvitations]);

  const handleInvite = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!inviteEmail.trim()) return;
    setInviting(true);
    setError('');
    setInviteLink('');
    try {
      const res = await orgsAPI.members.add(org.id, inviteEmail.trim(), inviteRole);
      setInviteEmail('');
      // 201: the address had an account and is now a member. 202: it did
      // not, so an invitation is outstanding — either one that went out just
      // now (with its one-time link) or the one that is already in their
      // inbox, which the server does not re-send within the hour and whose
      // link it cannot show again.
      if (res.status === 202 && res.data) {
        const created = res.data as { link?: string; emailed: boolean; reason?: string };
        setInviteLink(created.link || '');
        flash(
          created.emailed
            ? 'Invitation emailed. They join the workspace when they accept it.'
            : created.reason
              ? `They already have an invitation: ${created.reason}`
              : 'Invitation created. Send them the link below — it is shown only once.'
        );
        await loadInvitations();
      } else {
        flash('Member added to the workspace.');
        await load();
      }
    } catch (err: any) {
      setError(`Failed to add member: ${apiErrorMessage(err)}`);
    } finally {
      setInviting(false);
    }
  };

  const handleRevokeInvitation = async (invitation: OrgInvitation) => {
    const ok = await confirm({
      title: 'Revoke invitation',
      message: `Revoke the invitation to ${invitation.email}? Their link stops working.`,
      confirmLabel: 'Revoke',
      danger: true,
    });
    if (!ok) return;
    try {
      await orgsAPI.invitations.revoke(org.id, invitation.id);
      setInvitations((prev) => prev.filter((i) => i.id !== invitation.id));
      setError('');
    } catch (err: any) {
      setError(`Failed to revoke invitation: ${apiErrorMessage(err)}`);
    }
  };

  const handleSetRole = async (member: OrgMember, role: string) => {
    try {
      await orgsAPI.members.setRole(org.id, member.user_id, role);
      setMembers(
        members.map((m) =>
          m.user_id === member.user_id ? { ...m, role: role as OrgMember['role'] } : m
        )
      );
      setError('');
    } catch (err: any) {
      setError(`Failed to change role: ${apiErrorMessage(err)}`);
    }
  };

  const handleRemove = async (member: OrgMember) => {
    const isSelf = currentUser?.id === member.user_id;
    const label = member.user_name || member.user_email || 'this member';
    const question = isSelf
      ? `Leave the workspace "${org.name}"? You will lose access to its projects.`
      : `Remove ${label} from the workspace?`;
    const ok = await confirm({
      title: isSelf ? 'Leave workspace' : 'Remove member',
      message: question,
      confirmLabel: isSelf ? 'Leave' : 'Remove',
      danger: true,
    });
    if (!ok) return;
    try {
      await orgsAPI.members.remove(org.id, member.user_id);
      if (isSelf) {
        navigate('/projects');
        return;
      }
      setMembers(members.filter((m) => m.user_id !== member.user_id));
      setError('');
    } catch (err: any) {
      setError(`Failed to remove member: ${apiErrorMessage(err)}`);
    }
  };

  return (
    <>
      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 16 }} />
      {notice && (
        <div
          style={{
            background: 'var(--tint-green)',
            border: '1px solid var(--success)',
            color: 'var(--success-text)',
            padding: '10px 14px',
            borderRadius: 4,
            marginBottom: 16,
            fontSize: 13,
          }}
        >
          {notice}
        </div>
      )}

      <div className="card">
        <h3>Workspace members</h3>
        {loading ? (
          <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading members…</div>
        ) : (
          <div className="table-scroll">
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr>
                <th style={th}>Member</th>
                <th style={th}>Email</th>
                <th style={{ ...th, width: 130 }}>Role</th>
                <th style={{ ...th, width: 80 }}></th>
              </tr>
            </thead>
            <tbody>
              {members.map((m) => {
                const isSelf = currentUser?.id === m.user_id;
                return (
                  <tr key={m.user_id}>
                    <td style={td}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        <Avatar src={m.avatar_url} name={m.user_name || m.user_email} />
                        <span>
                          {m.user_name || '—'}
                          {isSelf && <span style={{ color: 'var(--text-muted)', fontSize: 12 }}> (you)</span>}
                        </span>
                      </div>
                    </td>
                    <td style={td}>{m.user_email || '—'}</td>
                    <td style={td}>
                      {isAdmin ? (
                        <select
                          value={m.role}
                          onChange={(e) => handleSetRole(m, e.target.value)}
                          style={{ padding: '5px 8px', fontSize: 13 }}
                        >
                          <option value="admin">admin</option>
                          <option value="member">member</option>
                        </select>
                      ) : (
                        <span>{m.role}</span>
                      )}
                    </td>
                    <td style={{ ...td, textAlign: 'right' }}>
                      {(isAdmin || isSelf) && (
                        <button
                          onClick={() => handleRemove(m)}
                          className="compact-action"
                          style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 13, width: 'auto' }}
                        >
                          {isSelf ? 'Leave' : 'Remove'}
                        </button>
                      )}
                    </td>
                  </tr>
                );
              })}
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

      {isAdmin && invitations.length > 0 && (
        <div className="card">
          <h3>Pending invitations</h3>
          <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
            People who have been invited but have not joined yet. An invitation expires after seven
            days; revoking one stops its link working immediately.
          </p>
          <div className="table-scroll">
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr>
                  <th style={th}>Email</th>
                  <th style={{ ...th, width: 100 }}>Role</th>
                  <th style={{ ...th, width: 150 }}>Expires</th>
                  <th style={{ ...th, width: 80 }}></th>
                </tr>
              </thead>
              <tbody>
                {invitations.map((inv) => (
                  <tr key={inv.id}>
                    <td style={td}>{inv.email}</td>
                    <td style={td}>{inv.role}</td>
                    <td style={td}>{new Date(inv.expires_at).toLocaleDateString()}</td>
                    <td style={{ ...td, textAlign: 'right' }}>
                      <button
                        onClick={() => handleRevokeInvitation(inv)}
                        style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 12, width: 'auto', padding: 2 }}
                      >
                        Revoke
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {isAdmin && (
        <div className="card">
          <h3>Add member</h3>
          <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
            Someone who already has an OpenV account joins straight away. An address with no account
            gets an invitation instead — emailed when the server has SMTP configured, and otherwise
            as a link for you to pass on.
          </p>
          <form onSubmit={handleInvite} style={{ display: 'flex', gap: 10, alignItems: 'flex-end', flexWrap: 'wrap' }}>
            <div style={{ flex: 1, minWidth: 220 }}>
              <label style={{ fontSize: 12 }}>Email</label>
              <input
                type="email"
                value={inviteEmail}
                onChange={(e) => setInviteEmail(e.target.value)}
                placeholder="teammate@example.com"
              />
            </div>
            <div style={{ width: 130 }}>
              <label style={{ fontSize: 12 }}>Role</label>
              <select value={inviteRole} onChange={(e) => setInviteRole(e.target.value)}>
                <option value="admin">admin</option>
                <option value="member">member</option>
              </select>
            </div>
            <button type="submit" className="button" disabled={inviting || !inviteEmail.trim()}>
              {inviting ? 'Adding…' : 'Add member'}
            </button>
          </form>
          {inviteLink && (
            <div style={{ marginTop: 14 }}>
              <label style={{ fontSize: 12 }}>Invitation link (shown once)</label>
              <input
                readOnly
                value={inviteLink}
                onFocus={(e) => e.currentTarget.select()}
                style={{ width: '100%', fontFamily: 'monospace', fontSize: 12 }}
              />
            </div>
          )}
        </div>
      )}
    </>
  );
};
