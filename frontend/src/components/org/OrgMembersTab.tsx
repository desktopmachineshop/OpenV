import React, { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Org, OrgMember, User, orgsAPI } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { ErrorBanner, useConfirm } from '../ui';

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

  useEffect(() => {
    load();
  }, [load]);

  const handleInvite = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!inviteEmail.trim()) return;
    setInviting(true);
    setError('');
    try {
      await orgsAPI.members.add(org.id, inviteEmail.trim(), inviteRole);
      setInviteEmail('');
      flash('Member added to the workspace.');
      await load();
    } catch (err: any) {
      if (err.response?.status === 404) {
        setError(
          `No account exists for "${inviteEmail.trim()}". They need to sign up first — once they have an account, add them here by the same email.`
        );
      } else {
        setError(`Failed to add member: ${apiErrorMessage(err)}`);
      }
    } finally {
      setInviting(false);
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
                        {m.avatar_url ? (
                          <img src={m.avatar_url} alt="" style={{ width: 28, height: 28, borderRadius: '50%' }} />
                        ) : (
                          <div
                            style={{
                              width: 28,
                              height: 28,
                              borderRadius: '50%',
                              background: 'var(--accent)',
                              color: 'var(--accent-fg)',
                              display: 'flex',
                              alignItems: 'center',
                              justifyContent: 'center',
                              fontSize: 13,
                              fontWeight: 700,
                            }}
                          >
                            {(m.user_name || m.user_email || '?').charAt(0).toUpperCase()}
                          </div>
                        )}
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
                          style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 12, width: 'auto', padding: 2 }}
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
        )}
      </div>

      {isAdmin && (
        <div className="card">
          <h3>Add member</h3>
          <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
            The person must already have an OpenV account — invite them to sign up first, then add
            their email here.
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
        </div>
      )}
    </>
  );
};
