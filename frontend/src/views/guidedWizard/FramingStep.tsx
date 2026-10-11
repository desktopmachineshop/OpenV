import React from 'react';
import { helpText } from './stepStyles';

interface FramingStepProps {
  vision: string;
  problem: string;
  targetUsers: string;
  setVision: (next: string) => void;
  setProblem: (next: string) => void;
  setTargetUsers: (next: string) => void;
}

/**
 * Step 1: the product framing, saved to the product profile.
 *
 * A function that returns the step, not a component, and with no hooks: the
 * wizard calls it where the block stood, so React reconciles the same
 * element tree and reuses the previous step's nodes where the element types
 * match, which keeps the DOM, down to its attribute order, as it was. A
 * component would mount new nodes at every step change.
 */
export const renderFramingStep = ({ vision, problem, targetUsers, setVision, setProblem, setTargetUsers }: FramingStepProps): React.ReactElement => {
  return (
    <div className="card">
      <h3>Product framing</h3>
      <div style={helpText}>
        Frame the product in a few sentences. This is saved to the product profile and shown on the
        overview page.
      </div>
      <div className="form-group">
        <label>Vision</label>
        <textarea rows={3} value={vision} onChange={(e) => setVision(e.target.value)} placeholder="What does the world look like once this product succeeds?" />
      </div>
      <div className="form-group">
        <label>Problem statement</label>
        <textarea rows={3} value={problem} onChange={(e) => setProblem(e.target.value)} placeholder="What problem are we solving, for whom, and why now?" />
      </div>
      <div className="form-group">
        <label>Target users</label>
        <textarea rows={3} value={targetUsers} onChange={(e) => setTargetUsers(e.target.value)} placeholder="Who are the primary users and buyers?" />
      </div>
    </div>
  );
};
