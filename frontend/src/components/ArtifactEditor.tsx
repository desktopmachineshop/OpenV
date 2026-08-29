import React, { useState, useEffect, useMemo } from 'react';
import {
  Artifact,
  AttributeDefinition,
  Attachment,
  EXECUTION_METHODS,
  ExecutionMethod,
  Link,
  executionMethodOf,
  metaAPI,
} from '../api/client';
import { ImageGallery } from './ImageGallery';
import { LinkPanel } from './LinkPanel';
import {
  applyLinkDeletion,
  createPendingLinkId,
  serializePendingAdds,
} from '../utils/pendingLinks';

interface ArtifactEditorProps {
  artifact?: Artifact;
  artifacts?: Artifact[];
  /**
   * Project the artifact belongs to. Drives the org/project-configurable
   * typed attribute definitions rendered as extra inputs (issue #219).
   */
  projectId?: string;
  /**
   * Pre-filled values for the create form (e.g. parent/type chosen via an
   * artifact's context menu). Applied whenever a new object is passed, so a
   * later context-menu choice still lands when the form is already open.
   */
  initialData?: Partial<Artifact>;
  onSave: (artifact: Partial<Artifact>) => void;
  onCancel: () => void;
  attachments?: Attachment[];
  onUploadAttachment?: (file: File) => void;
  onDeleteAttachment?: (attachmentId: string) => void;
  isUploadLoading?: boolean;
  links?: Link[];
  onCreateLink?: (link: Partial<Link>) => void;
  onDeleteLink?: (linkId: string) => void;
}

