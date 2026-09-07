import React, { useState } from 'react';
import { useLocation } from 'react-router-dom';
import { helpTopicForPath } from './helpTopics';
import './HelpSidebar.css';

// Context-aware help panel: a floating ? button that expands into the help
// for the page you are on. Mounted in ProjectLayout (every project page) and
// on the projects list. The content and the route→topic mapping live in
// helpTopics.ts; this file is only the rendering. It reads the current route and
// shows a short summary + practical tips for that page, a deep link into the
// most relevant manual chapter, and a link to the full manual. Manual links
// open in a new tab (the /manual routes full-load fine as an SPA entry).
//
// Chapter slugs must match MANUAL_CHAPTERS in src/manual/index.ts — they are
// deep-linked as /manual/<slug>.

interface HelpSidebarProps {
  /** Controlled open state (the compact shell keeps it, with the button in
   *  its top bar); omit for the floating button to manage it. */
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}

export const HelpSidebar: React.FC<HelpSidebarProps> = ({ open, onOpenChange }) => {
  const [ownExpanded, setOwnExpanded] = useState(false);
  const controlled = open !== undefined;
  const isExpanded = controlled ? open : ownExpanded;
  const setIsExpanded = (next: boolean) => {
    if (onOpenChange) onOpenChange(next);
    if (!controlled) setOwnExpanded(next);
  };
  const location = useLocation();
  const topic = helpTopicForPath(location.pathname);

  return (
    <div className={`help-sidebar ${isExpanded ? 'expanded' : 'collapsed'}`}>
      {/* Controlled by the shell's own button on phones: a floating button
          there lands on whatever composer sits at the bottom of the page. */}
      {(!controlled || isExpanded) && (
        <button
          className="help-toggle"
          onClick={() => setIsExpanded(!isExpanded)}
          title={isExpanded ? 'Collapse help' : 'Expand help'}
          aria-label={isExpanded ? 'Close help' : 'Help'}
        >
          {isExpanded ? '✕' : '?'}
        </button>
      )}

      {isExpanded && (
        <div className="help-content">
          <h3>{topic.title}</h3>

          <section className="help-section">
            <p className="help-intro">{topic.summary}</p>
          </section>

          <section className="help-section">
            <h4>Tips</h4>
            <ul>
              {topic.tips.map((tip) => (
                <li key={tip}>{tip}</li>
              ))}
            </ul>
          </section>

          <section className="help-section">
            <h4>Learn more</h4>
            <div className="help-item">
              <a
                href={`/manual/${topic.chapter.slug}`}
                target="_blank"
                rel="noopener noreferrer"
              >
                {topic.chapter.title} →
              </a>
              <p>The manual chapter for this page (opens in a new tab).</p>
            </div>
            {topic.also?.map((extra) => (
              <div className="help-item" key={extra.slug}>
                <a href={`/manual/${extra.slug}`} target="_blank" rel="noopener noreferrer">
                  {extra.title} →
                </a>
                <p>Related chapter (opens in a new tab).</p>
              </div>
            ))}
            <a
              className="help-manual-link"
              href="/manual"
              target="_blank"
              rel="noopener"
            >
              Full manual →
            </a>
          </section>
        </div>
      )}
    </div>
  );
};

export default HelpSidebar;
