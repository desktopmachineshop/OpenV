import React, { useEffect, useState } from 'react';
import { AgentDef, CrewGraph, CrewNode, crewsAPI, OrgMember } from '../../api/client';

interface NodeConfigPanelProps {
  node: CrewNode;
  graph: CrewGraph;
  agents: AgentDef[];
  members: OrgMember[];
  onChanged: () => void;
  onClose: () => void;
}

export const NodeConfigPanel: React.FC<NodeConfigPanelProps> = ({
  node,
  graph,
  agents,
  members,
  onChanged,
  onClose,
}) => {
  const isHuman = node.node_type === 'human';
  const [label, setLabel] = useState(node.label);
  const [agentId, setAgentId] = useState(node.agent_id || '');
  const [userId, setUserId] = useState(node.user_id || '');
  const [department, setDepartment] = useState(node.department || '');
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setLabel(node.label);
    setAgentId(node.agent_id || '');
    setUserId(node.user_id || '');
    setDepartment(node.department || '');
    setError('');
  }, [node]);

  const existingDepartments = Array.from(
    new Set(graph.nodes.map((n) => (n.department || '').trim()).filter(Boolean))
  );

  const isEntry = graph.team.entry_node_id === node.id;

  const save = async () => {
    setSaving(true);
    setError('');
    try {
      if (isHuman) {
        await crewsAPI.updateNode(node.id, { label, user_id: userId, department });
      } else {
        await crewsAPI.updateNode(node.id, { label, agent_id: agentId, department });
      }
      onChanged();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to save node');
    } finally {
      setSaving(false);
    }
  };

  const setEntry = async () => {
    setError('');
    try {
      await crewsAPI.update(graph.team.id, { entry_node_id: node.id });
      onChanged();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to set entry node');
    }
  };

  const remove = async () => {
    if (!window.confirm(`Remove node "${node.label}" from the crew?`)) return;
    setError('');
    try {
      await crewsAPI.removeNode(node.id);
      onClose();
      onChanged();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to remove node');
    }
  };

  return (
    <div className="card" style={{ marginBottom: 0 }}>
      <div style={{ display: 'flex', alignItems: 'center', marginBottom: 12 }}>
        <h3 style={{ margin: 0, flex: 1, fontSize: 15 }}>
          {isHuman ? 'Person' : 'Node'}{' '}
          {isHuman && (
            <span
              style={{
                fontSize: 11,
                background: '#8e44ad',
                color: '#fff',
                borderRadius: 10,
                padding: '2px 8px',
                marginLeft: 6,
              }}
            >
              human
            </span>
          )}
          {!isHuman && isEntry && (
            <span
              style={{
                fontSize: 11,
                background: '#3498db',
                color: '#fff',
                borderRadius: 10,
                padding: '2px 8px',
                marginLeft: 6,
              }}
            >
              entry
            </span>
          )}
        </h3>
        <button
          onClick={onClose}
          style={{ background: 'none', border: 'none', fontSize: 18, cursor: 'pointer', color: '#7f8c8d' }}
        >
          ×
        </button>
      </div>
      {error && <div style={{ color: '#e74c3c', fontSize: 12, marginBottom: 8 }}>{error}</div>}
      <div className="form-group">
        <label>Label</label>
        <input value={label} onChange={(e) => setLabel(e.target.value)} style={{ fontSize: 13 }} />
      </div>
      {isHuman ? (
        <div className="form-group">
          <label>Person</label>
          <select value={userId} onChange={(e) => setUserId(e.target.value)} style={{ fontSize: 13 }}>
            {!members.some((m) => m.user_id === userId) && userId && (
              <option value={userId}>{node.user_name || userId}</option>
            )}
            {members.map((m) => (
              <option key={m.user_id} value={m.user_id}>
                {m.user_name} ({m.user_email})
              </option>
            ))}
          </select>
        </div>
      ) : (
        <div className="form-group">
          <label>Agent</label>
          <select value={agentId} onChange={(e) => setAgentId(e.target.value)} style={{ fontSize: 13 }}>
            {agents.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name} ({a.provider})
              </option>
            ))}
          </select>
        </div>
      )}
      <div className="form-group">
        <label>Department</label>
        <input
          value={department}
          onChange={(e) => setDepartment(e.target.value)}
          placeholder="e.g. Engineering (groups nodes on the org chart)"
          list="crew-departments"
          style={{ fontSize: 13 }}
        />
        <datalist id="crew-departments">
          {existingDepartments.map((d) => (
            <option key={d} value={d} />
          ))}
        </datalist>
      </div>
      {isHuman && (
        <div
          style={{
            fontSize: 12,
            color: '#7f8c8d',
            background: '#f5eef8',
            border: '1px solid #e8daef',
            borderRadius: 4,
            padding: '8px 10px',
            marginBottom: 12,
          }}
        >
          Hand-offs to this person create a Board card assigned to them.
        </div>
      )}
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        <button className="button" style={{ padding: '6px 14px', fontSize: 13 }} onClick={save} disabled={saving}>
          {saving ? 'Saving…' : 'Save'}
        </button>
        {!isHuman && !isEntry && (
          <button
            className="button-secondary"
            style={{ padding: '6px 14px', fontSize: 13 }}
            onClick={setEntry}
          >
            Set as entry node
          </button>
        )}
        <button
          onClick={remove}
          style={{
            background: '#e74c3c',
            color: '#fff',
            border: 'none',
            padding: '6px 14px',
            borderRadius: 4,
            cursor: 'pointer',
            fontSize: 13,
          }}
        >
          Remove node
        </button>
      </div>
    </div>
  );
};
