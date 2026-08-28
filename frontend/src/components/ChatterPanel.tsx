import React, { useState, useEffect, useCallback } from 'react';
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

  const loadChatterEntries = useCallback(async () => {
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
  }, [artifactId]);

  useEffect(() => {
    if (artifactId && isOpen) {
      loadChatterEntries();
    }
  }, [artifactId, isOpen, loadChatterEntries]);

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
        backgroundColor: 'var(--surface)',
        border: '1px solid var(--border)',
        borderRadius: '4px',
        height: '100%',
        boxShadow: '0 2px 4px rgba(0,0,0,0.05)',
      }}
    >
      {/* Header */}
      <div
        style={{
          padding: '15px',
          borderBottom: '1px solid var(--neutral-soft)',
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          backgroundColor: 'var(--surface-alt)',
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
        {isLoading && <p style={{ fontSize: '12px', color: 'var(--text-body)' }}>Loading notes...</p>}
        {error && <p style={{ fontSize: '12px', color: 'var(--danger)' }}>{error}</p>}
        {entries.map((entry) => (
          <div
            key={entry.id}
            style={{
              marginBottom: '12px',
              padding: '8px',
              backgroundColor: entry.is_auto_entry ? 'var(--tint-blue)' : 'var(--tint-yellow)',
              border: '1px solid var(--border)',
              borderRadius: '4px',
            }}
          >
            <div style={{ fontSize: '11px', color: 'var(--text-body)', marginBottom: '4px' }}>
              {entry.is_auto_entry ? '🔄' : '✏️'} {new Date(entry.created_at).toLocaleString()}
            </div>
            <div
              style={{
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
                lineHeight: '1.4',
                color: 'var(--text)',
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
          borderTop: '1px solid var(--neutral-soft)',
          backgroundColor: 'var(--surface-alt)',
        }}
      >
        <textarea
          value={newMessage}
          onChange={(e) => setNewMessage(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && e.ctrlKey) {
              e.preventDefault();
              handleAddMessage();
            }
          }}
          placeholder="Add a note..."
          style={{
            width: '100%',
            minHeight: '60px',
            padding: '8px',
            border: '1px solid var(--neutral-mid)',
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
          title="Add note (Ctrl+Enter)"
          style={{
            width: '100%',
            marginTop: '8px',
            padding: '8px',
            backgroundColor: newMessage.trim() ? 'var(--accent)' : 'var(--neutral-mid)',
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
