import React from 'react';
import {
  ATTRIBUTE_DATA_TYPES,
  ArtifactTypeDef,
  AttributeDataType,
  AttributeDefinition,
  AttributeForm,
  Setter,
  td,
  th,
} from './shared';

// The Attributes tab of project settings: the project's typed custom
// attributes (issue #219) and the form that adds one.
// Props-only (refactor plan F6): the ProjectSettings shell owns the state, the
// loads and the handlers, so a tab switch keeps unsaved input.
interface AttributesTabProps {
  attrDefsLoading: boolean;
  attrDefs: AttributeDefinition[];
  handleDeleteAttribute: (def: AttributeDefinition) => void;
  attrForm: AttributeForm;
  setAttrForm: Setter<AttributeForm>;
  artifactTypes: ArtifactTypeDef[];
  savingAttr: boolean;
  handleAddAttribute: (e: React.FormEvent) => void;
}

export const AttributesTab: React.FC<AttributesTabProps> = ({
  attrDefsLoading,
  attrDefs,
  handleDeleteAttribute,
  attrForm,
  setAttrForm,
  artifactTypes,
  savingAttr,
  handleAddAttribute,
}) => (
  <div className="card">
    <h3>Custom attributes</h3>
    <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
      Define extra typed fields for this project's artifacts. They appear as inputs in the
      artifact editor and are validated on save. Project attributes override a workspace-wide
      attribute with the same key and type.
    </p>

    {attrDefsLoading ? (
      <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading…</div>
    ) : attrDefs.length === 0 ? (
      <div style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 12 }}>
        No project attributes defined yet.
      </div>
    ) : (
      <div className="table-scroll">
      <table style={{ width: '100%', borderCollapse: 'collapse', marginBottom: 16 }}>
        <thead>
          <tr>
            <th style={th}>Key</th>
            <th style={th}>Label</th>
            <th style={th}>Type</th>
            <th style={th}>Applies to</th>
            <th style={th}>Required</th>
            <th style={th}></th>
          </tr>
        </thead>
        <tbody>
          {attrDefs.map((def) => (
            <tr key={def.id}>
              <td style={td}>
                <code>{def.key}</code>
              </td>
              <td style={td}>{def.label}</td>
              <td style={td}>
                {def.data_type}
                {def.data_type === 'enum' && def.enum_values.length > 0 && (
                  <span style={{ color: 'var(--text-muted)' }}> ({def.enum_values.join(', ')})</span>
                )}
              </td>
              <td style={td}>{def.applies_to_type || 'All types'}</td>
              <td style={td}>{def.required ? 'Yes' : 'No'}</td>
              <td style={{ ...td, textAlign: 'right' }}>
                <button
                  className="button-secondary"
                  style={{ padding: '4px 8px', fontSize: 12, color: 'var(--danger)' }}
                  onClick={() => handleDeleteAttribute(def)}
                >
                  Delete
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      </div>
    )}

    <form onSubmit={handleAddAttribute} style={{ display: 'flex', flexWrap: 'wrap', gap: 10, alignItems: 'flex-end' }}>
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label htmlFor="attr-key">Key</label>
        <input
          id="attr-key"
          type="text"
          value={attrForm.key}
          onChange={(e) => setAttrForm((f) => ({ ...f, key: e.target.value }))}
          placeholder="priority"
          required
        />
      </div>
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label htmlFor="attr-label">Label</label>
        <input
          id="attr-label"
          type="text"
          value={attrForm.label}
          onChange={(e) => setAttrForm((f) => ({ ...f, label: e.target.value }))}
          placeholder="Priority"
        />
      </div>
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label htmlFor="attr-type">Type</label>
        <select
          id="attr-type"
          value={attrForm.data_type}
          onChange={(e) => setAttrForm((f) => ({ ...f, data_type: e.target.value as AttributeDataType }))}
        >
          {ATTRIBUTE_DATA_TYPES.map((t) => (
            <option key={t} value={t}>
              {t}
            </option>
          ))}
        </select>
      </div>
      {attrForm.data_type === 'enum' && (
        <div className="form-group" style={{ marginBottom: 0 }}>
          <label htmlFor="attr-enum">Options (comma-separated)</label>
          <input
            id="attr-enum"
            type="text"
            value={attrForm.enum_values}
            onChange={(e) => setAttrForm((f) => ({ ...f, enum_values: e.target.value }))}
            placeholder="low, medium, high"
          />
        </div>
      )}
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label htmlFor="attr-applies">Applies to</label>
        <select
          id="attr-applies"
          value={attrForm.applies_to_type}
          onChange={(e) => setAttrForm((f) => ({ ...f, applies_to_type: e.target.value }))}
        >
          <option value="">All types</option>
          {artifactTypes.map((t) => (
            <option key={t.value} value={t.value}>
              {t.label}
            </option>
          ))}
        </select>
      </div>
      <div className="form-group" style={{ marginBottom: 0, display: 'flex', alignItems: 'center', gap: 6 }}>
        <input
          id="attr-required"
          type="checkbox"
          checked={attrForm.required}
          onChange={(e) => setAttrForm((f) => ({ ...f, required: e.target.checked }))}
          style={{ width: 'auto' }}
        />
        <label htmlFor="attr-required" style={{ marginBottom: 0 }}>
          Required
        </label>
      </div>
      <button type="submit" className="button" disabled={savingAttr || !attrForm.key.trim()}>
        {savingAttr ? 'Adding…' : 'Add attribute'}
      </button>
    </form>
  </div>
);