export const ArtifactEditor: React.FC<ArtifactEditorProps> = ({
  artifact,
  artifacts = [],
  projectId,
  initialData,
  onSave,
  onCancel,
  attachments = [],
  onUploadAttachment,
  onDeleteAttachment,
  isUploadLoading,
  links = [],
  onCreateLink,
  onDeleteLink,
}) => {
  const [formData, setFormData] = useState<Partial<Artifact>>(
    artifact ||
      initialData || {
        type: 'requirement',
        title: '',
        body: '',
        attributes: {},
      }
  );

  // Re-apply create-form context when it changes after mount (the form may
  // already be open when the user picks "create child/sibling" again).
  useEffect(() => {
    if (!artifact && initialData) {
      setFormData({
        type: 'requirement',
        title: '',
        body: '',
        attributes: {},
        ...initialData,
      });
    }
  }, [artifact, initialData]);

  // Org/project-configurable typed attribute definitions (issue #219). Fetched
  // for the project and filtered per artifact type when rendering.
  const [attributeDefs, setAttributeDefs] = useState<AttributeDefinition[]>([]);
  const effectiveProjectId = projectId || artifact?.project_id;
  useEffect(() => {
    if (!effectiveProjectId) {
      setAttributeDefs([]);
      return;
    }
    let cancelled = false;
    metaAPI
      .attributeDefinitions(effectiveProjectId)
      .then((res) => {
        if (!cancelled) setAttributeDefs(res.data || []);
      })
      .catch(() => {
        // No definitions / no access: render the editor without custom fields.
        if (!cancelled) setAttributeDefs([]);
      });
    return () => {
      cancelled = true;
    };
  }, [effectiveProjectId]);

  // Definitions that apply to the currently selected type (or all types).
  const applicableDefs = useMemo(
    () =>
      attributeDefs.filter(
        (d) => d.applies_to_type === '' || d.applies_to_type === formData.type
      ),
    [attributeDefs, formData.type]
  );

  const setAttribute = (key: string, value: unknown) => {
    setFormData((prev) => ({
      ...prev,
      attributes: { ...(prev.attributes || {}), [key]: value },
    }));
  };

  // Track pending link changes during edit
  const [pendingLinkAdds, setPendingLinkAdds] = useState<Partial<Link>[]>([]);
  const [pendingLinkRemoves, setPendingLinkRemoves] = useState<string[]>([]);

  // Displayed links = persisted links minus pending removals, plus pending
  // adds. Derived so a refresh of the links prop can't clobber pending edits.
  const currentLinks = useMemo<Link[]>(() => {
    const base = (links || []).filter((l) => !pendingLinkRemoves.includes(l.id));
    return [...base, ...(pendingLinkAdds as Link[])];
  }, [links, pendingLinkAdds, pendingLinkRemoves]);

  const handleChange = (
    e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>
  ) => {
    const { name, value } = e.target;
    setFormData((prev) => ({
      ...prev,
      [name]: value,
    }));
  };

  const handleCreateLinkFromEditor = (link: Partial<Link>) => {
    // Give the unsaved link a stable client-side temp id so it can be
    // individually identified (and deleted) before it is persisted.
    const pendingLink: Partial<Link> = { ...link, id: createPendingLinkId() };
    // Add to pending additions (will be saved when Update is clicked);
    // it appears in the displayed links via the derived currentLinks.
    setPendingLinkAdds((prev) => [...prev, pendingLink]);
    // DO NOT call onCreateLink here - links are only persisted on Update
  };

  const handleDeleteLinkFromEditor = (linkId: string) => {
    // Classify by id: a pending (unsaved) add carries a temp id and is
    // simply dropped from the pending list; a persisted link id is recorded
    // in pendingLinkRemoves so the Update request deletes it server-side.
    const next = applyLinkDeletion({ pendingLinkAdds, pendingLinkRemoves }, linkId);
    setPendingLinkAdds(next.pendingLinkAdds);
    setPendingLinkRemoves(next.pendingLinkRemoves);
    // The displayed links update via the derived currentLinks.

    // DO NOT call onDeleteLink here - deletions only happen on Update
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    
    // Convert empty parent_id to null for proper UUID handling
    const dataToSave = {
      ...formData,
      parent_id: formData.parent_id === '' ? null : formData.parent_id,
      // Include link change information for backend to process.
      // Temp ids are client-side only — strip them before sending.
      pendingLinkAdds: serializePendingAdds(pendingLinkAdds),
      pendingLinkRemoves,
    } as any;
    
    onSave(dataToSave);
  };

  return (
    <>
      {artifact && (
        <div className="card" style={{ marginBottom: '20px' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
            <div>
              <h3 style={{ margin: '0 0 8px 0' }}>{formData.title || 'Untitled Artifact'}</h3>
              <p style={{ margin: 0, fontSize: '12px', color: 'var(--text-muted)' }}>
                UID: <code style={{ backgroundColor: 'var(--neutral-soft)', padding: '2px 6px', borderRadius: '3px' }}>{artifact.id}</code>
              </p>
            </div>
            <div style={{ display: 'flex', gap: '10px' }}>
              <button 
                type="submit" 
                className="button"
                onClick={(e) => {
                  e.preventDefault();
                  handleSubmit(e as any);
                }}
              >
                Update
              </button>
              <button
                type="button"
                className="button-secondary"
                onClick={onCancel}
              >
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}

      <div className="card">
        <h3>{artifact ? 'Edit Artifact Details' : 'New Artifact'}</h3>
        <form onSubmit={handleSubmit}>
          <div className="form-group">
            <label htmlFor="parent_id">Parent Artifact (Optional)</label>
            <select
              id="parent_id"
              name="parent_id"
              value={formData.parent_id || ''}
              onChange={handleChange}
            >
              <option value="">None (Top Level)</option>
              {artifacts
                .filter((a) => a.id !== artifact?.id)
                .map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.title || 'Untitled'} ({a.type})
                  </option>
                ))}
            </select>
          </div>

          <div className="form-group">
            <label htmlFor="type">Type</label>
            <select
              id="type"
              name="type"
              value={formData.type || ''}
              onChange={handleChange}
            >
              {/* Mirrors the backend type catalog (internal/domain/artifacts/
                  types.go). persona/user-need were missing here even though
                  the derives-from and validates link rules target user-need,
                  which made those links impossible to create through this UI. */}
              <option value="heading">Heading</option>
              <option value="description">Description</option>
              <option value="persona">Persona</option>
              <option value="user-need">User Need</option>
              <option value="requirement">Requirement</option>
              <option value="test-case">Test Case</option>
              <option value="hazard">Hazard</option>
              <option value="design-item">Design Item</option>
              <option value="other">Other</option>
            </select>
          </div>

          {formData.type === 'test-case' && (
            <div className="form-group">
              <label htmlFor="execution_method">How is this verified?</label>
              <select
                id="execution_method"
                name="execution_method"
                value={executionMethodOf(formData)}
                onChange={(e) =>
                  setFormData({
                    ...formData,
                    attributes: {
                      ...(formData.attributes || {}),
                      execution_method: e.target.value as ExecutionMethod,
                    },
                  })
                }
              >
                {EXECUTION_METHODS.map((m) => (
                  <option key={m.value} value={m.value}>
                    {m.label}
                  </option>
                ))}
              </select>
              <small style={{ color: 'var(--text-muted)', fontSize: 12 }}>
                {EXECUTION_METHODS.find((m) => m.value === executionMethodOf(formData))?.hint}
                {executionMethodOf(formData) !== 'automated' &&
                  ' Agents are never asked to run this case, and cannot record its result.'}
              </small>
            </div>
          )}

          <div className="form-group">
            <label htmlFor="title">Title</label>
            <input
              id="title"
              type="text"
              name="title"
              value={formData.title || ''}
              onChange={handleChange}
              required
              placeholder="Enter artifact title"
            />
          </div>

          <div className="form-group">
            <label htmlFor="body">Description</label>
            <textarea
              id="body"
              name="body"
              value={formData.body || ''}
              onChange={handleChange}
              placeholder="Enter artifact description (markdown supported)"
            />
          </div>

          {applicableDefs.length > 0 && (
            <div style={{ marginTop: 8 }}>
              {applicableDefs.map((def) => (
                <AttributeField
                  key={def.id}
                  def={def}
                  value={(formData.attributes || {})[def.key]}
                  onChange={(v) => setAttribute(def.key, v)}
                />
              ))}
            </div>
          )}

          {artifact && (
            <ImageGallery
              artifactId={artifact.id}
              attachments={attachments}
              onUpload={onUploadAttachment || (() => {})}
              onDelete={onDeleteAttachment || (() => {})}
              isUploadLoading={isUploadLoading}
              showUpload={true}
            />
          )}

          {artifact && (
            <div style={{ marginTop: '30px' }}>
              <LinkPanel
                artifacts={artifacts}
                selectedArtifactId={artifact.id}
                onCreateLink={handleCreateLinkFromEditor}
                links={currentLinks}
                title="Manage Links (Edit Mode)"
                readOnly={false}
                onSelectArtifact={() => {}}
                onDeleteLink={handleDeleteLinkFromEditor}
              />
            </div>
          )}

          {!artifact && (
            <div style={{ display: 'flex', gap: '10px', marginTop: '20px' }}>
              <button type="submit" className="button">
                Create
              </button>
              <button
                type="button"
                className="button-secondary"
                onClick={onCancel}
              >
                Cancel
              </button>
            </div>
          )}
        </form>
      </div>
    </>
  );
};

