import React, { useState } from 'react';
import './HelpSidebar.css';

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
          <h3>Help & Wiki</h3>

          <section className="help-section">
            <h4>Artifact Types</h4>
            <div className="help-item">
              <strong>Requirement</strong>
              <p>A formal specification or specification of what the system must do. Requirements define functional and non-functional needs.</p>
            </div>
            <div className="help-item">
              <strong>Test Case</strong>
              <p>A set of conditions or variables under which a tester will determine if a system works as intended. Maps to requirements to verify they are met.</p>
            </div>
            <div className="help-item">
              <strong>Hazard</strong>
              <p>A potential source of harm or danger. Used in safety-critical systems to identify and mitigate risks.</p>
            </div>
            <div className="help-item">
              <strong>Design Item</strong>
              <p>A design decision, architecture component, or implementation detail that realizes one or more requirements.</p>
            </div>
            <div className="help-item">
              <strong>Other</strong>
              <p>A catch-all category for artifacts that don't fit the standard types, such as assumptions, constraints, or notes.</p>
            </div>
          </section>

          <section className="help-section">
            <h4>Link Types</h4>
            <div className="help-item">
              <strong>verifies</strong>
              <p>A test case or verification item that proves a requirement is met. Direction: test → requirement.</p>
            </div>
            <div className="help-item">
              <strong>satisfies</strong>
              <p>A design item or implementation that fulfills a requirement. Direction: design → requirement.</p>
            </div>
            <div className="help-item">
              <strong>mitigates</strong>
              <p>A safety feature or design element that reduces or eliminates a hazard. Direction: design → hazard.</p>
            </div>
            <div className="help-item">
              <strong>relates-to</strong>
              <p>A general relationship between artifacts that don't fit other categories. Used for loose associations.</p>
            </div>
            <div className="help-item">
              <strong>decomposes-to</strong>
              <p>A requirement that breaks down into more specific sub-requirements. Direction: parent → child.</p>
            </div>
            <div className="help-item">
              <strong>impacts</strong>
              <p>A change to one artifact that affects another. Used to track dependencies and change propagation.</p>
            </div>
          </section>

          <section className="help-section">
            <h4>Tips</h4>
            <ul>
              <li>Click "↓ Links From This Artifact" to see what this artifact traces to</li>
              <li>Click "↑ Links To This Artifact" to see what traces back to this artifact</li>
              <li>Use the Switch Project button to manage multiple projects</li>
              <li>Create links to establish traceability between artifacts</li>
              <li>Links are grouped by type for easier navigation</li>
            </ul>
          </section>
        </div>
      )}
    </div>
  );
};

export default HelpSidebar;
