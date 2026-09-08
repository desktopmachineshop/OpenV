import React from 'react';
import { useViewport } from '../hooks/useViewport';
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
  const { isPhone } = useViewport();
  return (
    <div
      className="safe-area-top"
      style={{
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
        // A phone gets one row: logo, workspace, bell, account. The centre
        // title is dropped there because the page heading below names the
        // screen already, and wrapping the bar onto two rows cost 50 px.
        flexWrap: isPhone ? 'nowrap' : 'wrap',
        gap: isPhone ? '0 8px' : '8px 12px',
        padding: isPhone ? '8px 12px' : '12px 16px',
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
          style={{ height: isPhone ? '32px' : '44px', width: 'auto', flexShrink: 0 }}
        />
        {showWorkspaceControls && (
          <div style={{ minWidth: 0, flex: isPhone ? '1 1 auto' : '0 1 auto' }}>
            <OrgSwitcher variant="light" />
          </div>
        )}
      </div>

      {!isPhone && (
        <div style={{ flex: '1 1 auto', textAlign: 'center' }}>
          {title && (
            <div style={{ color: 'var(--text)' }}>
              {title}
            </div>
          )}
        </div>
      )}

      <div style={{ display: 'flex', alignItems: 'center', gap: isPhone ? 4 : '10px', flexShrink: 0 }}>
        {showWorkspaceControls && <NotificationBell variant="light" />}
        {showWorkspaceControls && <UserMenu variant="light" />}
      </div>
    </div>
  );
};
