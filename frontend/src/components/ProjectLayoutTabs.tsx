import React from 'react';
import { NavLink } from 'react-router-dom';
import { NavIcon, SHELL_ICONS, pageIconPath } from './ProjectLayoutIcons';

// The compact layout on a phone (the compact-layout feature key): a tab bar
// along the bottom with the four pages people open most on a phone, within
// reach of a thumb, and More, which opens the full project menu (the same
// drawer the top bar's menu button opens). The bar sits below the page in the
// shell's column rather than over it, so nothing on the page is covered.
//
// Short labels keep five tabs legible at 320 px; the drawer keeps the pages'
// full names, so "Requirements" is still reached by that name from the menu.

const TABS: { to: string; label: string; end?: boolean }[] = [
  { to: '', label: 'Overview', end: true },
  { to: 'requirements', label: 'Reqs' },
  { to: 'vv', label: 'V&V' },
  { to: 'review', label: 'Review' },
];

interface PhoneTabBarProps {
  menuOpen: boolean;
  onMore: () => void;
}

export const PhoneTabBar: React.FC<PhoneTabBarProps> = ({ menuOpen, onMore }) => (
  <nav className="ov-tabbar safe-area-bottom" aria-label="Project tabs" data-help-key="start.navigation">
    {TABS.map((tab) => (
      <NavLink key={tab.to} to={tab.to} end={tab.end} className="ov-tabbar-item">
        <NavIcon d={pageIconPath(tab.to)} size={20} />
        <span>{tab.label}</span>
      </NavLink>
    ))}
    <button type="button" className="ov-tabbar-item" aria-expanded={menuOpen} onClick={onMore}>
      <NavIcon d={SHELL_ICONS.more} size={20} />
      <span>More</span>
    </button>
  </nav>
);