// AttributeField renders one configurable typed attribute as the matching
// input for its data type (issue #219). Values are stored on the artifact's
// attributes map under the definition's key.
const AttributeField: React.FC<{
  def: AttributeDefinition;
  value: unknown;
  onChange: (value: unknown) => void;
}> = ({ def, value, onChange }) => {
  const label = (
    <label htmlFor={`attr-${def.key}`}>
      {def.label || def.key}
      {def.required && <span style={{ color: 'var(--danger)' }}> *</span>}
    </label>
  );

  if (def.data_type === 'boolean') {
    return (
      <div className="form-group" style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <input
          id={`attr-${def.key}`}
          type="checkbox"
          checked={value === true}
          onChange={(e) => onChange(e.target.checked)}
          style={{ width: 'auto' }}
        />
        {label}
      </div>
    );
  }

  if (def.data_type === 'enum') {
    return (
      <div className="form-group">
        {label}
        <select
          id={`attr-${def.key}`}
          value={typeof value === 'string' ? value : ''}
          onChange={(e) => onChange(e.target.value)}
        >
          <option value="">— Select —</option>
          {def.enum_values.map((opt) => (
            <option key={opt} value={opt}>
              {opt}
            </option>
          ))}
        </select>
      </div>
    );
  }

  const inputType = def.data_type === 'number' ? 'number' : def.data_type === 'date' ? 'date' : 'text';
  return (
    <div className="form-group">
      {label}
      <input
        id={`attr-${def.key}`}
        type={inputType}
        value={value === undefined || value === null ? '' : String(value)}
        onChange={(e) => {
          const raw = e.target.value;
          if (def.data_type === 'number') {
            onChange(raw === '' ? '' : Number(raw));
          } else {
            onChange(raw);
          }
        }}
      />
    </div>
  );
};
