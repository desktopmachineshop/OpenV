import React, { useState } from 'react';
import { useViewport } from '../hooks/useViewport';
import { useFeature } from '../hooks/useFeature';
import { useHelpShortcut } from '../hooks/useHelpShortcut';
import { COMPACT_LAYOUT_FEATURE } from '../features';
import { HelpSidebar } from './HelpSidebar';
import { NotificationBell } from './NotificationBell';
import { OrgSwitcher } from './OrgSwitcher';
import { UserMenu } from './UserMenu';

interface NavbarProps {
  title?: React.ReactNode;
  /** Show the workspace switcher (next to the logo) and the user menu (far right). */
  showWorkspaceControls?: boolean;
  /** Page help. Pass both to put a ? button beside the notification bell —
   *  the two belong together, being about the person rather than the page.
   *  Omitted, a bar with the workspace controls mounts its own help panel
   *  where the compact layout is on (workspace settings, workspace runs,
   *  platform admin, What's new), with ? and F1 to open it. */
  helpOpen?: boolean;
  onHelpToggle?: () => void;
}

export const Navbar: React.FC<NavbarProps> = ({
  title,
  showWorkspaceControls = false,
  helpOpen,
  onHelpToggle,
}) => {
  const { isPhone } = useViewport();
  const compactLayout = useFeature(COMPACT_LAYOUT_FEATURE);
  const ownHelp = !onHelpToggle && showWorkspaceControls && compactLayout;
  const [ownHelpOpen, setOwnHelpOpen] = useState(false);
  const toggleOwnHelp = () => setOwnHelpOpen((o) => !o);
  useHelpShortcut(ownHelp, toggleOwnHelp);
  const helpToggle = onHelpToggle ?? (ownHelp ? toggleOwnHelp : undefined);
  const isHelpOpen = onHelpToggle ? !!helpOpen : ownHelpOpen;
  return (
    <>
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
          {helpToggle && (
            <button
              type="button"
              aria-label="Help"
              aria-expanded={isHelpOpen}
              aria-keyshortcuts={compactLayout ? '? F1' : undefined}
              data-help-ui=""
              onClick={helpToggle}
              title={compactLayout ? 'Help for this page (? or F1)' : 'Help for this page'}
              style={{
                width: 44,
                height: 44,
                background: 'none',
                border: 'none',
                color: 'var(--text)',
                fontSize: 20,
                fontWeight: 700,
                cursor: 'pointer',
                flexShrink: 0,
              }}
            >
              ?
            </button>
          )}
          {showWorkspaceControls && <NotificationBell variant="light" />}
          {showWorkspaceControls && <UserMenu variant="light" />}
        </div>
      </div>
      {ownHelp && <HelpSidebar open={ownHelpOpen} onOpenChange={setOwnHelpOpen} />}
    </>
  );
};
