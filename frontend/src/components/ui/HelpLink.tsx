import React from 'react';
import { useDocsHelp, useHelp } from '../../hooks/useHelp';

interface HelpLinkProps {
  /** The manual's context key to open (`vv.coverage`); it falls back to its
   *  parents, so a key whose topic is not written yet opens the page's. */
  helpKey: string;
  /** The section of the topic to scroll to. */
  fragment?: string;
  /** What it explains, for its accessible name: "Help: <label>". */
  label: string;
}

/**
 * A small ? beside a section heading, an empty state or a dialog title that
 * opens the help panel at that element's topic. Shown only where help comes
 * from the manual (the manual-help feature and the help-docs capability);
 * elsewhere it renders nothing, so a stable-channel workspace sees no change.
 * Its styles are `.help-link` in HelpSidebar.css.
 */
export const HelpLink: React.FC<HelpLinkProps> = ({ helpKey, fragment, label }) => {
  const docsHelp = useDocsHelp();
  const { openAt } = useHelp();
  if (!docsHelp) return null;
  return (
    <button
      type="button"
      className="help-link"
      aria-label={`Help: ${label}`}
      title={`Help: ${label}`}
      data-help-key={helpKey}
      onClick={(e) => {
        // A HelpLink often sits in a clickable header or row.
        e.stopPropagation();
        openAt(helpKey, fragment);
      }}
    >
      ?
    </button>
  );
};

export default HelpLink;
