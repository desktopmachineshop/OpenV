import React from 'react';
import { NotificationBell } from './NotificationBell';
import { OrgSwitcher } from './OrgSwitcher';
import { UserMenu } from './UserMenu';

interface NavbarProps {
  title?: React.ReactNode;
  /** Show the workspace switcher (next to the logo) and the user menu (far right). */
  showWorkspaceControls?: boolean;
}

export const Navbar: React.FC<NavbarProps> = ({
  title,
  showWorkspaceControls = false,
}) => {
  return (
    <div
      style={{
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
        padding: '16px 24px',
        backgroundColor: 'var(--surface)',
        borderBottom: '1px solid var(--neutral-soft)',
        marginBottom: '24px',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
        <img
          src="/Images/logo.png"
          alt="OpenV Logo"
          className="app-logo"
          style={{ height: '56px', width: 'auto' }}
        />
        {showWorkspaceControls && <OrgSwitcher variant="light" />}
      </div>

      <div style={{ flex: 1, textAlign: 'center' }}>
        {title && (
          <div style={{ color: 'var(--text)' }}>
            {title}
          </div>
        )}
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
        {showWorkspaceControls && <NotificationBell variant="light" />}
        {showWorkspaceControls && <UserMenu variant="light" />}
      </div>
    </div>
  );
};
