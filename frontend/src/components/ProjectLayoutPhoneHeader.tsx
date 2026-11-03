import React from 'react';
import type { Project } from '../api/client';
import type { NavSection } from '../pages';
import { NotificationBell } from './NotificationBell';

// The top bar of the project shell on a phone or a tablet: the menu button,
// then a two-line title, the project's name small above the name of the page
// being read, as every phone screen of the design shows it, then help and
// the bell. The page's name is its label in the project menu, so the bar and
// the menu always agree; a page the menu does not list shows the project
// name alone.

/** The menu label of the page at activePath, or '' when the menu lists none. */
export const phonePageLabel = (sections: NavSection[], activePath: string): string =>
  sections.flatMap((s) => s.items).find((item) => item.to === activePath)?.label ?? '';

interface PhoneHeaderProps {
  project: Project | null;
  pageLabel: string;
  menuOpen: boolean;
  onMenuToggle: () => void;
  helpOpen: boolean;
  onHelpToggle: () => void;
}

const iconButton: React.CSSProperties = {
  width: 44,
  height: 44,
  background: 'none',
  border: 'none',
  color: 'var(--sidebar-text)',
  cursor: 'pointer',
  flexShrink: 0,
};

export const PhoneHeader: React.FC<PhoneHeaderProps> = ({
  project,
  pageLabel,
  menuOpen,
  onMenuToggle,
  helpOpen,
  onHelpToggle,
}) => {
  const name = project?.name || '…';
  return (
    <header className="safe-area-top ov-phone-header">
      <button
        type="button"
        aria-label="Project menu"
        aria-expanded={menuOpen}
        onClick={onMenuToggle}
        style={{ ...iconButton, width: 48, height: 48, fontSize: 22 }}
      >
        ☰
      </button>
      <div className="ov-phone-title" title={pageLabel ? `${name} · ${pageLabel}` : name}>
        {pageLabel ? (
          <>
            <span className="ov-phone-title-project">{name}</span>
            <span className="ov-phone-title-page">{pageLabel}</span>
          </>
        ) : (
          <span className="ov-phone-title-page">{name}</span>
        )}
      </div>
      <button
        type="button"
        aria-label="Help"
        aria-expanded={helpOpen}
        data-help-ui=""
        onClick={onHelpToggle}
        title="Help for this page"
        style={{ ...iconButton, fontSize: 20, fontWeight: 700 }}
      >
        ?
      </button>
      <NotificationBell variant="dark" />
    </header>
  );
};
