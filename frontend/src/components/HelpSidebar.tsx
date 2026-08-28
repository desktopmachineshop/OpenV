import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import './HelpSidebar.css';

// Quick links into the in-app manual. Slugs must match
// MANUAL_CHAPTERS in src/manual/index.ts (deep-linked as /manual/<slug>).
const MANUAL_QUICK_LINKS: { slug: string; title: string; blurb: string }[] = [
  {
    slug: 'artifacts',
    title: 'Requirements & artifacts',
    blurb: 'Artifact types (user needs, requirements, test cases, hazards, design items), versioning, and editing.',
  },
  {
    slug: 'links',
    title: 'Traceability links',
    blurb: 'Every link type — verifies, validates, satisfies, mitigates, derives-from, decomposes-to, impacts, relates-to — and the direction rules between artifact types.',
  },
  {
    slug: 'guided-wizard',
    title: 'Guided definition & copilot',
    blurb: 'The step-by-step wizard for building out a project definition with the copilot chat.',
  },
  {
    slug: 'vv',
    title: 'V&V & test runs',
    blurb: 'Coverage rollups, gap reports, baselines, and running test campaigns.',
  },
  {
    slug: 'board',
    title: 'Kanban board',
    blurb: 'Tracking work items and agent-driven cards.',
  },
  {
    slug: 'troubleshooting',
    title: 'Troubleshooting & FAQ',
    blurb: 'Common errors and how to resolve them.',
  },
];

export const HelpSidebar: React.FC = () => {
  const [isExpanded, setIsExpanded] = useState(false);

  return (
    <div className={`help-sidebar ${isExpanded ? 'expanded' : 'collapsed'}`}>
      <button
        className="help-toggle"
        onClick={() => setIsExpanded(!isExpanded)}
        title={isExpanded ? 'Collapse help' : 'Expand help'}
      >
        {isExpanded ? '✕' : '?'}
      </button>

      {isExpanded && (
        <div className="help-content">
          <h3>Help</h3>

          <section className="help-section">
            <p className="help-intro">
              OpenV organizes a project as typed <strong>artifacts</strong> (user
              needs, requirements, test cases, hazards, design items) connected
              by directional <strong>traceability links</strong> — for example a
              test case <em>verifies</em> a requirement, a requirement{' '}
              <em>derives from</em> a user need, and a test case{' '}
              <em>validates</em> that need. The full reference for every type
              and rule lives in the in-app manual.
            </p>
            <Link to="/manual" className="help-manual-link">
              Open the full manual →
            </Link>
          </section>

          <section className="help-section">
            <h4>Manual chapters</h4>
            {MANUAL_QUICK_LINKS.map((entry) => (
              <div className="help-item" key={entry.slug}>
                <Link to={`/manual/${entry.slug}`}>{entry.title}</Link>
                <p>{entry.blurb}</p>
              </div>
            ))}
          </section>

          <section className="help-section">
            <h4>Tips</h4>
            <ul>
              <li>Links on an artifact are grouped by type, split into outgoing ("Links From") and incoming ("Links To").</li>
              <li>Use the Requirements view's link panel to create traceability; direction rules are enforced per link type.</li>
              <li>Manual chapters are deep-linkable — share a /manual/&lt;chapter&gt; URL with teammates.</li>
            </ul>
          </section>
        </div>
      )}
    </div>
  );
};

export default HelpSidebar;
