import React, { useMemo, useRef, useState } from 'react';
import { Artifact } from '../../api/client';

interface ArtifactPickerProps {
  /** All artifacts of the project (e.g. from artifactAPI.list). */
  artifacts: Artifact[];
  /** Selected artifact ids (wire format unchanged: a string[] of ids). */
  value: string[];
  onChange: (ids: string[]) => void;
  placeholder?: string;
}

/**
 * Searchable multi-select for linking artifacts: type to filter, click to
 * add; selected artifacts show as removable chips.
 */
export const ArtifactPicker: React.FC<ArtifactPickerProps> = ({
  artifacts,
  value,
  onChange,
  placeholder = 'Search artifacts…',
}) => {
  const [query, setQuery] = useState('');
  const [open, setOpen] = useState(false);
  const blurTimer = useRef<number | undefined>(undefined);

  const byId = useMemo(() => {
    const m: Record<string, Artifact> = {};
    artifacts.forEach((a) => {
      m[a.id] = a;
    });
    return m;
  }, [artifacts]);

  const matches = useMemo(() => {
    const q = query.trim().toLowerCase();
    return artifacts
      .filter((a) => !value.includes(a.id))
      .filter(
        (a) =>
          !q ||
          a.title.toLowerCase().includes(q) ||
          a.type.toLowerCase().includes(q) ||
          a.id.toLowerCase().includes(q)
      )
      .slice(0, 30);
  }, [artifacts, value, query]);

  const add = (id: string) => {
    onChange([...value, id]);
    setQuery('');
  };

  const remove = (id: string) => onChange(value.filter((v) => v !== id));

  return (
    <div style={{ position: 'relative' }}>
      {value.length > 0 && (
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4, marginBottom: 6 }}>
          {value.map((id) => (
            <span
              key={id}
              title={byId[id] ? `${byId[id].type}: ${byId[id].title}` : id}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 4,
                maxWidth: '100%',
                background: 'var(--tint-blue)',
                border: '1px solid var(--tint-blue-border)',
                color: 'var(--text)',
                borderRadius: 10,
                padding: '2px 4px 2px 8px',
                fontSize: 11.5,
              }}
            >
              <span
                style={{
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                  maxWidth: 170,
                }}
              >
                🔗 {byId[id]?.title || id}
              </span>
              <button
                type="button"
                onClick={() => remove(id)}
                aria-label={`Remove ${byId[id]?.title || id}`}
                style={{
                  background: 'none',
                  border: 'none',
                  cursor: 'pointer',
                  color: 'var(--text-muted)',
                  fontSize: 13,
                  lineHeight: 1,
                  padding: '0 2px',
                }}
              >
                ×
              </button>
            </span>
          ))}
        </div>
      )}
      <input
        value={query}
        placeholder={placeholder}
        onChange={(e) => {
          setQuery(e.target.value);
          setOpen(true);
        }}
        onFocus={() => {
          window.clearTimeout(blurTimer.current);
          setOpen(true);
        }}
        onBlur={() => {
          // Delay so option mousedown/click can land before the list closes.
          blurTimer.current = window.setTimeout(() => setOpen(false), 150);
        }}
        style={{ fontSize: 12 }}
      />
      {open && (
        <div
          style={{
            position: 'absolute',
            top: '100%',
            left: 0,
            right: 0,
            zIndex: 50,
            background: 'var(--surface)',
            border: '1px solid var(--border)',
            borderRadius: 4,
            boxShadow: '0 4px 12px rgba(0,0,0,0.15)',
            maxHeight: 180,
            overflowY: 'auto',
          }}
        >
          {matches.length === 0 && (
            <div style={{ padding: '8px 10px', fontSize: 12, color: 'var(--text-muted)' }}>
              {artifacts.length === 0 ? 'No artifacts in this project.' : 'No matches.'}
            </div>
          )}
          {matches.map((a) => (
            <button
              key={a.id}
              type="button"
              onMouseDown={(e) => {
                // mousedown (not click) so it fires before the input blurs.
                e.preventDefault();
                add(a.id);
              }}
              style={{
                display: 'block',
                width: '100%',
                textAlign: 'left',
                background: 'none',
                border: 'none',
                borderBottom: '1px solid var(--border-soft)',
                padding: '6px 10px',
                cursor: 'pointer',
                fontSize: 12,
                color: 'var(--text)',
              }}
            >
              <span style={{ color: 'var(--text-muted)', marginRight: 6, textTransform: 'uppercase', fontSize: 10.5 }}>
                {a.type}
              </span>
              {a.title}
            </button>
          ))}
        </div>
      )}
    </div>
  );
};
