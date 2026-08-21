import React, { useEffect, useState } from 'react';
import { CrewEdge, CrewGraph, crewsAPI } from '../../api/client';

interface EdgeConfigPanelProps {
  edge: CrewEdge;
  graph: CrewGraph;
  onChanged: () => void;
  onClose: () => void;
}

const edgeTypeColor = (type: string): string => {
  switch (type) {
    case 'delegates-to':
      return '#3498db';
    case 'hands-off-to':
      return '#27ae60';
    case 'reviews':
      return '#f39c12';
    default:
      return '#95a5a6';
  }
};

export const EdgeConfigPanel: React.FC<EdgeConfigPanelProps> = ({
  edge,
  graph,
  onChanged,
  onClose,
}) => {
  const [promptTemplate, setPromptTemplate] = useState<string>(
    (edge.config?.prompt_template as string) || ''
  );
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setPromptTemplate((edge.config?.prompt_template as string) || '');
    setError('');
  }, [edge]);

  const nodeLabel = (id: string) => graph.nodes.find((n) => n.id === id)?.label || id;
  const targetIsHuman =
    graph.nodes.find((n) => n.id === edge.to_node_id)?.node_type === 'human';

  const save = async () => {
    setSaving(true);
    setError('');
    try {
      await crewsAPI.updateEdge(edge.id, { ...edge.config, prompt_template: promptTemplate });
      onChanged();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to save edge');
    } finally {
      setSaving(false);
    }
  };

  const remove = async () => {
    if (!window.confirm('Remove this connection?')) return;
    setError('');
    try {
      await crewsAPI.removeEdge(edge.id);
      onClose();
      onChanged();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to remove edge');
    }
  };

  return (
    <div className="card" style={{ marginBottom: 0 }}>
      <div style={{ display: 'flex', alignItems: 'center', marginBottom: 12 }}>
        <h3 style={{ margin: 0, flex: 1, fontSize: 15 }}>Connection</h3>
        <button
          onClick={onClose}
          style={{ background: 'none', border: 'none', fontSize: 18, cursor: 'pointer', color: '#7f8c8d' }}
        >
          ×
        </button>
      </div>
      {error && <div style={{ color: '#e74c3c', fontSize: 12, marginBottom: 8 }}>{error}</div>}
      <div style={{ fontSize: 13, color: '#2c3e50', marginBottom: 12 }}>
        <strong>{nodeLabel(edge.from_node_id)}</strong>{' '}
        <span
          style={{
            display: 'inline-block',
            padding: '2px 10px',
            borderRadius: 10,
            background: edgeTypeColor(edge.edge_type),
            color: '#fff',
            fontSize: 11,
            fontWeight: 600,
            margin: '0 6px',
          }}
        >
          {edge.edge_type}
        </span>{' '}
        <strong>{nodeLabel(edge.to_node_id)}</strong>
      </div>
      {targetIsHuman && (
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
          This connection targets a person — it creates a Board card for this person.
        </div>
      )}
      <div className="form-group">
        <label>Prompt template</label>
        <textarea
          value={promptTemplate}
          onChange={(e) => setPromptTemplate(e.target.value)}
          placeholder="Optional template used when work flows across this connection…"
          style={{ minHeight: 90, fontSize: 13, fontFamily: 'monospace' }}
        />
      </div>
      <div style={{ display: 'flex', gap: 8 }}>
        <button className="button" style={{ padding: '6px 14px', fontSize: 13 }} onClick={save} disabled={saving}>
          {saving ? 'Saving…' : 'Save'}
        </button>
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
          Remove edge
        </button>
      </div>
    </div>
  );
};
