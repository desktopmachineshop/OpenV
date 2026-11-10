import React from 'react';
import { PLANS, type AdminUser, type AdminWorkspace } from '../../api/client';

// The Platform admin page's per-row controls, shared by its tables and the
// usage view: a workspace's plan, a person's platform-admin standing and
// their password reset link.

export const linkButton: React.CSSProperties = {
  background: 'none',
  border: '1px solid var(--border)',
  borderRadius: 4,
  color: 'var(--text)',
  cursor: 'pointer',
  fontSize: 13,
  width: 'auto',
  padding: '5px 10px',
  minHeight: 32,
};

const selectStyle: React.CSSProperties = { padding: '5px 8px', fontSize: 13, width: 'auto', minWidth: 140 };

export const PlanSelect: React.FC<{ ws: AdminWorkspace; disabled: boolean; onChange: (ws: AdminWorkspace, plan: string) => void }> = ({
  ws,
  disabled,
  onChange,
}) => (
  <select
    value={PLANS.some((p) => p.value === ws.plan) ? ws.plan : ''}
    onChange={(e) => onChange(ws, e.target.value)}
    disabled={disabled}
    aria-label={`Plan for ${ws.name}`}
    style={selectStyle}
  >
    {!PLANS.some((p) => p.value === ws.plan) && <option value="">{ws.plan}</option>}
    {/* A legacy plan only for the workspace already on it. */}
    {PLANS.filter((p) => !p.legacy || p.value === ws.plan).map((p) => (
      <option key={p.value} value={p.value}>
        {p.label}
      </option>
    ))}
  </select>
);

export interface PersonActions {
  currentUserId?: string;
  busy: string;
  onToggleAdmin: (u: AdminUser) => void;
  onIssueReset: (u: AdminUser) => void;
}

export const AdminToggle: React.FC<{ u: AdminUser } & PersonActions> = ({ u, currentUserId, busy, onToggleAdmin }) =>
  u.is_admin ? (
    <span style={{ display: 'inline-flex', gap: 8, alignItems: 'center' }}>
      <span className="badge">admin</span>
      {currentUserId && u.id !== currentUserId && (
        <button type="button" onClick={() => onToggleAdmin(u)} disabled={busy === u.id} style={linkButton}>
          Remove
        </button>
      )}
    </span>
  ) : (
    <button type="button" onClick={() => onToggleAdmin(u)} disabled={busy === u.id} style={linkButton}>
      Make admin
    </button>
  );

export const ResetLinkButton: React.FC<{ u: AdminUser } & PersonActions> = ({ u, busy, onIssueReset }) =>
  !u.auth_provider || u.auth_provider === 'password' ? (
    <button
      type="button"
      onClick={() => onIssueReset(u)}
      disabled={busy === u.id}
      style={linkButton}
      aria-label={`Make a password reset link for ${u.name || u.email}`}
    >
      Reset link
    </button>
  ) : (
    <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>via {u.auth_provider}</span>
  );
