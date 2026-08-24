import React, { useState } from 'react';
import { ProviderModel } from '../../api/client';

const CUSTOM = '__custom__';

interface ModelSelectProps {
  value: string;
  // Models offered for the selected provider, from the server's catalog.
  models: ProviderModel[];
  onChange: (model: string) => void;
  // Text for the empty option — the model the provider picks on its own.
  emptyLabel?: string;
  style?: React.CSSProperties;
}

// Model picker: a dropdown of the provider's known models with an escape
// hatch, since vendors ship new model ids faster than OpenV releases.
export const ModelSelect: React.FC<ModelSelectProps> = ({
  value,
  models,
  onChange,
  emptyLabel = 'provider default',
  style,
}) => {
  const [typing, setTyping] = useState(false);
  // Derived, not stored: a model the catalog doesn't know always needs the
  // text field, whichever agent or provider the form switched to. `typing`
  // only covers the empty-value case right after "Custom…" is picked.
  const custom = typing || (!!value && !models.some((m) => m.id === value));

  if (custom) {
    return (
      <div style={{ display: 'flex', gap: 6 }}>
        <input
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="model id, e.g. claude-opus-5"
          style={{ ...style, flex: 1 }}
        />
        <button
          type="button"
          className="button-secondary"
          onClick={() => {
            setTyping(false);
            onChange('');
          }}
          style={{ padding: '0 10px', fontSize: 12, width: 'auto', whiteSpace: 'nowrap' }}
          title="Back to the model list"
        >
          List
        </button>
      </div>
    );
  }

  return (
    <select
      value={value}
      onChange={(e) => {
        if (e.target.value === CUSTOM) {
          setTyping(true);
          onChange('');
        } else {
          onChange(e.target.value);
        }
      }}
      style={style}
    >
      <option value="">{emptyLabel}</option>
      {models.map((m) => (
        <option key={m.id} value={m.id}>
          {m.label || m.id}
        </option>
      ))}
      <option value={CUSTOM}>Custom…</option>
    </select>
  );
};
