import React, { useState, useEffect } from 'react';
import { ChatterEntry, chatterAPI } from '../api/client';

interface ChatterPanelProps {
  artifactId?: string;
  isOpen: boolean;
  onToggle: () => void;
}

export const ChatterPanel: React.FC<ChatterPanelProps> = ({
  artifactId,
  isOpen,
  onToggle,
}) => {
  const [entries, setEntries] = useState<ChatterEntry[]>([]);
  const [newMessage, setNewMessage] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (artifactId && isOpen) {
      loadChatterEntries();
    }
  }, [artifactId, isOpen]);

  const loadChatterEntries = async () => {
    if (!artifactId) return;

    setIsLoading(true);
    setError('');

    try {
      const response = await chatterAPI.list(artifactId);
      setEntries(response.data || []);
    } catch (err: any) {
      console.error('Failed to load chatter entries:', err);
      setError(`Failed to load chatter: ${err.message}`);
    } finally {
      setIsLoading(false);
    }
  };

  const handleAddMessage = async () => {
    if (!newMessage.trim() || !artifactId) {
      return;
    }

    setError('');
    try {
      const response = await chatterAPI.create({
        artifact_id: artifactId,
        message: newMessage,
      });

      // Add the new entry to the top of the list
      setEntries([response.data, ...entries]);
      setNewMessage('');
    } catch (err: any) {
      console.error('Failed to add chatter entry:', err);
      setError(`Failed to add message: ${err.message}`);
    }
  };

  if (!isOpen) {
    return null;
  }

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        width: '100%',
        backgroundColor: '#ffffff',
        border: '1px solid #ddd',
        borderRadius: '4px',
        height: '100%',
        boxShadow: '0 2px 4px rgba(0,0,0,0.05)',
      }}
    >
      {/* Header */}
      <div
        style={{
          padding: '15px',
          borderBottom: '1px solid #ecf0f1',
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          backgroundColor: '#f9f9f9',
        }}
      >
        <h3 style={{ margin: 0, fontSize: '14px', fontWeight: 'bold' }}>
          💬 Notes
        </h3>
        <button
          onClick={onToggle}
          style={{
            background: 'none',
            border: 'none',
            cursor: 'pointer',
            fontSize: '18px',
            padding: '0',
            width: '24px',
            height: '24px',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
          }}
          title="Close Notes"
        >
          ×
        </button>
      </div>

      {/* Entries list */}
      <div
        style={{
          flex: 1,
          overflowY: 'auto',
          padding: '10px',
        }}
      >
        {isLoading && <p style={{ fontSize: '12px', color: '#666' }}>Loading notes...</p>}
        {error && <p style={{ fontSize: '12px', color: '#e74c3c' }}>{error}</p>}
        {entries.map((entry) => (
          <div
            key={entry.id}
            style={{
              marginBottom: '12px',
              padding: '8px',
              backgroundColor: entry.is_auto_entry ? '#f0f8ff' : '#fff9e6',
              border: '1px solid #e0e0e0',
              borderRadius: '4px',
            }}
          >
            <div style={{ fontSize: '11px', color: '#666', marginBottom: '4px' }}>
              {entry.is_auto_entry ? '🔄' : '✏️'} {new Date(entry.created_at).toLocaleString()}
            </div>
            <div
              style={{
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
                lineHeight: '1.4',
                color: '#333',
              }}
            >
              {entry.message}
            </div>
          </div>
        ))}
      </div>

      {/* Input form */}
      <div
        style={{
          padding: '10px',
          borderTop: '1px solid #ecf0f1',
          backgroundColor: '#f9f9f9',
        }}
      >
        <textarea
          value={newMessage}
          onChange={(e) => setNewMessage(e.target.value)}
          placeholder="Add a note..."
          style={{
            width: '100%',
            minHeight: '60px',
            padding: '8px',
            border: '1px solid #bdc3c7',
            borderRadius: '4px',
            fontFamily: 'inherit',
            fontSize: '12px',
            resize: 'vertical',
            boxSizing: 'border-box',
          }}
        />
        <button
          onClick={handleAddMessage}
          disabled={!newMessage.trim()}
          style={{
            width: '100%',
            marginTop: '8px',
            padding: '8px',
            backgroundColor: newMessage.trim() ? '#3498db' : '#bdc3c7',
            color: 'white',
            border: 'none',
            borderRadius: '4px',
            cursor: newMessage.trim() ? 'pointer' : 'not-allowed',
            fontSize: '12px',
            fontWeight: 'bold',
          }}
        >
          Add Note
        </button>
      </div>
    </div>
  );
};

export default ChatterPanel;
