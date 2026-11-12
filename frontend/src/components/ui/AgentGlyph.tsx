import React from 'react';
import { useUiRefresh } from '../../hooks/useUiRefresh';

/**
 * The mark before an agent's name. In the refreshed interface (ui-refresh)
 * it is the Agents icon of the menu, drawn in the text's colour and size,
 * so an agent reads the same in a list as in the navigation; without the key
 * it is the robot emoji it replaces.
 */
export const AgentGlyph: React.FC = () => {
  const refresh = useUiRefresh();
  if (!refresh) return <>🤖</>;
  return (
    <svg
      width="1em"
      height="1em"
      viewBox="0 0 20 20"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.6}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      data-agent-glyph=""
      style={{ verticalAlign: '-0.125em', flex: 'none' }}
    >
      <path d="M10 3l1.5 4.5L16 9l-4.5 1.5L10 15l-1.5-4.5L4 9l4.5-1.5z" />
    </svg>
  );
};
