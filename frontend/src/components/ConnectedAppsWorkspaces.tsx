import React from 'react';
import type { Org } from '../api/client';

// The workspaces a connected app reaches (REQ-44), as a checklist: on the
// consent page when the app is connected, and under Connected apps when the
// member changes them later. At least one stays chosen; to reach none, the
// member disconnects the app.

export const ConnectedAppsWorkspaces: React.FC<{
  orgs: Org[];
  chosen: string[];
  onChange: (orgIds: string[]) => void;
  disabled?: boolean;
  legend?: string;
}> = ({ orgs, chosen, onChange, disabled, legend = 'Workspaces' }) => {
  const toggle = (id: string) => {
    if (chosen.includes(id)) {
      if (chosen.length > 1) onChange(chosen.filter((c) => c !== id));
    } else {
      onChange([...chosen, id]);
    }
  };
  return (
    <fieldset style={{ border: 'none', padding: 0, margin: '0 0 12px' }}>
      <legend style={{ fontSize: 13, fontWeight: 600, marginBottom: 6 }}>{legend}</legend>
      {orgs.map((o) => (
        <label key={o.id} style={{ display: 'block', fontSize: 14, marginBottom: 4 }}>
          <input
            type="checkbox"
            checked={chosen.includes(o.id)}
            onChange={() => toggle(o.id)}
            disabled={disabled || (chosen.length === 1 && chosen[0] === o.id)}
            style={{ width: 'auto', marginRight: 6 }}
          />
          {o.name}
        </label>
      ))}
    </fieldset>
  );
};
