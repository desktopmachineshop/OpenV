import React, { useEffect, useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { orgsAPI } from '../api/client';
import { useAppStore } from '../state/store';
import { useViewport } from '../hooks/useViewport';
import { CreateOrgModal } from './CreateOrgModal';

interface OrgSwitcherProps {
  /** 'dark' fits the sidebar (var(--text)); 'light' fits white page headers. */
  variant?: 'dark' | 'light';
}

// Workspace switcher dropdown: shows the active org and lets the user switch
// workspaces, open workspace settings or create a company workspace.
export const OrgSwitcher: React.FC<OrgSwitcherProps> = ({ variant = 'light' }) => {
  const navigate = useNavigate();
  const location = useLocation();
  const { orgs, activeOrgId, setActiveOrgId } = useAppStore();
  const [open, setOpen] = useState(false);
  const [showCreate, setShowCreate] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const { isPhone } = useViewport();
  // Where the trigger sits when the menu opens: on a phone the menu is
  // fixed to the screen (full width) instead of hanging off the trigger,
  // so a long workspace name can never push it past the right edge.
  const [menuTop, setMenuTop] = useState(0);

  const activeOrg = orgs.find((o) => o.id === activeOrgId) || null;
  const dark = variant === 'dark';

  useEffect(() => {
    if (!open) return;
    const onClickOutside = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener('mousedown', onClickOutside);
    return () => document.removeEventListener('mousedown', onClickOutside);
  }, [open]);

  const selectOrg = (orgId: string) => {
    setOpen(false);
    if (orgId === activeOrgId) return;
    // Persist the session default server-side; fire and forget.
    orgsAPI.activate(orgId).catch(() => {});
    setActiveOrgId(orgId);
    // Already on workspace settings: stay there for the new workspace
    // (OrgSettings itself bounces to /projects if the org isn't available).
    if (location.pathname !== '/org/settings') {
      navigate('/projects');
    }
  };

  const pill = (text: string, bg: string, color = '#fff'): React.ReactNode => (
    <span
      style={{
        display: 'inline-block',
        padding: '1px 7px',
        borderRadius: 9,
        fontSize: 12,
        fontWeight: 600,
        background: bg,
        color,
        lineHeight: '17px',
        flexShrink: 0,
        verticalAlign: 'middle',
      }}
    >
      {text}
    </span>
  );

  const menuItemStyle: React.CSSProperties = {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    width: '100%',
    padding: '9px 14px',
    minHeight: isPhone ? 44 : undefined,
    background: 'none',
    border: 'none',
    textAlign: 'left',
    cursor: 'pointer',
    fontSize: 13,
    color: 'var(--text)',
  };

  if (orgs.length === 0) {
    // Orgs not loaded (or server predates orgs) — keep the plain brand mark.
    return (
      <div style={{ fontWeight: 700, fontSize: 18, color: dark ? 'var(--sidebar-text)' : 'var(--text)' }}>OpenV</div>
    );
  }

  return (
    <div ref={rootRef} style={{ position: 'relative' }}>
      <button
        onClick={(e) => {
          setMenuTop(e.currentTarget.getBoundingClientRect().bottom + 6);
          setOpen(!open);
        }}
        title="Switch workspace"
        aria-haspopup="menu"
        aria-expanded={open}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          width: '100%',
          minHeight: 40,
          background: 'none',
          border: 'none',
          padding: 0,
          cursor: 'pointer',
          color: dark ? 'var(--sidebar-text)' : 'var(--text)',
        }}
      >
        <span
          style={{
            fontWeight: 700,
            fontSize: 16,
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            // Narrow enough on a phone to leave room for the account controls.
            maxWidth: dark ? 110 : 'min(220px, 42vw)',
            minWidth: 0,
          }}
        >
          {activeOrg ? activeOrg.name : 'OpenV'}
        </span>
        {activeOrg?.type === 'personal' && pill('personal', dark ? 'var(--sidebar-menu-bg)' : 'var(--neutral-soft)', dark ? 'var(--sidebar-text-dim)' : 'var(--text-muted)')}
        {activeOrg?.type === 'company' && activeOrg.role === 'admin' && pill('admin', 'var(--accent)')}
        <span aria-hidden style={{ fontSize: 12, color: dark ? 'var(--sidebar-text-faint)' : 'var(--text-muted)', flexShrink: 0 }}>▼</span>
      </button>

      {open && (
        <div
          role="menu"
          style={{
            ...(isPhone
              ? { position: 'fixed', top: menuTop, left: 12, right: 12 }
              : { position: 'absolute', top: 'calc(100% + 6px)', left: 0, minWidth: 240 }),
            background: 'var(--surface)',
            borderRadius: 6,
            border: '1px solid var(--border)',
            boxShadow: '0 6px 18px rgba(0,0,0,0.18)',
            zIndex: 1500,
            overflow: 'hidden',
          }}
        >
          <div style={{ padding: '8px 14px 4px', fontSize: 12, color: 'var(--text-muted)', fontWeight: 600, textTransform: 'uppercase' }}>
            Workspaces
          </div>
          {orgs.map((org) => (
            <button key={org.id} style={menuItemStyle} onClick={() => selectOrg(org.id)}>
              <span style={{ width: 14, color: 'var(--success)', fontWeight: 700 }}>
                {org.id === activeOrgId ? '✓' : ''}
              </span>
              <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {org.name}
              </span>
              {org.type === 'personal' && pill('personal', 'var(--neutral-soft)', 'var(--text-muted)')}
              {org.type === 'company' && org.role === 'admin' && pill('admin', 'var(--accent)')}
            </button>
          ))}
          <div style={{ borderTop: '1px solid var(--border-soft)', margin: '4px 0' }} />
          <button
            style={menuItemStyle}
            onClick={() => {
              setOpen(false);
              navigate('/org/settings');
            }}
          >
            <span style={{ width: 14 }} />
            Workspace settings
          </button>
          <button
            style={{ ...menuItemStyle, color: 'var(--accent)', fontWeight: 600 }}
            onClick={() => {
              setOpen(false);
              setShowCreate(true);
            }}
          >
            <span style={{ width: 14 }} />
            + Create a company workspace
          </button>
        </div>
      )}

      {showCreate && (
        <CreateOrgModal
          onClose={() => setShowCreate(false)}
          onCreated={() => navigate('/projects')}
        />
      )}
    </div>
  );
};
