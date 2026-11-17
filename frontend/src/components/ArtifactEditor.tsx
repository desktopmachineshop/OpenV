import React, { useState, useEffect, useMemo, useRef } from 'react';
import { useFeature } from '../hooks/useFeature';
import { FLOW_DOWN_FEATURE, ARTIFACT_OWNERS_FEATURE } from '../features';
import {
  Artifact,
  AttributeDefinition,
  Attachment,
  EXECUTION_METHODS,
  ExecutionMethod,
  Link,
  executionMethodOf,
  metaAPI,
  LinkedArtifact,
  projectAPI,
  membersAPI,
} from '../api/client';
import { ImageGallery } from './ImageGallery';
import { LinkPanel } from './LinkPanel';
import { ArtifactReleaseRange } from './ArtifactReleaseRange';
import {
  applyLinkDeletion,
  createPendingLinkId,
  serializePendingAdds,
} from '../utils/pendingLinks';
import { FIGURE_CITATIONS_FEATURE } from './attachmentKinds';
import { TokenMenu } from './ui/TokenMenu';
import { Picker } from './ui/Picker';
import { ArtifactPriorityField } from './ArtifactPriorityField';
import { PRIORITY_ATTR } from './artifactPriority';
import { ownerOptions as buildOwnerOptions, parentOptions, typeOptions } from './artifactEditorOptions';
import { useAppStore } from '../state/store';
import {
  ReferenceCandidate,
  ReferenceQuery,
  activeReferenceQuery,
  applyReference,
  matchReferences,
  referenceCandidates,
} from './artifactReferences';

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
  /**
   * Every figure in the project, for the "##" citations that reach across
   * artifacts. Left empty the wider menu simply offers nothing, which is the
   * right degradation: a writer is never shown a figure they cannot cite.
   */
  projectAttachments?: Attachment[];
  onUploadAttachment?: (file: File) => void;
  onUploadAttachmentVersion?: (attachmentId: string, file: File) => void;
  onRenameAttachment?: (attachmentId: string, title: string) => void;
  /** Reload the figures after one has been restored to an older version. */
  onAttachmentRestored?: () => void;
  onDeleteAttachment?: (attachmentId: string) => void;
  isUploadLoading?: boolean;
  uploadPercent?: number | null;
  links?: Link[];
  onCreateLink?: (link: Partial<Link>) => void;
  onDeleteLink?: (linkId: string) => void;
  /** Far ends of cross-project links, so their titles can be shown. */
  linked?: LinkedArtifact[];
  /** The parent project's requirements a requirement here may refine. */
  parentArtifacts?: Artifact[];
  parentProjectName?: string;
}

