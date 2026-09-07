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
      className="safe-area-top"
      style={{
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
        flexWrap: 'wrap',
        gap: '8px 12px',
        padding: '12px 16px',
        backgroundColor: 'var(--surface)',
        borderBottom: '1px solid var(--neutral-soft)',
        marginBottom: '24px',
      }}
    >
      {/* minWidth: 0 lets the workspace switcher shrink and truncate on a
          phone instead of pushing the account controls off the right edge. */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '12px', minWidth: 0, flex: '1 1 auto' }}>
        <img
          src="/Images/logo.png"
          alt="OpenV Logo"
          className="app-logo"
          style={{ height: '44px', width: 'auto', flexShrink: 0 }}
        />
        {showWorkspaceControls && <OrgSwitcher variant="light" />}
      </div>

      <div style={{ flex: '1 1 auto', textAlign: 'center' }}>
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
