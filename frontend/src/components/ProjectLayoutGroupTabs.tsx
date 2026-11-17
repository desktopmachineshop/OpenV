import React, { useEffect, useRef } from 'react';
import { NavLink, useLocation } from 'react-router-dom';
import type { NavSection } from '../pages';

// The row of the current group's pages (the ui-refresh feature key): under
// the top bar on a computer, under the header on a phone. It is where a page
// is named, so the page itself does not repeat its name, and it shows the
// pages next door, which the rail's flyout hid once it closed.
//
// The links are the menu's own, by the menu's labels. On a narrow screen the
// row scrolls sideways rather than wrapping.

interface GroupTabsProps {
  section: NavSection;
}

export const GroupTabs: React.FC<GroupTabsProps> = ({ section }) => {
  const location = useLocation();
  const row = useRef<HTMLElement | null>(null);
  // A row wider than the screen scrolls; the page in view is brought into it,
  // so the row never shows a group with nothing lit.
  useEffect(() => {
    const active = row.current?.querySelector<HTMLElement>('.active');
    active?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' });
  }, [location.pathname]);
  return (
  <nav ref={row} className="ov-grouptabs" aria-label={`${section.label} pages`}>
    <span className="ov-grouptabs-group" aria-hidden="true">
      {section.label}
    </span>
    {section.items.map((item) => (
      <NavLink key={item.to} to={item.to} end={item.end} className="ov-grouptabs-link">
        {item.label}
      </NavLink>
    ))}
  </nav>
  );
};
