import React, { useCallback, useState, useEffect } from 'react';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { useAppStore } from '../state/store';
import { artifactAPI, linkAPI, attachmentAPI, baselineAPI, qualityAPI, agentsAPI, Artifact, Link, Attachment, Baseline, ProjectExport } from '../api/client';
import type { ArtifactContextAction, QualityRowInfo } from '../components/ArtifactList';
import { DropZone, planMove } from '../utils/artifactDrag';
import { ImageLightbox } from '../components/ImageLightbox';
import { isFigureRef } from '../components/artifactReferences';
import {
  PanelMode,
  loadPanelMode,
  nextPanelMode,
  panelIsOpen,
  panelModeLabel,
  panelTakesSpace,
  savePanelMode,
} from '../components/panelMode';
import { ArtifactEditor } from '../components/ArtifactEditor';
import { ArtifactList } from '../components/ArtifactList';
import { ArtifactHeader } from '../components/ArtifactHeader';
import { ArtifactDetails } from '../components/ArtifactDetails';
import { ChatterPanel } from '../components/ChatterPanel';
import { DownloadWizard } from '../components/DownloadWizard';
import { ErrorBanner, useAlert, useConfirm, usePrompt } from '../components/ui';
import { apiErrorMessage } from '../api/errors';

export const ModuleView: React.FC = () => {
  const confirm = useConfirm();
  const prompt = usePrompt();
  const alertDialog = useAlert();
  const [isCreating, setIsCreating] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const [editingArtifact, setEditingArtifact] = useState<Artifact | undefined>();
  const [error, setError] = useState<string>('');
  const [allLinks, setAllLinks] = useState<Link[]>([]);
  const [searchText, setSearchText] = useState<string>('');
  const [searchExact, setSearchExact] = useState<boolean>(false);
  const [filterLogic, setFilterLogic] = useState<'and' | 'or'>('and');
  const [filterRows, setFilterRows] = useState<Array<{ id: string; field: string; value: string; comparator: string }>>([
    { id: 'filter-1', field: 'type', value: '', comparator: 'contains' },
  ]);
  const [filterPresetName, setFilterPresetName] = useState<string>('');
  const [filterPresets, setFilterPresets] = useState<Array<{ name: string; data: string }>>([]);
  const [selectedPreset, setSelectedPreset] = useState<string>('');
  const [showFilterPanel, setShowFilterPanel] = useState<boolean>(false);
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [uploadingAttachmentId, setUploadingAttachmentId] = useState<string | null>(null);
  const [baselines, setBaselines] = useState<Baseline[]>([]);
  const [activeBaselineId, setActiveBaselineId] = useState<string>('live');
  const [baselineData, setBaselineData] = useState<ProjectExport | null>(null);
  // Per-requirement quality scores keyed by artifact id (issue #217); drives
  // the score badge on requirement rows.
  const [qualityScores, setQualityScores] = useState<Record<string, QualityRowInfo>>({});
  const [collapseAllToken, setCollapseAllToken] = useState<number>(0);
  const [expandAllToken, setExpandAllToken] = useState<number>(0);
  const [previewVersion, setPreviewVersion] = useState<Artifact | null>(null);
  // Pre-filled values for the create form when it is opened from an
  // artifact's context menu (create before/after/child). Explicit state —
  // ArtifactEditor applies it via an effect whenever it changes (issue #26).
  // How much room the notes panel takes. Same three states as the project
  // menu, remembered separately: notes and navigation are wanted at different
  // times.
  const [notesMode, setNotesMode] = useState<PanelMode>(() => loadPanelMode('artifact-notes'));
  const [notesHovered, setNotesHovered] = useState(false);
  const [pendingCreateContext, setPendingCreateContext] = useState<Partial<Artifact> | null>(null);
  // Where a "create before/after" should put the artifact once it exists. The
  // API appends new artifacts to the end of their sibling group, so without
  // this a "create after" landed at the bottom of the level rather than next
  // to the artifact that was right-clicked.
  const [pendingCreatePlacement, setPendingCreatePlacement] = useState<
    { anchorId: string; position: 'before' | 'after' } | null
  >(null);
  // "Draft test cases" launch guard: disables the button while the run is being
  // enqueued so a double-click can't launch two runs (issue #218).
  const [draftingTests, setDraftingTests] = useState(false);
  // The download wizard, opened from the one Download button above.
  const [downloadOpen, setDownloadOpen] = useState(false);

  // Resizable columns state
  const [leftColumnWidth, setLeftColumnWidth] = useState<number>(() => {
    const saved = localStorage.getItem('openv-leftColumnWidth');
    return saved ? parseInt(saved) : 400;
  });
  const [rightColumnWidth, setRightColumnWidth] = useState<number>(() => {
    const saved = localStorage.getItem('openv-rightColumnWidth');
    return saved ? parseInt(saved) : 320;
  });
  const [isResizing, setIsResizing] = useState<'left' | 'right' | null>(null);
  // Drag origin for a column resize: the pointer position and column width at
  // mousedown. Resizing is a delta from that origin rather than an absolute
  // position derived from clientX, so the divider stays under the cursor no
  // matter what sits to the left of the columns (the 200px project sidebar,
  // page padding, the handle itself) instead of snapping on grab (issue: the
  // divider jumped ~a sidebar's width left on the first mouse move).
  const resizeOrigin = React.useRef<{ side: 'left' | 'right'; startX: number; startWidth: number } | null>(null);
  // Latest width during a drag, so mouseup can persist it without the effect
  // having to re-subscribe on every mousemove.
  const liveWidths = React.useRef({ left: leftColumnWidth, right: rightColumnWidth });

  // Read the project from the URL first so a hard refresh doesn't flash
  // "No Project Selected" while ProjectLayout syncs the store.
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();

  const {
    artifacts,
    setArtifacts,
    addArtifact,
    updateArtifact,
    removeArtifact,
    addLink,
    selectedArtifactId,
    setSelectedArtifactId,
  } = useAppStore();

  // The ?artifact= search param is the shareable source of truth for the
  // selection (mirrors AgentRunsPage's ?run= pattern).
  const urlArtifactId = searchParams.get('artifact');

  const isBaselineView = activeBaselineId !== 'live';

  const normalizeParentId = (parentId?: string | null): string | null => parentId ?? null;

  const compareArtifacts = (left: Artifact, right: Artifact): number => {
    const leftOrder = left.sort_order ?? 0;
    const rightOrder = right.sort_order ?? 0;
    const leftHasOrder = leftOrder > 0;
    const rightHasOrder = rightOrder > 0;

    if (leftHasOrder && rightHasOrder) {
      return leftOrder - rightOrder;
    }

    if (!leftHasOrder && !rightHasOrder) {
      return new Date(left.created_at).getTime() - new Date(right.created_at).getTime();
    }

    return leftHasOrder ? -1 : 1;
  };

  // Start a column resize: record where the drag began so the move handler can
  // work in deltas. preventDefault keeps the browser from starting a text
  // selection or native drag under the cursor.
  const startResize = (side: 'left' | 'right') => (e: React.MouseEvent) => {
    e.preventDefault();
    resizeOrigin.current = {
      side,
      startX: e.clientX,
      startWidth: side === 'left' ? leftColumnWidth : rightColumnWidth,
    };
    setIsResizing(side);
  };

  // Handle column resizing. Depends only on isResizing: the handlers read the
  // in-flight width from a ref, so the listeners are attached once per drag
  // rather than being torn down and re-added on every mousemove.
  useEffect(() => {
    if (!isResizing) return;

    const handleMouseMove = (e: MouseEvent) => {
      const origin = resizeOrigin.current;
      if (!origin) return;

      // The left column grows as the pointer moves right; the right column
      // grows as it moves left.
      const delta = origin.side === 'left' ? e.clientX - origin.startX : origin.startX - e.clientX;
      const [min, max] = origin.side === 'left' ? [200, 800] : [250, 600];
      const newWidth = Math.max(min, Math.min(max, origin.startWidth + delta));

      liveWidths.current[origin.side] = newWidth;
      if (origin.side === 'left') {
        setLeftColumnWidth(newWidth);
      } else {
        setRightColumnWidth(newWidth);
      }
    };

    const handleMouseUp = () => {
      const origin = resizeOrigin.current;
      if (origin?.side === 'right') {
        localStorage.setItem('openv-rightColumnWidth', liveWidths.current.right.toString());
      } else if (origin?.side === 'left') {
        localStorage.setItem('openv-leftColumnWidth', liveWidths.current.left.toString());
      }
      resizeOrigin.current = null;
      setIsResizing(null);
    };

    document.addEventListener('mousemove', handleMouseMove);
    document.addEventListener('mouseup', handleMouseUp);
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';

    return () => {
      document.removeEventListener('mousemove', handleMouseMove);
      document.removeEventListener('mouseup', handleMouseUp);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };
  }, [isResizing]);

  const loadArtifacts = useCallback(async () => {
    try {
      // The module tree is assembled client-side from parent_id, so it needs
      // the complete artifact set; artifactAPI.list pages through the
      // limit/offset API (1000 per request) until it has everything. UI
      // follow-up for issue #136: lazy-load subtrees instead.
      const response = await artifactAPI.list(projectId);
      setArtifacts(response.data || []);
      setError('');
    } catch (error: any) {
      console.error('Failed to load artifacts:', error);
      setArtifacts([]);
      setError(`Failed to load artifacts: ${apiErrorMessage(error)}`);
    }
  }, [projectId, setArtifacts]);

  const loadLinks = useCallback(async () => {
    try {
      const response = await linkAPI.list(projectId);
      setAllLinks(response.data || []);
    } catch (error: any) {
      console.error('Failed to load links:', error);
      setAllLinks([]);
    }
  }, [projectId]);

  // Load rule-based quality scores for the project's requirements. Best-effort:
  // a failure just leaves the badges absent, so it never blocks the tree.
  const loadQuality = useCallback(async () => {
    if (!projectId) return;
    try {
      const response = await qualityAPI.project(projectId);
      const map: Record<string, QualityRowInfo> = {};
      (response.data.entries || []).forEach((entry) => {
        map[entry.artifact_id] = {
          score: entry.score,
          band: entry.band,
          findingCount: entry.findings.length,
        };
      });
      setQualityScores(map);
    } catch (error: any) {
      console.error('Failed to load quality scores:', error);
      setQualityScores({});
    }
  }, [projectId]);

  const loadBaselines = useCallback(async () => {
    if (!projectId) return;
    try {
      const response = await baselineAPI.list(projectId);
      setBaselines(response.data || []);
    } catch (error: any) {
      console.error('Failed to load baselines:', error);
      setBaselines([]);
    }
  }, [projectId]);

  const loadAttachments = useCallback(async (artifactId: string) => {
    try {
      const response = await attachmentAPI.listByArtifact(artifactId);
      setAttachments(response.data || []);
    } catch (error: any) {
      console.error('Failed to load attachments:', error);
      setAttachments([]);
    }
  }, []);

  // Load project data on mount and whenever the project changes
  useEffect(() => {
    if (projectId) {
      loadArtifacts();
      loadLinks();
      loadBaselines();
      loadQuality();
      setActiveBaselineId('live');
      setBaselineData(null);
    }
  }, [projectId, loadArtifacts, loadLinks, loadBaselines, loadQuality]);

  // Load attachments when artifact is selected or when editing
  useEffect(() => {
    if (isBaselineView) {
      setAttachments([]);
      return;
    }
    const artifactIdToLoad = editingArtifact?.id || selectedArtifactId;
    if (artifactIdToLoad) {
      loadAttachments(artifactIdToLoad);
    } else {
      setAttachments([]);
    }
  }, [selectedArtifactId, editingArtifact?.id, isBaselineView, loadAttachments]);

  // Sync the ?artifact= param into the store selection. This covers deep
  // links, browser back/forward, and in-app links from other views; the URL
  // wins whenever the two disagree.
  useEffect(() => {
    if (urlArtifactId === selectedArtifactId) return;
    setSelectedArtifactId(urlArtifactId);
    setIsEditing(false);
    setEditingArtifact(undefined);
    setPreviewVersion(null);
  }, [urlArtifactId, selectedArtifactId, setSelectedArtifactId]);

  // Handle artifact selection with automatic exit from edit/preview modes.
  // Updates the store and the ?artifact= param together so the URL always
  // reflects (and can restore) the current selection.
  const handleSelectArtifact = (artifactId: string | null) => {
    // If selecting a different artifact than currently selected
    if (artifactId !== selectedArtifactId) {
      // Exit edit mode if active
      if (isEditing) {
        setIsEditing(false);
        setEditingArtifact(undefined);
      }
      // Exit history/preview mode if active
      if (previewVersion) {
        setPreviewVersion(null);
      }
    }
    // Set the new selected artifact
    setSelectedArtifactId(artifactId);
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (artifactId) next.set('artifact', artifactId);
      else next.delete('artifact');
      return next;
    });
  };

  const handleUploadAttachment = async (file: File) => {
    if (!selectedArtifactId) {
      setError('No artifact selected');
      return;
    }

    setUploadingAttachmentId(selectedArtifactId);
    try {
      const response = await attachmentAPI.upload(selectedArtifactId, file);
      setAttachments([...attachments, response.data]);
      setError('');
    } catch (error: any) {
      console.error('Failed to upload attachment:', error);
      const errorMsg = apiErrorMessage(error, 'Unknown error');
      setError(`Failed to upload image: ${errorMsg}`);
    } finally {
      setUploadingAttachmentId(null);
    }
  };

  // Replacing a figure's image bumps the artifact's version server-side and
  // writes a note, so the artifact is reloaded rather than patched locally.
  const handleUploadAttachmentVersion = async (attachmentId: string, file: File) => {
    setUploadingAttachmentId(attachmentId);
    try {
      const response = await attachmentAPI.uploadVersion(attachmentId, file);
      setAttachments((prev) => prev.map((a) => (a.id === attachmentId ? response.data : a)));
      setError('');
      loadArtifacts();
    } catch (error: any) {
      console.error('Failed to upload figure version:', error);
      setError(`Failed to upload the new figure version: ${apiErrorMessage(error, 'Unknown error')}`);
    } finally {
      setUploadingAttachmentId(null);
    }
  };

  const handleDeleteAttachment = async (attachmentId: string) => {
    try {
      await attachmentAPI.delete(attachmentId);
      setAttachments(attachments.filter((a) => a.id !== attachmentId));
      setError('');
    } catch (error: any) {
      console.error('Failed to delete attachment:', error);
      const errorMsg = apiErrorMessage(error, 'Unknown error');
      setError(`Failed to delete image: ${errorMsg}`);
    }
  };

  const handleCreateArtifact = async (data: Partial<Artifact>) => {
    try {
      const response = await artifactAPI.create({
        project_id: projectId,
        ...data,
      });
      const created = response.data;
      addArtifact(created);

      // "Create before/after" means beside the artifact that was
      // right-clicked, not at the end of its level, so the new artifact is
      // moved into place once it has an id.
      if (pendingCreatePlacement) {
        const changed = await placeAmongSiblings(
          created,
          pendingCreatePlacement.anchorId,
          pendingCreatePlacement.position,
          artifacts
        );
        if (changed.length > 0) {
          const updated = new Map<string, Artifact>(changed.map((a) => [a.id, a]));
          setArtifacts([
            ...artifacts.map((a) => updated.get(a.id) || a),
            updated.get(created.id) || created,
          ]);
        }
      }

      setIsCreating(false);
      setPendingCreateContext(null);
      setPendingCreatePlacement(null);
      loadQuality();
      setError('');
    } catch (error: any) {
      console.error('Failed to create artifact:', error);
      setError(`Failed to create artifact: ${apiErrorMessage(error, 'Unknown error')}`);
    }
  };

  const handleUpdateArtifact = async (data: Partial<Artifact>) => {
    try {
      if (!editingArtifact) return;
      const response = await artifactAPI.update(editingArtifact.id, data);
      updateArtifact(response.data);
      setIsEditing(false);
      setEditingArtifact(undefined);
      // An update can carry pendingLinkAdds/pendingLinkRemoves; the backend
      // then auto-versions the counterpart artifacts (issue #169). Refetch
      // artifacts and links so no client-held version goes stale.
      await Promise.all([loadArtifacts(), loadLinks()]);
      // Content changed — refresh quality badges for the edited requirement.
      loadQuality();
      setError('');
    } catch (error: any) {
      console.error('Failed to update artifact:', error);
      const errorMsg = apiErrorMessage(error, 'Unknown error');
      setError(`Failed to update artifact: ${errorMsg}`);
    }
  };

  const handleDeleteArtifact = async (id: string) => {
    const ok = await confirm({
      title: 'Delete artifact',
      message: 'Are you sure you want to delete this artifact?',
      confirmLabel: 'Delete',
      danger: true,
    });
    if (!ok) {
      return;
    }
    try {
      await artifactAPI.delete(id);
      removeArtifact(id);
      if (selectedArtifactId === id) {
        handleSelectArtifact(null);
      }
      setError('');
    } catch (error: any) {
      console.error('Failed to delete artifact:', error);
      const errorMsg = apiErrorMessage(error, 'Unknown error');
      setError(`Failed to delete artifact: ${errorMsg}`);
    }
  };

  const handleEditArtifact = (artifact: Artifact) => {
    // Clear preview mode when entering edit
    if (previewVersion) {
      setPreviewVersion(null);
    }
    // Route through handleSelectArtifact so the ?artifact= param stays in
    // sync; the edit flags below win over the reset it performs on change.
    handleSelectArtifact(artifact.id);
    setEditingArtifact(artifact);
    setIsEditing(true);
    setError('');
  };

  const handleCreateLink = async (linkData: Partial<Link>) => {
    try {
      const response = await linkAPI.create(linkData);
      addLink(response.data);
      setAllLinks([...allLinks, response.data]);

      // The backend auto-versions BOTH linked artifacts (link snapshot
      // refresh), so refetch artifacts and the authoritative link list —
      // otherwise the client keeps stale versions (issue #169).
      await Promise.all([loadArtifacts(), loadLinks()]);

      setError('');
      await alertDialog({ title: 'Link created', message: 'Link created successfully.' });
    } catch (error: any) {
      console.error('Failed to create link:', error);
      const errorMsg = apiErrorMessage(error, 'Unknown error');
      setError(`Failed to create link: ${errorMsg}`);
    }
  };

  const handleBaselineChange = async (baselineId: string) => {
    setActiveBaselineId(baselineId);
    handleSelectArtifact(null);
    setIsEditing(false);
    setIsCreating(false);
    setEditingArtifact(undefined);
    setPreviewVersion(null);

    if (baselineId === 'live') {
      setBaselineData(null);
      return;
    }

    try {
      const response = await baselineAPI.get(baselineId);
      setBaselineData(response.data);
      setError('');
    } catch (error: any) {
      console.error('Failed to load baseline:', error);
      const errorMsg = apiErrorMessage(error, 'Unknown error');
      setError(`Failed to load baseline: ${errorMsg}`);
    }
  };

  const handleCaptureBaseline = async () => {
    if (!projectId) return;
    const name = await prompt({
      title: 'Capture baseline',
      label: 'Baseline name',
      placeholder: 'e.g. Design freeze — rev A',
    });
    if (name === null) return;
    if (!name.trim()) {
      setError('Baseline name is required');
      return;
    }

    try {
      await baselineAPI.create(projectId, name.trim());
      await loadBaselines();
      setError('');
    } catch (error: any) {
      console.error('Failed to capture baseline:', error);
      const errorMsg = apiErrorMessage(error, 'Unknown error');
      setError(`Failed to capture baseline: ${errorMsg}`);
    }
  };

  const handleDeleteBaseline = async (baselineId: string) => {
    if (baselineId === 'live') return;
    const ok = await confirm({
      title: 'Delete baseline',
      message: 'Delete this baseline? This cannot be undone.',
      confirmLabel: 'Delete',
      danger: true,
    });
    if (!ok) {
      return;
    }

    try {
      await baselineAPI.delete(baselineId);
      await loadBaselines();
      setActiveBaselineId('live');
      setBaselineData(null);
      setError('');
    } catch (error: any) {
      console.error('Failed to delete baseline:', error);
      const errorMsg = apiErrorMessage(error, 'Unknown error');
      setError(`Failed to delete baseline: ${errorMsg}`);
    }
  };

  /**
   * Save a sibling group in a given order, re-parenting `reparentId` on the way
   * when the move changed its parent.
   *
   * Renumbering the whole group is the same approach the ▲▼ buttons and paste
   * already use: sort orders are plain integers with no guaranteed gaps, so
   * rewriting 1..n is simpler than finding room between two of them.
   */
  const saveSiblingOrder = async (
    ordered: Artifact[],
    parentId: string | null,
    reparentId?: string
  ) => {
    const updates = ordered
      .map((artifact, index) => ({ artifact, newOrder: index + 1 }))
      .filter(
        ({ artifact, newOrder }) =>
          (artifact.sort_order ?? 0) !== newOrder || artifact.id === reparentId
      );
    if (updates.length === 0) return;

    try {
      const responses = await Promise.all(
        updates.map(({ artifact, newOrder }) =>
          artifactAPI.update(artifact.id, {
            // Only the moved artifact changes parent; its new siblings keep
            // theirs, which is the same value.
            parent_id: artifact.id === reparentId ? parentId : artifact.parent_id ?? null,
            type: artifact.type,
            title: artifact.title,
            body: artifact.body,
            attributes: artifact.attributes,
            sort_order: newOrder,
          })
        )
      );

      const updatedMap = new Map<string, Artifact>(
        responses.map((response) => [response.data.id, response.data])
      );
      setArtifacts(artifacts.map((item) => updatedMap.get(item.id) || item));
      setError('');
    } catch (error: any) {
      console.error('Failed to move artifacts:', error);
      setError(`Failed to move artifacts: ${apiErrorMessage(error, 'Unknown error')}`);
    }
  };

  const handleReorderArtifact = async (
    sourceId: string,
    targetId: string,
    mode: 'swap' | DropZone
  ) => {
    if (isBaselineView) return;

    // The ▲▼ buttons swap two siblings in place; they never re-parent.
    if (mode === 'swap') {
      const source = artifacts.find((item) => item.id === sourceId);
      const target = artifacts.find((item) => item.id === targetId);
      if (!source || !target) return;
      if (normalizeParentId(source.parent_id) !== normalizeParentId(target.parent_id)) return;

      const siblings = artifacts
        .filter((item) => normalizeParentId(item.parent_id) === normalizeParentId(source.parent_id))
        .sort(compareArtifacts);
      const sourceIndex = siblings.findIndex((item) => item.id === sourceId);
      const targetIndex = siblings.findIndex((item) => item.id === targetId);
      if (sourceIndex === -1 || targetIndex === -1) return;

      const reordered = [...siblings];
      reordered[sourceIndex] = siblings[targetIndex];
      reordered[targetIndex] = siblings[sourceIndex];
      await saveSiblingOrder(reordered, normalizeParentId(source.parent_id));
      return;
    }

    // A drag: the planner decides where it lands and refuses the moves that
    // must not happen (into its own subtree, or changing nothing).
    const plan = planMove(artifacts, sourceId, targetId, mode);
    if (!plan) return;
    await saveSiblingOrder(plan.ordered, plan.parentId, plan.reparents ? sourceId : undefined);
  };

  // Copying holds an artifact's CONTENT, not its identity: type, title, body
  // and attributes. Links and figures stay with the original — a pasted copy
  // that inherited "verifies REQ-12" would assert a verification nobody made.
  const [clipboard, setClipboard] = useState<Artifact | null>(null);

  /**
   * Move `created` to sit immediately before or after `anchorId` within its
   * sibling group, by renumbering that group 1..n.
   *
   * Renumbering the whole group is what drag-to-reorder already does: sort
   * orders are plain integers, so there is not always a gap to slot into, and
   * rewriting 1..n is both simpler and always correct. Returns the artifacts
   * that changed, so the caller can fold them into state in one go.
   */
  const placeAmongSiblings = async (
    created: Artifact,
    anchorId: string,
    position: 'before' | 'after',
    pool: Artifact[]
  ): Promise<Artifact[]> => {
    const siblings = pool
      .filter((a) => (a.parent_id ?? null) === (created.parent_id ?? null) && a.id !== created.id)
      .sort((a, b) => (a.sort_order ?? 0) - (b.sort_order ?? 0));
    const at = siblings.findIndex((a) => a.id === anchorId);
    if (at === -1) return [];

    const ordered = [...siblings];
    ordered.splice(position === 'before' ? at : at + 1, 0, created);
    const updates = ordered
      .map((a, index) => ({ artifact: a, newOrder: index + 1 }))
      .filter(({ artifact: a, newOrder }) => (a.sort_order ?? 0) !== newOrder);

    const responses = await Promise.all(
      updates.map(({ artifact: a, newOrder }) =>
        artifactAPI.update(a.id, {
          parent_id: a.parent_id ?? null,
          type: a.type,
          title: a.title,
          body: a.body,
          attributes: a.attributes,
          sort_order: newOrder,
        })
      )
    );
    return responses.map((r) => r.data);
  };

  /**
   * Create `source`'s content as a sibling of `target`, positioned immediately
   * before or after it.
   */
  const pasteRelativeTo = async (
    source: Pick<Artifact, 'type' | 'title' | 'body' | 'attributes'>,
    target: Artifact,
    position: 'before' | 'after'
  ) => {
    if (!projectId) return;
    try {
      // links_snapshot describes the ORIGINAL's links; carrying it over would
      // show a copy wearing traceability it does not have.
      const { links_snapshot, ...attributes } = (source.attributes || {}) as Record<string, any>;
      const response = await artifactAPI.create({
        project_id: projectId,
        parent_id: target.parent_id ?? null,
        type: source.type,
        title: `${source.title} (copy)`,
        body: source.body,
        attributes,
      });
      const created = response.data;

      const changed = await placeAmongSiblings(created, target.id, position, artifacts);
      const updated = new Map<string, Artifact>(changed.map((a) => [a.id, a]));
      setArtifacts([
        ...artifacts.map((a) => updated.get(a.id) || a),
        updated.get(created.id) || created,
      ]);
      setSelectedArtifactId(created.id);
      loadQuality();
      setError('');
    } catch (error: any) {
      console.error('Failed to paste artifact:', error);
      setError(`Failed to paste artifact: ${apiErrorMessage(error, 'Unknown error')}`);
    }
  };

  // A citation clicked in a description. A figure opens where it is — the
  // reader wanted to see the drawing, not navigate away from the sentence
  // citing it — and an artifact reference selects that artifact.
  const [figureInView, setFigureInView] = useState<Attachment | null>(null);

  const notesOpen = panelIsOpen(notesMode, notesHovered);
  const notesPinned = panelTakesSpace(notesMode);

  const cycleNotesMode = () => {
    const next = nextPanelMode(notesMode);
    setNotesMode(next);
    savePanelMode('artifact-notes', next);
    setNotesHovered(false);
  };

  const handleReferenceClick = (ref: string) => {
    if (isFigureRef(ref)) {
      const figure = attachments.find((a) => a.figure_ref === ref);
      if (figure) {
        setFigureInView(figure);
        return;
      }
    }
    const target = artifacts.find((a) => a.ref === ref);
    if (target) {
      setSelectedArtifactId(target.id);
      setIsEditing(false);
      setIsCreating(false);
      return;
    }
    setError(`${ref} is not in this project — it may have been deleted.`);
  };

  const handleArtifactContextMenu = (action: ArtifactContextAction, artifact: Artifact) => {
    if (action === 'copy') {
      setClipboard(artifact);
      return;
    }
    if (action === 'duplicate') {
      // A duplicate is a copy of this artifact placed directly after it.
      void pasteRelativeTo(artifact, artifact, 'after');
      return;
    }
    if (action === 'paste-before' || action === 'paste-after') {
      if (!clipboard) return;
      void pasteRelativeTo(clipboard, artifact, action === 'paste-before' ? 'before' : 'after');
      return;
    }

    // Auto-populate the create form based on the action. A sibling shares the
    // clicked artifact's parent; a child uses the clicked artifact as parent.
    // Both inherit its type. The fresh object identity re-applies the context
    // even when the create form is already open.
    setPendingCreateContext({
      parent_id: action === 'create-child' ? artifact.id : artifact.parent_id ?? null,
      type: artifact.type,
      title: '',
      body: '',
      attributes: {},
    });
    // A child goes to the end of its new parent's children, which is where a
    // first child belongs; a sibling goes beside the artifact clicked.
    setPendingCreatePlacement(
      action === 'create-child'
        ? null
        : { anchorId: artifact.id, position: action === 'create-before' ? 'before' : 'after' }
    );

    // Switch to create mode
    setIsEditing(false);
    setEditingArtifact(undefined);
    setIsCreating(true);
    setError('');
  };

  const activeArtifacts = isBaselineView ? (baselineData?.artifacts || []) : artifacts;
  const activeLinks = isBaselineView ? (baselineData?.links || []) : allLinks;

  const fieldOptions: Array<{ value: string; label: string }> = [
    { value: 'id', label: 'ID' },
    { value: 'project_id', label: 'Project ID' },
    { value: 'parent_id', label: 'Parent ID' },
    { value: 'type', label: 'Type' },
    { value: 'title', label: 'Title' },
    { value: 'body', label: 'Body' },
    { value: 'attributes', label: 'Attributes' },
    { value: 'version', label: 'Version' },
    { value: 'created_at', label: 'Created At' },
    { value: 'updated_at', label: 'Updated At' },
  ];

  // Fields with finite selection options
  const finiteFields = ['type'];

  // Get unique values for a field to populate selection dropdown
  const getFieldUniqueValues = (fieldName: string): string[] => {
    const values = new Set<string>();
    activeArtifacts.forEach((artifact) => {
      const val = getFieldValue(artifact, fieldName);
      if (val) values.add(val);
    });
    return Array.from(values).sort();
  };

  const getFieldValue = (artifact: Artifact, field: string): string => {
    switch (field) {
      case 'id':
        return artifact.id;
      case 'project_id':
        return artifact.project_id;
      case 'parent_id':
        return artifact.parent_id ?? '';
      case 'type':
        return artifact.type;
      case 'title':
        return artifact.title;
      case 'body':
        return artifact.body || '';
      case 'attributes':
        return JSON.stringify(artifact.attributes || {});
      case 'version':
        return String(artifact.version ?? '');
      case 'created_at':
        return artifact.created_at || '';
      case 'updated_at':
        return artifact.updated_at || '';
      default:
        return '';
    }
  };

  const matchesSearch = (artifact: Artifact, query: string): boolean => {
    const normalized = query.trim();
    if (!normalized) return true;

    const haystack = [
      artifact.title,
      artifact.body,
      artifact.type,
      artifact.id,
      artifact.parent_id ?? '',
      JSON.stringify(artifact.attributes || {}),
    ]
      .filter(Boolean)
      .join(' ')
      .toLowerCase();

    if (searchExact) {
      // "Exact match": the query must appear as a whole word/phrase, i.e. not
      // as a substring of a longer word ("log" no longer matches "catalog").
      const escaped = normalized.toLowerCase().replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
      return new RegExp(`(^|[^a-z0-9_])${escaped}($|[^a-z0-9_])`).test(haystack);
    }

    return haystack.includes(normalized.toLowerCase());
  };

  const applyComparator = (fieldValue: string, comparator: string, compareValue: string): boolean => {
    const normalizedField = fieldValue.toLowerCase();
    const normalizedCompare = compareValue.toLowerCase();

    if (comparator === 'equals') {
      return normalizedField === normalizedCompare;
    }

    if (comparator === 'not-equals') {
      return normalizedField !== normalizedCompare;
    }

    if (comparator === 'starts-with') {
      return normalizedField.startsWith(normalizedCompare);
    }

    if (comparator === 'ends-with') {
      return normalizedField.endsWith(normalizedCompare);
    }

    if (comparator === 'not-contains') {
      return !normalizedField.includes(normalizedCompare);
    }

    if (comparator === 'gt' || comparator === 'lt') {
      const fieldNumber = Number(fieldValue);
      const compareNumber = Number(compareValue);
      if (!Number.isNaN(fieldNumber) && !Number.isNaN(compareNumber)) {
        return comparator === 'gt' ? fieldNumber > compareNumber : fieldNumber < compareNumber;
      }

      const fieldDate = Date.parse(fieldValue);
      const compareDate = Date.parse(compareValue);
      if (!Number.isNaN(fieldDate) && !Number.isNaN(compareDate)) {
        return comparator === 'gt' ? fieldDate > compareDate : fieldDate < compareDate;
      }

      return comparator === 'gt'
        ? normalizedField > normalizedCompare
        : normalizedField < normalizedCompare;
    }

    return normalizedField.includes(normalizedCompare);
  };

  const matchesFieldFilters = (artifact: Artifact): boolean => {
    const activeRows = filterRows.filter((row) => row.value.trim() !== '');
    if (activeRows.length === 0) {
      return true;
    }

    const evaluations = activeRows.map((row) => {
      const value = row.value.trim().toLowerCase();
      const fieldValue = getFieldValue(artifact, row.field);
      return applyComparator(fieldValue, row.comparator, value);
    });

    return filterLogic === 'and'
      ? evaluations.every(Boolean)
      : evaluations.some(Boolean);
  };

  const filteredArtifacts = activeArtifacts.filter(
    (artifact) => matchesSearch(artifact, searchText) && matchesFieldFilters(artifact)
  );

  const buildFilterSummary = (): string => {
    const parts: string[] = [];

    if (searchText.trim()) {
      parts.push(searchExact ? `Exact search: "${searchText.trim()}"` : `Search: "${searchText.trim()}"`);
    }

    const activeRows = filterRows.filter((row) => row.value.trim() !== '');
    if (activeRows.length > 0) {
      const rowSummaries = activeRows.map((row) => `${row.field} ${row.comparator} "${row.value.trim()}"`);
      parts.push(`Filters (${filterLogic.toUpperCase()}): ${rowSummaries.join(filterLogic === 'and' ? ' + ' : ' | ')}`);
    }

    if (parts.length === 0) {
      return 'No filters applied';
    }

    return parts.join(' • ');
  };

  useEffect(() => {
    const saved = window.localStorage.getItem('artifactFilterPresets');
    if (!saved) return;
    try {
      const parsed = JSON.parse(saved) as Array<{ name: string; data: string }>;
      setFilterPresets(parsed);
    } catch (error) {
      console.warn('Failed to load filter presets', error);
    }
  }, []);

  const savePresets = (presets: Array<{ name: string; data: string }>) => {
    setFilterPresets(presets);
    window.localStorage.setItem('artifactFilterPresets', JSON.stringify(presets));
  };

  const applyPreset = (presetData: string) => {
    try {
      const parsed = JSON.parse(presetData) as {
        searchText: string;
        searchExact: boolean;
        filterLogic: 'and' | 'or';
        filterRows: Array<{ id: string; field: string; value: string; comparator: string }>;
      };

      setSearchText(parsed.searchText || '');
      setSearchExact(Boolean(parsed.searchExact));
      setFilterLogic(parsed.filterLogic || 'and');
      setFilterRows(
        parsed.filterRows && parsed.filterRows.length > 0
          ? parsed.filterRows
          : [{ id: `filter-${Date.now()}`, field: 'type', value: '', comparator: 'contains' }]
      );
    } catch (error) {
      console.warn('Failed to apply preset', error);
    }
  };

  if (!projectId) {
    return (
      <div className="card">
        <h3>No Project Selected</h3>
        <p>Please select a project to get started.</p>
      </div>
    );
  }

  const selectedArtifact = activeArtifacts.find((a) => a.id === selectedArtifactId);
  const detailAttachments = isBaselineView ? [] : attachments;

  // Requirements the "Draft test cases" action will cover: the selected
  // requirement if one is selected, otherwise every requirement in view. Only
  // the IDs are sent — the agent fetches each requirement's content itself.
  const requirementTargets =
    selectedArtifact && selectedArtifact.type === 'requirement'
      ? [selectedArtifact]
      : activeArtifacts.filter((a) => a.type === 'requirement');

  const handleDraftTestCases = async () => {
    if (!projectId || requirementTargets.length === 0 || draftingTests) return;
    setDraftingTests(true);
    try {
      await agentsAPI.draftTestCases(
        projectId,
        requirementTargets.map((a) => a.id)
      );
      setError('');
      navigate(`/projects/${projectId}/agent-runs`);
    } catch (error: any) {
      setError(`Failed to launch test-case drafting: ${apiErrorMessage(error, 'Unknown error')}`);
    } finally {
      setDraftingTests(false);
    }
  };

  return (
    // The module owns exactly the height it is given and no more: the toolbar
    // takes what it needs (two rows when the window is narrow) and the columns
    // below take the rest. Nothing here is measured in viewport units, so the
    // page never grows past the window and the browser never adds a scrollbar
    // around the whole app — every panel that needs to scroll scrolls itself.
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      {/* The floating help panel is mounted once in ProjectLayout now. */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '10px', padding: '16px 20px 12px', flexWrap: 'wrap', flexShrink: 0 }}>
        <h2 style={{ color: 'var(--text)', margin: 0 }}>Requirements</h2>
        <div style={{ flex: 1 }} />
        <select
          value={activeBaselineId}
          onChange={(e) => handleBaselineChange(e.target.value)}
          title="Select baseline"
          style={{
            height: '36px',
            padding: '0 10px',
            borderRadius: '4px',
            border: '1px solid var(--neutral-mid)',
            fontSize: '12px',
            backgroundColor: 'var(--surface)',
            cursor: 'pointer',
            minWidth: '180px',
          }}
        >
          <option value="live">Live Project</option>
          {baselines.map((baseline) => (
            <option key={baseline.id} value={baseline.id}>
              {baseline.name}
            </option>
          ))}
        </select>
        <button
          onClick={() => {
            // Compare the selected baseline (or the newest one when viewing
            // live) against the live project by default.
            const base = activeBaselineId !== 'live' ? activeBaselineId : baselines[0]?.id;
            if (base) navigate(`/projects/${projectId}/baselines/${base}/compare`);
          }}
          disabled={baselines.length === 0}
          style={{
            height: '36px',
            padding: '0 12px',
            backgroundColor: baselines.length === 0 ? 'var(--neutral-mid)' : 'var(--accent)',
            color: 'var(--accent-fg)',
            border: 'none',
            borderRadius: '4px',
            cursor: baselines.length === 0 ? 'not-allowed' : 'pointer',
            fontSize: '12px',
          }}
          title="Compare this baseline against another baseline or the live project"
        >
          Compare
        </button>
        <button
          onClick={() => handleDeleteBaseline(activeBaselineId)}
          disabled={activeBaselineId === 'live'}
          style={{
            height: '36px',
            padding: '0 10px',
            backgroundColor: activeBaselineId === 'live' ? 'var(--neutral-mid)' : 'var(--danger)',
            color: 'white',
            border: 'none',
            borderRadius: '4px',
            cursor: activeBaselineId === 'live' ? 'not-allowed' : 'pointer',
            fontSize: '12px',
          }}
          title="Delete selected baseline"
        >
          🗑
        </button>
        <button
          onClick={handleCaptureBaseline}
          style={{
            height: '36px',
            padding: '0 12px',
            backgroundColor: 'var(--success-bright)',
            color: 'white',
            border: 'none',
            borderRadius: '4px',
            cursor: 'pointer',
            fontSize: '12px',
          }}
        >
          Capture Baseline
        </button>
        <button
          onClick={handleDraftTestCases}
          disabled={isBaselineView || draftingTests || requirementTargets.length === 0}
          style={{
            height: '36px',
            padding: '0 12px',
            backgroundColor:
              isBaselineView || requirementTargets.length === 0 ? 'var(--neutral-mid)' : 'var(--success-bright)',
            color: 'white',
            border: 'none',
            borderRadius: '4px',
            cursor:
              isBaselineView || draftingTests || requirementTargets.length === 0 ? 'not-allowed' : 'pointer',
            fontSize: '12px',
          }}
          title={
            requirementTargets.length === 0
              ? 'No requirements to draft test cases for'
              : selectedArtifact && selectedArtifact.type === 'requirement'
                ? 'Draft test cases for the selected requirement (as proposals)'
                : `Draft test cases for all ${requirementTargets.length} requirements in view (as proposals)`
          }
        >
          {draftingTests ? 'Drafting…' : '🧪 Draft test cases'}
        </button>
        {/* One way out of a project: the wizard asks what shape and how much,
            and every format reads the same narrowed snapshot. */}
        <button
          onClick={() => setDownloadOpen(true)}
          style={{
            height: '36px',
            padding: '0 12px',
            backgroundColor: 'var(--accent-alt)',
            color: 'white',
            border: 'none',
            borderRadius: '4px',
            cursor: 'pointer',
            fontSize: '12px',
          }}
          title="Download this project — choose a format, sections and attachments"
        >
          ↓ Download
        </button>
      </div>
      <div style={{ display: 'flex', gap: '0', paddingLeft: '20px', paddingRight: '20px', paddingBottom: '10px', flex: 1, minHeight: 0, overflow: 'hidden' }}>
      {/* The column scrolls nothing itself: the artifact tree inside it owns
          the leftover height and scrolls there, so the tree grows with the
          window instead of sitting in a fixed-height box. */}
      <div style={{ width: `${leftColumnWidth}px`, minWidth: '200px', maxWidth: '800px', display: 'flex', flexDirection: 'column', overflowX: 'hidden', overflowY: 'hidden', minHeight: 0, paddingRight: '10px' }}>
        <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 15 }} />
        {!isBaselineView && (
          <button
            onClick={() => {
              setIsCreating(!isCreating);
              // A manual open (or cancel) always starts from a blank form, and
              // without the placement a context-menu create had asked for.
              setPendingCreateContext(null);
              setPendingCreatePlacement(null);
              setError('');
            }}
            className="button"
            style={{ width: '100%', marginBottom: '20px' }}
          >
            {isCreating ? 'Cancel' : '+ New Artifact'}
          </button>
        )}

        <div style={{ marginBottom: '20px' }}>
          <label style={{ display: 'block', fontSize: '12px', fontWeight: 'bold', marginBottom: '8px', color: 'var(--text)' }}>
            Filter and Search:
          </label>
          <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
            <div style={{ position: 'relative', flex: 1 }}>
              <input
                type="text"
                value={searchText}
                onChange={(e) => setSearchText(e.target.value)}
                placeholder="Search..."
                style={{
                  width: '100%',
                  padding: '8px 160px 8px 8px',
                  borderRadius: '4px',
                  border: '1px solid var(--neutral-mid)',
                  fontSize: '14px',
                  backgroundColor: 'var(--surface)',
                }}
              />
              <div
                style={{
                  position: 'absolute',
                  top: '50%',
                  right: '10px',
                  transform: 'translateY(-50%)',
                  fontSize: '11px',
                  color: 'var(--text-muted)',
                  maxWidth: '140px',
                  whiteSpace: 'nowrap',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  pointerEvents: 'none',
                }}
                title={buildFilterSummary()}
              >
                {buildFilterSummary()}
              </div>
            </div>
            <button
              onClick={() => setShowFilterPanel((prev) => !prev)}
              className="button-secondary"
              style={{ padding: '6px 10px', fontSize: '14px' }}
              title="Toggle filters"
            >
              ⚙
            </button>
          </div>
          {showFilterPanel && (
            <div style={{
              marginTop: '12px',
              border: '1px solid var(--border)',
              borderRadius: '6px',
              padding: '10px',
              backgroundColor: 'var(--surface-alt)',
              display: 'flex',
              flexDirection: 'column',
              gap: '10px',
            }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '12px', flexWrap: 'wrap' }}>
                <label style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '12px', color: 'var(--text)' }}>
                  <input
                    type="checkbox"
                    checked={searchExact}
                    onChange={(e) => setSearchExact(e.target.checked)}
                  />
                  Exact match
                </label>
                <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                  <span style={{ fontSize: '12px', color: 'var(--text)' }}>Filter logic</span>
                  <select
                    value={filterLogic}
                    onChange={(e) => setFilterLogic(e.target.value === 'or' ? 'or' : 'and')}
                    style={{
                      padding: '6px',
                      borderRadius: '4px',
                      border: '1px solid var(--neutral-mid)',
                      fontSize: '12px',
                    }}
                  >
                    <option value="and">AND</option>
                    <option value="or">OR</option>
                  </select>
                </div>
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                {filterRows.map((row) => {
                  const isFiniteField = finiteFields.includes(row.field);
                  const comparatorIsFinite = ['equals', 'not-equals'].includes(row.comparator);
                  const shouldUseSelect = isFiniteField && comparatorIsFinite;
                  const fieldValues = shouldUseSelect ? getFieldUniqueValues(row.field) : [];

                  return (
                    <div key={row.id} style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
                      <select
                        value={row.field}
                        onChange={(e) => {
                          const next = filterRows.map((item) =>
                            item.id === row.id ? { ...item, field: e.target.value } : item
                          );
                          setFilterRows(next);
                        }}
                        style={{
                          flex: '0 0 140px',
                          padding: '6px',
                          borderRadius: '4px',
                          border: '1px solid var(--neutral-mid)',
                          fontSize: '12px',
                        }}
                      >
                        {fieldOptions.map((option) => (
                          <option key={option.value} value={option.value}>
                            {option.label}
                          </option>
                        ))}
                      </select>
                      <select
                        value={row.comparator}
                        onChange={(e) => {
                          const next = filterRows.map((item) =>
                            item.id === row.id ? { ...item, comparator: e.target.value } : item
                          );
                          setFilterRows(next);
                        }}
                        style={{
                          flex: '0 0 140px',
                          padding: '6px',
                          borderRadius: '4px',
                          border: '1px solid var(--neutral-mid)',
                          fontSize: '12px',
                        }}
                      >
                        <option value="contains">Contains</option>
                        <option value="not-contains">Not contains</option>
                        <option value="equals">Equals</option>
                        <option value="not-equals">Not equals</option>
                        <option value="starts-with">Starts with</option>
                        <option value="ends-with">Ends with</option>
                        <option value="gt">Greater than</option>
                        <option value="lt">Less than</option>
                      </select>
                      {shouldUseSelect ? (
                        <select
                          value={row.value}
                          onChange={(e) => {
                            const next = filterRows.map((item) =>
                              item.id === row.id ? { ...item, value: e.target.value } : item
                            );
                            setFilterRows(next);
                          }}
                          style={{
                            flex: 1,
                            padding: '6px',
                            borderRadius: '4px',
                            border: '1px solid var(--neutral-mid)',
                            fontSize: '12px',
                          }}
                        >
                          <option value="">-- Select {row.field} --</option>
                          {fieldValues.map((val) => (
                            <option key={val} value={val}>
                              {val}
                            </option>
                          ))}
                        </select>
                      ) : (
                        <input
                          type="text"
                          value={row.value}
                          onChange={(e) => {
                            const next = filterRows.map((item) =>
                              item.id === row.id ? { ...item, value: e.target.value } : item
                            );
                            setFilterRows(next);
                          }}
                          placeholder="Contains..."
                          style={{
                            flex: 1,
                            padding: '6px',
                            borderRadius: '4px',
                            border: '1px solid var(--neutral-mid)',
                            fontSize: '12px',
                          }}
                        />
                      )}
                      <button
                        onClick={() => {
                          const next = filterRows.filter((item) => item.id !== row.id);
                          setFilterRows(
                            next.length > 0
                              ? next
                              : [{ id: `filter-${Date.now()}`, field: 'type', value: '', comparator: 'contains' }]
                          );
                        }}
                        className="button-secondary"
                        style={{ padding: '5px 8px', fontSize: '12px' }}
                        title="Remove filter"
                      >
                        Remove
                      </button>
                    </div>
                  );
                })}
                <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
                  <button
                    onClick={() => {
                      setFilterRows((prev) => [
                        ...prev,
                        { id: `filter-${Date.now()}`, field: 'type', value: '', comparator: 'contains' },
                      ]);
                    }}
                    className="button-secondary"
                    style={{ padding: '6px 10px', fontSize: '12px' }}
                  >
                    + Add filter
                  </button>
                  <button
                    onClick={() => {
                      const name = filterPresetName.trim();
                      if (!name) return;
                      const data = JSON.stringify({
                        searchText,
                        searchExact,
                        filterLogic,
                        filterRows,
                      });
                      const next = filterPresets.filter((preset) => preset.name !== name);
                      next.unshift({ name, data });
                      setFilterPresetName('');
                      setSelectedPreset(name);
                      savePresets(next);
                    }}
                    className="button-secondary"
                    style={{ padding: '6px 10px', fontSize: '12px' }}
                  >
                    Save preset
                  </button>
                  <button
                    onClick={() => {
                      setSearchText('');
                      setSearchExact(false);
                      setFilterLogic('and');
                      setFilterRows([{ id: `filter-${Date.now()}`, field: 'type', value: '', comparator: 'contains' }]);
                    }}
                    className="button-secondary"
                    style={{ padding: '6px 10px', fontSize: '12px' }}
                  >
                    Clear filters
                  </button>
                </div>
                <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap', alignItems: 'center' }}>
                  <input
                    type="text"
                    value={filterPresetName}
                    onChange={(e) => setFilterPresetName(e.target.value)}
                    placeholder="Preset name"
                    style={{
                      flex: '0 0 180px',
                      padding: '6px',
                      borderRadius: '4px',
                      border: '1px solid var(--neutral-mid)',
                      fontSize: '12px',
                    }}
                  />
                  <select
                    value={selectedPreset}
                    onChange={(e) => {
                      const value = e.target.value;
                      setSelectedPreset(value);
                      const preset = filterPresets.find((item) => item.name === value);
                      if (preset) {
                        applyPreset(preset.data);
                      }
                    }}
                    style={{
                      flex: '0 0 220px',
                      padding: '6px',
                      borderRadius: '4px',
                      border: '1px solid var(--neutral-mid)',
                      fontSize: '12px',
                    }}
                  >
                    <option value="">Saved presets</option>
                    {filterPresets.map((preset) => (
                      <option key={preset.name} value={preset.name}>
                        {preset.name}
                      </option>
                    ))}
                  </select>
                  <button
                    onClick={() => {
                      if (!selectedPreset) return;
                      const next = filterPresets.filter((preset) => preset.name !== selectedPreset);
                      setSelectedPreset('');
                      savePresets(next);
                    }}
                    className="button-secondary"
                    style={{ padding: '6px 10px', fontSize: '12px' }}
                  >
                    Delete preset
                  </button>
                </div>
              </div>
            </div>
          )}
          <div style={{ marginTop: '10px', display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
            <button
              onClick={() => {
                setCollapseAllToken((prev) => prev + 1);
              }}
              className="button-secondary"
              style={{ padding: '6px 10px', fontSize: '12px' }}
            >
              Collapse all
            </button>
            <button
              onClick={() => {
                setExpandAllToken((prev) => prev + 1);
              }}
              className="button-secondary"
              style={{ padding: '6px 10px', fontSize: '12px' }}
            >
              Expand all
            </button>
          </div>
        </div>

        {isCreating && !isBaselineView && (
          <ArtifactEditor
            artifacts={artifacts}
            projectId={projectId}
            initialData={pendingCreateContext ?? undefined}
            onSave={handleCreateArtifact}
            onCancel={() => {
              setIsCreating(false);
              setPendingCreateContext(null);
              setPendingCreatePlacement(null);
              setError('');
            }}
          />
        )}

        <ArtifactList
          artifacts={filteredArtifacts}
          allArtifacts={artifacts}
          selectedId={selectedArtifactId || undefined}
          onSelect={handleSelectArtifact}
          onReorder={handleReorderArtifact}
          onContextMenuAction={handleArtifactContextMenu}
          canPaste={!!clipboard}
          defaultCollapsed
          collapseAllTrigger={collapseAllToken}
          expandAllTrigger={expandAllToken}
          readOnly={isBaselineView}
          qualityScores={isBaselineView ? undefined : qualityScores}
        /></div>

      {/* Resize handle for left column */}
      <div
        onMouseDown={startResize('left')}
        style={{
          width: '10px',
          cursor: 'col-resize',
          backgroundColor: isResizing === 'left' ? 'var(--accent)' : 'transparent',
          borderLeft: '1px solid var(--border)',
          borderRight: '1px solid var(--border)',
          transition: 'background-color 0.2s',
          flexShrink: 0,
        }}
        onMouseEnter={(e) => {
          if (!isResizing) {
            e.currentTarget.style.backgroundColor = 'var(--neutral-soft)';
          }
        }}
        onMouseLeave={(e) => {
          if (!isResizing) {
            e.currentTarget.style.backgroundColor = 'transparent';
          }
        }}
      />

      <div style={{ display: 'flex', flex: 1, gap: '0', minWidth: 0, overflow: 'hidden' }}>
        <div style={{ flex: 1, minWidth: 0, overflow: 'auto', display: 'flex', flexDirection: 'column', paddingLeft: '10px', paddingRight: selectedArtifact ? '5px' : '10px' }}>
        {!isBaselineView && isEditing && editingArtifact && (
          <ArtifactEditor
            artifact={editingArtifact}
            artifacts={artifacts}
            projectId={projectId}
            onSave={handleUpdateArtifact}
            onCancel={() => {
              setIsEditing(false);
              setEditingArtifact(undefined);
            }}
            attachments={attachments}
            onUploadAttachment={handleUploadAttachment}
            onUploadAttachmentVersion={handleUploadAttachmentVersion}
            onDeleteAttachment={handleDeleteAttachment}
            isUploadLoading={uploadingAttachmentId === editingArtifact.id}
            links={allLinks}
            onCreateLink={handleCreateLink}
            onDeleteLink={(linkId) => {
              // Link deletion is now handled in edit mode
              // The backend will process actual link deletion
            }}
          />
        )}

        {selectedArtifact && !isEditing && (
          <>
            <ArtifactHeader
              artifact={selectedArtifact}
              onEdit={handleEditArtifact}
              onDelete={handleDeleteArtifact}
              onRestore={(restored) => {
                updateArtifact(restored);
                handleSelectArtifact(restored.id);
              }}
              previewVersion={previewVersion}
              onPreviewChange={setPreviewVersion}
            />
            <ArtifactDetails 
              artifact={selectedArtifact} 
              links={activeLinks} 
              artifacts={activeArtifacts}
              attachments={detailAttachments}
              onSelectArtifact={handleSelectArtifact}
              onReferenceClick={handleReferenceClick}
              previewVersion={previewVersion}
              onClosePreview={() => setPreviewVersion(null)}
              allowLinkDelete={!isBaselineView}
              liveLinks={!isBaselineView}
              onLinksChanged={() => {
                // Link writes auto-version both artifacts server-side;
                // refetch so displayed versions and link lists stay current.
                loadArtifacts();
                loadLinks();
              }}
            />
          </>
        )}

        {!selectedArtifact && !isEditing && (
          <div className="card">
            <h3>No Artifact Selected</h3>
            <p>Select an artifact from the list to view details.</p>
          </div>
        )}
        </div>

        {/* The notes column stays whether or not an artifact is selected: its
            comments tab needs one, its assistant tab does not. Its width is
            only spent when pinned — auto-hide floats it over the document on
            hover, and hidden leaves just the strip that brings it back. */}
        {!notesPinned && (
          <div
            onMouseEnter={() => setNotesHovered(true)}
            onMouseLeave={() => setNotesHovered(false)}
            onClick={() => setNotesHovered(true)}
            title={`Notes: ${panelModeLabel(notesMode)} — click the button inside to change`}
            style={{
              width: 10,
              minWidth: 10,
              borderLeft: '1px solid var(--border)',
              background: 'var(--surface-alt)',
              cursor: 'pointer',
              display: 'flex',
              justifyContent: 'center',
              paddingTop: 12,
              fontSize: 10,
              color: 'var(--text-muted)',
            }}
          >
            ‹
          </div>
        )}
            {/* Resize handle — only a pinned column has a width to drag. */}
            {notesPinned && (
            <div
              onMouseDown={startResize('right')}
              style={{
                width: '10px',
                cursor: 'col-resize',
                backgroundColor: isResizing === 'right' ? 'var(--accent)' : 'transparent',
                borderLeft: '1px solid var(--border)',
                borderRight: '1px solid var(--border)',
                transition: 'background-color 0.2s',
                flexShrink: 0,
              }}
              onMouseEnter={(e) => {
                if (!isResizing) {
                  e.currentTarget.style.backgroundColor = 'var(--neutral-soft)';
                }
              }}
              onMouseLeave={(e) => {
                if (!isResizing) {
                  e.currentTarget.style.backgroundColor = 'transparent';
                }
              }}
            />
            )}
            {notesOpen && (
            <div
              onMouseEnter={() => notesMode === 'autohide' && setNotesHovered(true)}
              onMouseLeave={() => notesMode === 'autohide' && setNotesHovered(false)}
              style={{
                width: `${rightColumnWidth}px`,
                minWidth: '250px',
                maxWidth: '600px',
                overflow: 'hidden',
                // Unpinned, the panel floats over the document rather than
                // reflowing it whenever the pointer crosses the edge.
                ...(notesPinned
                  ? {}
                  : {
                      position: 'fixed',
                      right: 10,
                      top: 0,
                      bottom: 0,
                      zIndex: 900,
                      background: 'var(--surface)',
                      boxShadow: '-2px 0 8px rgba(0,0,0,0.2)',
                    }),
              }}
            >
              <ChatterPanel
                key={selectedArtifact ? `chatter-${selectedArtifact.id}-v${selectedArtifact.version}` : 'chatter-none'}
                artifactId={selectedArtifact?.id}
                projectId={projectId || undefined}
                isOpen={true}
                onToggle={cycleNotesMode}
                modeLabel={panelModeLabel(notesMode)}
                nextModeLabel={panelModeLabel(nextPanelMode(notesMode))}
              />
            </div>
            )}
      </div>
      </div>

      {downloadOpen && projectId && (
        <DownloadWizard
          projectId={projectId}
          baselineId={activeBaselineId}
          onClose={() => setDownloadOpen(false)}
        />
      )}

      {/* A figure opened by clicking its citation in a description. */}
      {figureInView && (
        <ImageLightbox
          imageUrl={attachmentAPI.getDownloadUrl(figureInView.id, figureInView.version)}
          filename={`${figureInView.figure_ref || figureInView.filename} (v${figureInView.version})`}
          onClose={() => setFigureInView(null)}
        />
      )}
    </div>
  );
};

export default ModuleView;
