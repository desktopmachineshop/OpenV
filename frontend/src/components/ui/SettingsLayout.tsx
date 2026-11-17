import React from 'react';
import { useViewport } from '../../hooks/useViewport';

// The settings screens' frame (the Settings design): a left sub-nav of the
// sections, and the open section beside it at a reading measure. On a phone
// the sub-nav becomes a row of chip tabs above the section, as the phone
// design draws it.
//
// The sections stay the WAI-ARIA tabs pattern the strip had (bug 104): one
// role="tablist" of role="tab" buttons, each naming the one panel, which the
// open tab labels. The list is vertical beside the panel and horizontal on a
// phone, and says so in aria-orientation.

export interface SettingsSection {
  key: string;
  label: string;
  /** A destructive section (Danger Zone): set apart and in the danger colour. */
  danger?: boolean;
}

interface SettingsLayoutProps {
  /** The section label above the list ("Project", "Workspace"). */
  group: string;
  sections: SettingsSection[];
  active: string;
  onSelect: (key: string) => void;
  tabId: (key: string) => string;
  panelId: string;
  /** The help pane each tab opens on (settings.<key>), if any. */
  helpPane?: (key: string) => string;
  /** What sits above the open section: its title and any notices. */
  head?: React.ReactNode;
  /**
   * "button" keeps a page's sections as the plain toggle buttons it always
   * had (the workspace settings, which e2e selects by that role, I21);
   * "tab" (the default) is the tabs pattern.
   */
  itemRole?: 'tab' | 'button';
  children: React.ReactNode;
}

export const SettingsLayout: React.FC<SettingsLayoutProps> = ({
  group,
  sections,
  active,
  onSelect,
  tabId,
  panelId,
  helpPane,
  head,
  itemRole = 'tab',
  children,
}) => {
  const phone = useViewport().isPhone;
  const tabs = (
    <div
      className="ov-settings-tabs"
      role="tablist"
      aria-label={`${group} settings sections`}
      aria-orientation={phone ? 'horizontal' : 'vertical'}
    >
      {sections.map((s) => {
        const selected = s.key === active;
        return (
          <button
            key={s.key}
            {...(itemRole === 'tab' ? { role: 'tab' } : {})}
            id={tabId(s.key)}
            {...(itemRole === 'tab'
              ? { 'aria-selected': selected, 'aria-controls': panelId }
              : { 'aria-pressed': selected })}
            data-help-pane={helpPane?.(s.key)}
            className={[phone ? 'ov-chip-tab' : 'ov-settings-nav-item', s.danger ? 'is-danger' : '']
              .filter(Boolean)
              .join(' ')}
            onClick={() => onSelect(s.key)}
            style={{ fontWeight: selected ? 600 : 400 }}
          >
            {s.label}
          </button>
        );
      })}
    </div>
  );

  if (phone) {
    return (
      <div className="ov-settings is-phone">
        <div className="ov-chip-tabs">{tabs}</div>
        <div className="ov-settings-main">
          {head}
          {children}
        </div>
      </div>
    );
  }

  return (
    <div className="ov-settings">
      <nav className="ov-settings-nav" aria-label="Settings sections">
        <span className="ov-section-label">{group}</span>
        {tabs}
      </nav>
      <div className="ov-settings-main">
        {head}
        {children}
      </div>
    </div>
  );
};