export const ArtifactEditor: React.FC<ArtifactEditorProps> = ({
  artifact,
  artifacts = [],
  projectId,
  initialData,
  linked = [],
  parentArtifacts = [],
  parentProjectName = '',
  onSave,
  onCancel,
  attachments = [],
  projectAttachments = [],
  onUploadAttachment,
  onUploadAttachmentVersion,
  onRenameAttachment,
  onAttachmentRestored,
  onDeleteAttachment,
  isUploadLoading,
  uploadPercent,
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
  // Who can own an artifact (REQ-147): the project's parties (the workspace
  // first) and its members, offered as suggestions; any name is accepted.
  const [owners, setOwners] = useState<{ parties: string[]; members: string[] }>({ parties: [], members: [] });
  const ownerOptions = useMemo(() => buildOwnerOptions(owners.parties, owners.members), [owners]);
  const metaTypes = useAppStore((s) => s.meta?.artifactTypes);
  const types = useMemo(() => typeOptions(metaTypes || []), [metaTypes]);
  const parents = useMemo(() => parentOptions(artifacts, artifact?.id), [artifacts, artifact?.id]);
  // Whether the artifact had a priority when the form opened: detection then
  // leaves it alone (ArtifactPriorityField).
  const [hadPriority] = useState(() => Boolean(artifact?.attributes?.[PRIORITY_ATTR]));
  const ownersOn = useFeature(ARTIFACT_OWNERS_FEATURE);
  const flowDown = useFeature(FLOW_DOWN_FEATURE);
  useEffect(() => {
    if (!effectiveProjectId) return;
    let cancelled = false;
    Promise.all([
      projectAPI.parties(effectiveProjectId).then((r) => r.data.parties.map((p) => p.name)).catch(() => [] as string[]),
      membersAPI.list(effectiveProjectId).then((r) => (r.data || []).map((m) => m.user_name || m.user_email || '')).catch(() => [] as string[]),
    ]).then(([parties, people]) => {
      if (cancelled) return;
      setOwners({ parties: parties.filter(Boolean), members: people.filter(Boolean) });
    });
    return () => {
      cancelled = true;
    };
  }, [effectiveProjectId]);
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

  // Detection calls this from an effect, so it keeps one identity.
  const setPriority = React.useCallback((value: string | undefined) => {
    setFormData((prev) => {
      const attributes = { ...(prev.attributes || {}) };
      if (value) attributes[PRIORITY_ATTR] = value;
      else delete attributes[PRIORITY_ATTR];
      return { ...prev, attributes };
    });
  }, []);

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

  // "#" in the description offers what this artifact can already point at:
  // its own figures and the artifacts it is linked to. Nothing else, so the
  // text cannot cite something the traceability matrix has never heard of.
  const bodyRef = useRef<HTMLTextAreaElement>(null);
  const [refQuery, setRefQuery] = useState<ReferenceQuery | null>(null);
  const [refHighlight, setRefHighlight] = useState(0);

  // Two menus behind one keystroke: "#" offers what this artifact is already
  // connected to, "##" offers the project's figures. Both are computed so that
  // switching between them is a re-render rather than a fetch.
  const localCandidates = useMemo(
    () => referenceCandidates(artifact, links, artifacts, attachments),
    [artifact, links, artifacts, attachments]
  );
  // "##" is gated (REQ-137): a stable-channel workspace is not offered the
  // wider menu until it has the release. Reading is never gated — a "##"
  // citation already in a description resolves for everybody, because a gate
  // that broke existing prose would be worse than no gate.
  const crossArtifactFigures = useFeature(FIGURE_CITATIONS_FEATURE);
  const projectCandidates = useMemo(
    () =>
      crossArtifactFigures
        ? referenceCandidates(artifact, links, artifacts, projectAttachments, 'project')
        : [],
    [artifact, links, artifacts, projectAttachments, crossArtifactFigures]
  );
  const candidates = refQuery?.scope === 'project' ? projectCandidates : localCandidates;
  const refMatches = useMemo(
    () => (refQuery ? matchReferences(candidates, refQuery.query).slice(0, 8) : []),
    [candidates, refQuery]
  );

  // Recompute from the caret after every keystroke or cursor move, so the menu
  // opens, filters and closes with the writer rather than lagging behind.
  //
  // The marker is read before the candidates are, because which list is on
  // offer depends on which marker was typed — and typing the second "#" has to
  // swap the menu rather than close it.
  const syncReferenceMenu = () => {
    const el = bodyRef.current;
    if (!el) {
      setRefQuery(null);
      return;
    }
    const next = activeReferenceQuery(el.value, el.selectionStart ?? 0);
    const available = next?.scope === 'project' ? projectCandidates : localCandidates;
    setRefQuery(available.length > 0 ? next : null);
    setRefHighlight(0);
  };

  const insertReference = (candidate: ReferenceCandidate) => {
    const el = bodyRef.current;
    if (!el || !refQuery) return;
    const { text, caret } = applyReference(el.value, refQuery, el.selectionStart ?? 0, candidate.ref);
    setFormData((prev) => ({ ...prev, body: text }));
    setRefQuery(null);
    // The value lands via React, so the caret is restored once it has.
    requestAnimationFrame(() => {
      el.focus();
      el.setSelectionRange(caret, caret);
    });
  };

  const handleBodyKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (!refQuery || refMatches.length === 0) return;
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setRefHighlight((i) => (i + 1) % refMatches.length);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setRefHighlight((i) => (i - 1 + refMatches.length) % refMatches.length);
    } else if (e.key === 'Enter' || e.key === 'Tab') {
      e.preventDefault();
      insertReference(refMatches[Math.min(refHighlight, refMatches.length - 1)]);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      setRefQuery(null);
    }
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
        <div className="card" style={{ marginBottom: '20px' }} data-help-pane="requirements.versions">
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

      {/* Editing is the manual's "Editing, versions & history"; a new
          artifact is "Creating & organizing artifacts". */}
      <div className="card" data-help-pane={artifact ? 'requirements.versions' : 'requirements.create'}>
        <h3>{artifact ? 'Edit Artifact Details' : 'New Artifact'}</h3>
        <form
          onSubmit={handleSubmit}
          data-help-key={artifact ? 'requirements.versions' : 'requirements.create'}
        >
          <div className="form-group">
            <label htmlFor="parent_id">Parent Artifact (Optional)</label>
            <Picker
              id="parent_id"
              value={formData.parent_id || ''}
              noneLabel="None (Top Level)"
              options={parents}
              onChange={(v) => setFormData((prev) => ({ ...prev, parent_id: v }))}
            />
          </div>

          <div className="form-group">
            <label htmlFor="type">Type</label>
            {/* The options are the backend catalogue (internal/domain/artifacts/
                types.go) through the generated contract, so a type the server
                knows is never missing here. */}
            <Picker
              id="type"
              value={formData.type || ''}
              options={types}
              onChange={(v) => setFormData((prev) => ({ ...prev, type: v }))}
            />
          </div>

          {formData.type === 'requirement' && !applicableDefs.some((d) => d.key === PRIORITY_ATTR) && (
            <ArtifactPriorityField
              projectId={effectiveProjectId}
              text={`${formData.title || ''}\n${formData.body || ''}`}
              value={typeof formData.attributes?.[PRIORITY_ATTR] === 'string' ? (formData.attributes[PRIORITY_ATTR] as string) : ''}
              initiallySet={hadPriority}
              onChange={setPriority}
            />
          )}

          {ownersOn && (
          <div className="form-group">
            <label htmlFor="owner">Owner</label>
            <Picker
              id="owner"
              freeText
              value={(formData.attributes?.owner as string) || ''}
              options={ownerOptions}
              placeholder="The party or member responsible for this artifact"
              onChange={(v) => setAttribute('owner', v)}
            />
          </div>
          )}

          <ArtifactReleaseRange
            projectId={effectiveProjectId}
            parentId={formData.parent_id}
            attributes={formData.attributes || {}}
            artifacts={artifacts}
            onChange={setAttribute}
          />

          {formData.type === 'test-case' && (
            <div className="form-group">
              <label htmlFor="execution_method">How is this verified?</label>
              <Picker
                id="execution_method"
                value={executionMethodOf(formData)}
                options={EXECUTION_METHODS.map((m) => ({ value: m.value, label: m.label, hint: m.hint }))}
                onChange={(v) => setAttribute('execution_method', v as ExecutionMethod)}
              />
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

          <div className="form-group" style={{ position: 'relative' }} data-help-key="requirements.references">
            <label htmlFor="body">Description</label>
            <textarea
              id="body"
              name="body"
              ref={bodyRef}
              value={formData.body || ''}
              onChange={(e) => {
                handleChange(e);
                syncReferenceMenu();
              }}
              onKeyDown={handleBodyKeyDown}
              onKeyUp={syncReferenceMenu}
              onClick={syncReferenceMenu}
              onBlur={() => setRefQuery(null)}
              placeholder={
                projectCandidates.length > 0
                  ? 'Enter artifact description (markdown supported) — # to reference a figure or linked artifact, ## for a figure anywhere in the project'
                  : localCandidates.length > 0
                  ? 'Enter artifact description (markdown supported) — # to reference a figure or linked artifact'
                  : 'Enter artifact description (markdown supported)'
              }
            />
            {refQuery && (
              <TokenMenu
                aria-label="Reference suggestions"
                rows={refMatches.map((c) => ({
                  key: c.ref,
                  primary: c.ref,
                  // Under "#" a linked artifact names its link type. Under
                  // "##" there is no link to name, so it says what it is.
                  secondary:
                    `${c.kind === 'figure' ? 'figure' : c.relation || 'artifact'} · ${c.label}` +
                    (c.owner ? ` · on ${c.owner}` : ''),
                }))}
                highlight={refHighlight}
                onHighlight={setRefHighlight}
                onChoose={(i) => insertReference(refMatches[i])}
              />
            )}
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
              onUploadVersion={onUploadAttachmentVersion}
              onRename={onRenameAttachment}
              onRestored={onAttachmentRestored}
              onDelete={onDeleteAttachment || (() => {})}
              isUploadLoading={isUploadLoading}
              uploadPercent={uploadPercent}
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
                linked={linked}
                extraTargets={flowDown ? parentArtifacts : []}
                extraTargetsLabel={parentProjectName}
                hiddenLinkTypes={flowDown ? [] : ['refines']}
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
        <Picker
          id={`attr-${def.key}`}
          value={typeof value === 'string' ? value : ''}
          noneLabel="— Select —"
          options={def.enum_values.map((opt) => ({ value: opt, label: opt }))}
          onChange={onChange}
        />
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
