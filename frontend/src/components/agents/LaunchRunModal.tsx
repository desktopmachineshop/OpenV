import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { AgentDef, agentsAPI } from '../../api/client';
import { Modal } from '../ui';

// The Agents page's launch dialog: a prompt for one run of an agent, in the
// page's project, which opens the Runs page on launch.

interface Props {
  agent: AgentDef;
  projectId: string | null | undefined;
  onClose: () => void;
  onError: (message: string) => void;
}

export const LaunchRunModal: React.FC<Props> = ({ agent, projectId, onClose, onError }) => {
  const navigate = useNavigate();
  const [prompt, setPrompt] = useState('');
  const [launching, setLaunching] = useState(false);

  const launch = async () => {
    if (!prompt.trim()) return;
    setLaunching(true);
    try {
      await agentsAPI.launchRun(agent.slug, { project_id: projectId || undefined, prompt: prompt.trim() });
      onClose();
      navigate(`/projects/${projectId}/agent-runs`);
    } catch (err: any) {
      onError(err.response?.data?.error || err.message || 'Failed to launch run');
    } finally {
      setLaunching(false);
    }
  };

  return (
    <Modal title={`Launch run: ${agent.name}`} width={520} onClose={onClose}>
      <div className="form-group" data-help-pane="agents.launch" data-help-key="agents.launch">
        <label>Prompt</label>
        <textarea
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          placeholder="What should this agent do?"
          style={{ minHeight: 110, fontSize: 13 }}
          autoFocus
        />
        <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 6 }}>
          Keep prompts lean — the agent fetches requirements itself via its OpenV tools.
        </div>
      </div>
      <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }} data-help-pane="agents.launch" data-help-key="agents.launch">
        <button className="button-secondary" onClick={onClose}>
          Cancel
        </button>
        <button className="button" onClick={launch} disabled={launching || !prompt.trim()}>
          {launching ? 'Launching…' : 'Launch'}
        </button>
      </div>
    </Modal>
  );
};
