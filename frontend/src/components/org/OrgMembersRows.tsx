import React from 'react';
import type { OrgInvitation, OrgMember } from '../../api/client';
import { Avatar } from '../Avatar';

// The members and the pending invitations on a phone in the refreshed
// interface (ui-refresh): a row each, the person on the first line and what
// can be done about them on the second, in place of a four-column table that
// scrolls sideways.

const row: React.CSSProperties = { padding: '10px 0', borderBottom: '1px solid var(--border-soft)', display: 'flex', flexDirection: 'column', gap: 8 };
const second: React.CSSProperties = { display: 'flex', alignItems: 'center', gap: 10, justifyContent: 'space-between' };
const quiet: React.CSSProperties = { color: 'var(--text-muted)', fontSize: 13, overflowWrap: 'anywhere' };
const danger: React.CSSProperties = { background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 14, fontWeight: 600, width: 'auto', minHeight: 44, padding: '0 4px' };

export const MemberRows: React.FC<{
  members: OrgMember[];
  selfId?: string;
  isAdmin: boolean;
  onRole: (m: OrgMember, role: string) => void;
  onRemove: (m: OrgMember) => void;
}> = ({ members, selfId, isAdmin, onRole, onRemove }) => (
  <div role="list" aria-label="Workspace members">
    {members.map((m) => {
      const isSelf = selfId === m.user_id;
      return (
        <div key={m.user_id} role="listitem" style={row}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0 }}>
            <Avatar src={m.avatar_url} name={m.user_name || m.user_email} />
            <div style={{ minWidth: 0 }}>
              <div style={{ fontWeight: 600 }}>
                {m.user_name || '—'}
                {isSelf && <span style={{ color: 'var(--text-muted)', fontSize: 12, fontWeight: 400 }}> (you)</span>}
              </div>
              <div style={quiet}>{m.user_email || '—'}</div>
            </div>
          </div>
          <div style={second}>
            {isAdmin ? (
              <select
                aria-label={`Role of ${m.user_name || m.user_email}`}
                value={m.role}
                onChange={(e) => onRole(m, e.target.value)}
                style={{ width: 'auto', minHeight: 44, padding: '0 10px', fontSize: 14 }}
              >
                <option value="admin">admin</option>
                <option value="member">member</option>
              </select>
            ) : (
              <span style={quiet}>{m.role}</span>
            )}
            {(isAdmin || isSelf) && (
              <button type="button" onClick={() => onRemove(m)} style={danger}>
                {isSelf ? 'Leave' : 'Remove'}
              </button>
            )}
          </div>
        </div>
      );
    })}
    {members.length === 0 && <p style={{ ...quiet, margin: 0, padding: '10px 0' }}>No members found.</p>}
  </div>
);

export const InvitationRows: React.FC<{ invitations: OrgInvitation[]; onRevoke: (inv: OrgInvitation) => void }> = ({ invitations, onRevoke }) => (
  <div role="list" aria-label="Pending invitations">
    {invitations.map((inv) => (
      <div key={inv.id} role="listitem" style={{ ...row, flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' }}>
        <div style={{ minWidth: 0 }}>
          <div style={{ fontWeight: 600, overflowWrap: 'anywhere' }}>{inv.email}</div>
          <div style={quiet}>
            {inv.role} · expires {new Date(inv.expires_at).toLocaleDateString()}
          </div>
        </div>
        <button type="button" onClick={() => onRevoke(inv)} style={danger}>
          Revoke
        </button>
      </div>
    ))}
  </div>
);
