import React, { useCallback, useState, useEffect } from 'react';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { useAppStore } from '../state/store';
import { artifactAPI, linkAPI, attachmentAPI, baselineAPI, qualityAPI, agentsAPI, Artifact, Link, Attachment, Baseline, ProjectExport, LinkedArtifact, Project, projectAPI } from '../api/client';
import type { ArtifactContextAction, QualityRowInfo } from '../components/ArtifactList';
import { DropZone, planMove } from '../utils/artifactDrag';
import { matchesFieldFilters, matchesSearch } from '../utils/artifactFilter';
import { AttachmentViewer } from '../components/AttachmentViewer';
import { artifactRefOfFigure, isFigureRef } from '../components/artifactReferences';
import {
  PanelMode,
  loadPanelMode,
  nextPanelMode,
  panelIsOpen,
  panelModeLabel,
  panelStripLabel,
  panelStripWidth,
  panelTakesSpace,
  revealAfterModeChange,
  savePanelMode,
} from '../components/panelMode';
import { ArtifactEditor } from '../components/ArtifactEditor';
import { ArtifactHeader } from '../components/ArtifactHeader';
import { ArtifactDetails } from '../components/ArtifactDetails';
import { ChatterPanel } from '../components/ChatterPanel';
import { DownloadWizard } from '../components/DownloadWizard';
import { useAlert, useConfirm, usePrompt } from '../components/ui';
import { apiErrorMessage } from '../api/errors';
import { useViewport } from '../hooks/useViewport';
import { useFeature } from '../hooks/useFeature';
import { ARTIFACT_STEPPING_FEATURE } from '../components/ArtifactStepper';
import {
  compareArtifacts,
  documentOrder,
  normalizeParentId,
  sequencePosition,
  stepArtifact,
} from '../utils/artifactSequence';
import { overlayIsOpen, readingStepFor } from '../hooks/readingKeys';
import { useHorizontalSwipe } from '../hooks/useSwipe';
import { Toolbar } from './moduleView/Toolbar';
import { DocumentHeader } from './moduleView/DocumentHeader';
import { PhoneActionsSheet } from './moduleView/PhoneActionsSheet';
import { FilterPanel } from './moduleView/FilterPanel';
import { TreePane } from './moduleView/TreePane';

export const ModuleView: React.FC = () => {
  const confirm = useConfirm();
  const prompt = usePrompt();
  const alertDialog = useAlert();
  const [isCreating, setIsCreating] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const [editingArtifact, setEditingArtifact] = useState<Artifact | undefined>();
  const [error, setError] = useState<string>('');
  const [allLinks, setAllLinks] = useState<Link[]>([]);
  // Flow-down (REQ-145): the far end of every link crossing into another
  // project, and the parent project's requirements a requirement here may
  // refine.
  const [linkedArtifacts, setLinkedArtifacts] = useState<LinkedArtifact[]>([]);
  const ownersOn = useFeature('artifact-owners');
  const steppingOn = useFeature(ARTIFACT_STEPPING_FEATURE);
  const [parentProject, setParentProject] = useState<Project | null>(null);
  const [parentArtifacts, setParentArtifacts] = useState<Artifact[]>([]);
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
  // Every figure in the project, as opposed to the selected artifact's. It is
  // what lets a "##" citation be offered while writing and opened while
  // reading, neither of which can wait to find out which artifact holds the
  // figure in question.
  const [projectAttachments, setProjectAttachments] = useState<Attachment[]>([]);
  const [uploadingAttachmentId, setUploadingAttachmentId] = useState<string | null>(null);
  // How far the figure currently uploading has got. Null both when nothing is
  // uploading and when the browser cannot measure the body.
  const [uploadPercent, setUploadPercent] = useState<number | null>(null);
  const [baselines, setBaselines] = useState<Baseline[]>([]);
  const [activeBaselineId, setActiveBaselineId] = useState<string>('live');
  const [baselineData, setBaselineData] = useState<ProjectExport | null>(null);
  // A baseline snapshot is a whole project export — close to a megabyte of
  // JSON for a real project — so on a phone it can take seconds to arrive.
  // Without this the tree renders empty the moment a baseline is selected and
  // only fills in when the fetch lands, which looks exactly like a project
  // that lost all its requirements.
  const [baselineLoading, setBaselineLoading] = useState(false);
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
  // A first visit on a small laptop (under 1200px) starts the notes column
  // auto-hidden: pinned, it would leave the document a sliver between the
  // tree and the notes. A saved choice always wins.
  const [notesMode, setNotesMode] = useState<PanelMode>(() =>
    loadPanelMode(
      'artifact-notes',
      typeof window !== 'undefined' && window.innerWidth < 1200 ? 'autohide' : 'pinned'
    )
  );
  const [notesHovered, setNotesHovered] = useState(false);
  // An explicit "show it to me" from the edge strip. Hover alone never opens a
  // hidden notes panel, so this is the only way back from that mode, and it
  // stays open until dismissed rather than vanishing when the pointer moves.
  const [notesRevealed, setNotesRevealed] = useState(false);
  // Phones and tablets cannot fit three columns: the module stacks and shows
  // one pane at a time — the tree, the selected artifact's document, or the
  // notes — chosen with a segmented control under the toolbar. Selecting an
  // artifact in the tree moves to its document, which is what the tap meant.
  const viewport = useViewport();
  const stacked = viewport.isCompact;
  const [stackedPane, setStackedPane] = useState<'tree' | 'document' | 'notes'>('tree');
  // Stacked, the toolbar's baseline picker and five buttons fold into one
  // actions sheet behind a ⋯ button: on a phone they took two full rows
  // above the content.
  const [toolsOpen, setToolsOpen] = useState(false);
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
  // The document pane. A step has to start the next artifact at its top: the
  // pane keeps its scroll position across a selection change, so otherwise the
  // next requirement opens at whatever offset the last one was left at.
  const documentPaneRef = React.useRef<HTMLDivElement | null>(null);
  // The tree column as drawn: the saved width, clamped so the document keeps
  // at least 420px beside the project sidebar and a pinned notes column. The
  // saved value is untouched — a wider window gets it back.
  const notesTakesSpace = panelTakesSpace(notesMode);
  const leftColumnDrawn = Math.max(
    200,
    Math.min(leftColumnWidth, viewport.width - 260 - (notesTakesSpace ? rightColumnWidth + 10 : 10) - 420)
  );
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

  // Start a column resize: record where the drag began so the move handler can
  // work in deltas. preventDefault keeps the browser from starting a text
  // selection or native drag under the cursor.
  const startResize = (side: 'left' | 'right') => (e: React.MouseEvent) => {
    e.preventDefault();
    resizeOrigin.current = {
      side,
      startX: e.clientX,
      startWidth: side === 'left' ? leftColumnDrawn : rightColumnWidth,
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
    // Best-effort: without it a cross-project link shows a bare id.
    try {
      const linked = await projectAPI.linkedArtifacts(projectId);
      setLinkedArtifacts(linked.data || []);
    } catch {
      setLinkedArtifacts([]);
    }
  }, [projectId]);

  // The parent project and its requirements, for the refines picker.
  useEffect(() => {
    let cancelled = false;
    setParentProject(null);
    setParentArtifacts([]);
    if (!projectId) return;
    (async () => {
      try {
        const me = await projectAPI.get(projectId);
        const parentId = me.data?.parent_project_id;
        if (!parentId || cancelled) return;
        const [parent, reqs] = await Promise.all([
          projectAPI.get(parentId),
          artifactAPI.list(parentId, 'requirement'),
        ]);
        if (cancelled) return;
        setParentProject(parent.data);
        setParentArtifacts(reqs.data || []);
      } catch {
        // A member of the child project may not read the parent; the picker
        // then offers local targets only.
      }
    })();
    return () => {
      cancelled = true;
    };
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

  const loadProjectAttachments = useCallback(async () => {
    if (!projectId) {
      setProjectAttachments([]);
      return;
    }
    try {
      const response = await attachmentAPI.listByProject(projectId);
      setProjectAttachments(response.data || []);
    } catch (error: any) {
      // A citation that cannot be resolved falls back to the selected
      // artifact's own figures and then to an explanatory message, so this is
      // a degraded menu rather than a broken screen.
      console.error('Failed to load the project figures:', error);
      setProjectAttachments([]);
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
      loadProjectAttachments();
      setActiveBaselineId('live');
      setBaselineData(null);
    }
  }, [projectId, loadArtifacts, loadLinks, loadBaselines, loadQuality, loadProjectAttachments]);

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

  // Select an artifact (or none) by writing the ?artifact= param; the sync
  // effect above moves the store selection to it, so the URL always reflects
  // (and can restore) the current selection. Every path that changes the
  // selection goes through here. A store write alone is undone by the sync
  // effect, which puts the URL's selection back (#379, bug 134); writing
  // the store as well loads the document twice, because the router updates
  // the URL in a transition, after the store, and in between the effect
  // sees the URL without the new selection and clears it (#379, bug 102).
  const selectInUrl = (artifactId: string | null) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (artifactId) next.set('artifact', artifactId);
      else next.delete('artifact');
      return next;
    });
  };

  // Handle artifact selection with automatic exit from edit/preview modes.
  const handleSelectArtifact = (artifactId: string | null) => {
    if (artifactId && stacked) {
      setStackedPane('document');
    }
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
    selectInUrl(artifactId);
  };

  // Adding a figure takes the artifact to a new version server-side, as a new
  // image and a rename do, so the artifact is reloaded too.
  const handleUploadAttachment = async (file: File) => {
    if (!selectedArtifactId) {
      setError('No artifact selected');
      return;
    }

    setUploadingAttachmentId(selectedArtifactId);
    setUploadPercent(0);
    try {
      const response = await attachmentAPI.upload(selectedArtifactId, file, setUploadPercent);
      setAttachments([...attachments, response.data]);
      // The project's figure list is what "##" offers and what a citation
      // resolves against, so a new figure has to reach it too.
      setProjectAttachments((prev) => [...prev, response.data]);
      setError('');
      loadArtifacts();
    } catch (error: any) {
      console.error('Failed to attach the file:', error);
      const errorMsg = apiErrorMessage(error, 'Unknown error');
      setError(`Failed to attach the file: ${errorMsg}`);
    } finally {
      setUploadingAttachmentId(null);
      setUploadPercent(null);
    }
  };

  // Replacing a figure's image bumps the artifact's version server-side and
  // writes a note, so the artifact is reloaded rather than patched locally.
  const handleUploadAttachmentVersion = async (attachmentId: string, file: File) => {
    setUploadingAttachmentId(attachmentId);
    setUploadPercent(0);
    try {
      const response = await attachmentAPI.uploadVersion(attachmentId, file, setUploadPercent);
      setAttachments((prev) => prev.map((a) => (a.id === attachmentId ? response.data : a)));
      setProjectAttachments((prev) =>
        prev.map((a) => (a.id === attachmentId ? response.data : a))
      );
      setError('');
      loadArtifacts();
    } catch (error: any) {
      console.error('Failed to upload figure version:', error);
      setError(`Failed to upload the new figure version: ${apiErrorMessage(error, 'Unknown error')}`);
    } finally {
      setUploadingAttachmentId(null);
      setUploadPercent(null);
    }
  };

  // Renaming a figure is a figure version and an artifact version, like a
  // new image, so the artifact is reloaded the same way.
  const handleRenameAttachment = async (attachmentId: string, title: string) => {
    try {
      const response = await attachmentAPI.rename(attachmentId, title);
      setAttachments((prev) => prev.map((a) => (a.id === attachmentId ? response.data : a)));
      setError('');
      loadArtifacts();
    } catch (error: any) {
      console.error('Failed to rename figure:', error);
      setError(`Failed to rename the figure: ${apiErrorMessage(error, 'Unknown error')}`);
    }
  };

  // A restore rewrites the figure's current image and title, so the editor's
  // copy has to be refetched — the gallery only knows what it did, not what
  // the artifact's other readers now show.
  const handleAttachmentRestored = () => {
    // Same artifact the figures were loaded for — the editor's when one is
    // open, else the selected row.
    const artifactId = editingArtifact?.id || selectedArtifactId;
    if (artifactId) loadAttachments(artifactId);
    loadArtifacts();
  };

  const handleDeleteAttachment = async (attachmentId: string) => {
    try {
      await attachmentAPI.delete(attachmentId);
      setAttachments(attachments.filter((a) => a.id !== attachmentId));
      setProjectAttachments((prev) => prev.filter((a) => a.id !== attachmentId));
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
      setBaselineLoading(false);
      return;
    }

    setBaselineData(null);
    setBaselineLoading(true);
    try {
      const response = await baselineAPI.get(baselineId);
      setBaselineData(response.data);
      setError('');
    } catch (error: any) {
      console.error('Failed to load baseline:', error);
      const errorMsg = apiErrorMessage(error, 'Unknown error');
      setError(`Failed to load baseline: ${errorMsg}`);
      // Fall back to the live project rather than leaving the reader on a
      // baseline view with nothing in it, which they would read as a baseline
      // that captured nothing.
      setActiveBaselineId('live');
    } finally {
      setBaselineLoading(false);
    }
  };

  const handleCaptureBaseline = async () => {
    if (!projectId) {
      // A click that does nothing and says nothing is indistinguishable from
      // a broken button.
      setError('No project is open, so there is nothing to capture.');
      return;
    }
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
    source: Pick<Artifact, 'id' | 'type' | 'title' | 'body' | 'attributes'>,
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
        // The copy starts with no history of its own; the server writes the
        // one note it should have, naming where it came from.
        copied_from: source.id,
      });
      const created = response.data;

      const changed = await placeAmongSiblings(created, target.id, position, artifacts);
      const updated = new Map<string, Artifact>(changed.map((a) => [a.id, a]));
      setArtifacts([
        ...artifacts.map((a) => updated.get(a.id) || a),
        updated.get(created.id) || created,
      ]);
      selectInUrl(created.id);
      loadQuality();
      setError('');
    } catch (error: any) {
      console.error('Failed to paste artifact:', error);
      setError(`Failed to paste artifact: ${apiErrorMessage(error, 'Unknown error')}`);
    }
  };

  // A citation clicked in a description. A figure opens where it is — the
  // reader wanted to see the drawing, not navigate away from the sentence
  // citing it, and least of all to be sent to the artifact the drawing happens
  // to hang on — and an artifact reference selects that artifact.
  const [figureInView, setFigureInView] = useState<Attachment | null>(null);

  const notesOpen = panelIsOpen(notesMode, notesHovered, notesRevealed);
  const notesPinned = panelTakesSpace(notesMode);

  const cycleNotesMode = () => {
    const next = nextPanelMode(notesMode);
    setNotesMode(next);
    savePanelMode('artifact-notes', next);
    setNotesHovered(false);
    // Keep the panel on screen while its mode is being chosen, so the button
    // doing the choosing does not disappear mid-cycle (issue #362).
    setNotesRevealed(revealAfterModeChange(next));
  };

  // Escape dismisses a revealed notes panel, the same key that closes every
  // other thing this view floats over the document.
  useEffect(() => {
    if (!notesRevealed) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setNotesRevealed(false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [notesRevealed]);

  const handleReferenceClick = (ref: string) => {
    if (isFigureRef(ref)) {
      // Whichever artifact holds it: a reader who clicked a figure wants the
      // figure. Falling through to the artifact would open the requirement
      // the figure hangs on, which for a citation of one's own figure is the
      // page already on screen.
      const figure =
        projectAttachments.find((a) => a.figure_ref === ref) ||
        attachments.find((a) => a.figure_ref === ref);
      if (figure) {
        setFigureInView(figure);
        return;
      }
      // The figure is gone, or the project's figures never loaded. Going to
      // the artifact that would hold it beats a dead end.
      const holder = artifacts.find((a) => a.ref === artifactRefOfFigure(ref));
      if (holder) {
        selectInUrl(holder.id);
        setIsEditing(false);
        setIsCreating(false);
        setError(`${ref} is no longer on ${holder.ref}. Showing the artifact instead.`);
        return;
      }
      setError(`${ref} is not in this project — it may have been deleted.`);
      return;
    }
    const target = artifacts.find((a) => a.ref === ref);
    if (target) {
      selectInUrl(target.id);
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
    ...(ownersOn ? [{ value: 'owner', label: 'Owner' }] : []),
    { value: 'title', label: 'Title' },
    { value: 'body', label: 'Body' },
    { value: 'attributes', label: 'Attributes' },
    { value: 'version', label: 'Version' },
    { value: 'created_at', label: 'Created At' },
    { value: 'updated_at', label: 'Updated At' },
  ];

  // Fields with finite selection options
  const finiteFields = ['type', 'owner'];

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
      case 'owner':
        return typeof artifact.attributes?.owner === 'string' ? artifact.attributes.owner : '';
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

  const filteredArtifacts = activeArtifacts.filter(
    (artifact) => matchesSearch(artifact, searchText, searchExact) && matchesFieldFilters(artifact, filterRows, filterLogic, getFieldValue)
  );

  // Reading order for ‹ ›, J / K and the swipe: the tree's own order, over what
  // the search and the filters have left in view, so a narrowed tree reads as
  // its own short document and a step never lands on a row the tree is not
  // drawing.
  //
  // Collapsed rows are still in it. A closed parent hides its children on
  // screen; it does not take them out of the document, and the tree opens
  // itself onto wherever a step lands.
  //
  // Deliberately not memoized: filteredArtifacts is a fresh array every render,
  // so a memo keyed on it would never hit and would only add a dependency to
  // get wrong.
  const readingOrder = documentOrder(filteredArtifacts);
  const readingPlace = sequencePosition(readingOrder, selectedArtifactId);

  const stepToArtifact = (delta: 1 | -1) => {
    const target = stepArtifact(readingOrder, selectedArtifactId, delta);
    if (!target) return;
    handleSelectArtifact(target.id);
    // Optional call: scrollTo is missing on elements in jsdom and in older
    // browsers, and failing to scroll must not cost the step.
    documentPaneRef.current?.scrollTo?.({ top: 0 });
  };

  // What the key listener needs to know changes every render — readingOrder is
  // a new array each time — so it reads the current answer from a ref instead
  // of being torn down and re-added. Written in an effect rather than during
  // render so a discarded render cannot leave a stale step behind.
  const readingRef = React.useRef<{ enabled: boolean; step: (delta: 1 | -1) => void }>({
    enabled: false,
    step: () => {},
  });
  useEffect(() => {
    readingRef.current = {
      enabled: steppingOn && readingOrder.length > 1 && !isEditing && !isCreating,
      step: stepToArtifact,
    };
  });

  // J is next, K is previous: the keys a reader of anything paged already has
  // in their fingers, and bare, because stepping through a review happens a
  // hundred times in a sitting and a modifier turns that into work. Every way
  // they could have been meant as letters is excluded — see readingStepFor —
  // and anything that handled the key first and said so keeps it.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.defaultPrevented) return;
      const step = readingStepFor({
        key: event.key,
        target: event.target,
        altKey: event.altKey,
        ctrlKey: event.ctrlKey,
        metaKey: event.metaKey,
        shiftKey: event.shiftKey,
        busy: !readingRef.current.enabled || overlayIsOpen(),
      });
      if (!step) return;
      event.preventDefault();
      readingRef.current.step(step === 'next' ? 1 : -1);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  // A phone has one pane and no keyboard, so the gesture is the affordance.
  // Pane switching stays on the Tree / Document / Notes control: a swipe here
  // turns the page, it does not change which pane is on it.
  const documentSwipe = useHorizontalSwipe(
    steppingOn && viewport.coarsePointer && readingOrder.length > 1 && !isEditing,
    (direction) => stepToArtifact(direction === 'next' ? 1 : -1)
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

  const toolbarActions = (
    <Toolbar
      activeBaselineId={activeBaselineId}
      handleBaselineChange={handleBaselineChange}
      baselines={baselines}
      navigate={navigate}
      projectId={projectId}
      handleDeleteBaseline={handleDeleteBaseline}
      stacked={stacked}
      handleCaptureBaseline={handleCaptureBaseline}
      handleDraftTestCases={handleDraftTestCases}
      isBaselineView={isBaselineView}
      draftingTests={draftingTests}
      requirementTargets={requirementTargets}
      selectedArtifact={selectedArtifact}
      setDownloadOpen={setDownloadOpen}
    />
  );

  return (
    // The module owns exactly the height it is given and no more: the toolbar
    // takes what it needs (two rows when the window is narrow) and the columns
    // below take the rest. Nothing here is measured in viewport units, so the
    // page never grows past the window and the browser never adds a scrollbar
    // around the whole app — every panel that needs to scroll scrolls itself.
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      {/* The floating help panel is mounted once in ProjectLayout now. */}
      <DocumentHeader stacked={stacked} toolsOpen={toolsOpen} setToolsOpen={setToolsOpen} toolbarActions={toolbarActions} />
      {stacked && toolsOpen && (
        <PhoneActionsSheet setToolsOpen={setToolsOpen} toolbarActions={toolbarActions} />
      )}
      {stacked && (
        <div
          role="tablist"
          aria-label="Requirements panes"
          style={{ display: 'flex', margin: '0 12px 8px', border: '1px solid var(--border)', borderRadius: 6, overflow: 'hidden', flexShrink: 0 }}
        >
          {(['tree', 'document', 'notes'] as const).map((pane) => {
            const active = stackedPane === pane;
            const label = pane === 'tree' ? 'Tree' : pane === 'document' ? 'Document' : 'Notes';
            return (
              <button
                key={pane}
                type="button"
                role="tab"
                aria-selected={active}
                onClick={() => setStackedPane(pane)}
                style={{
                  flex: 1,
                  minHeight: 44,
                  border: 'none',
                  borderLeft: pane === 'tree' ? 'none' : '1px solid var(--border)',
                  background: active ? 'var(--accent)' : 'var(--surface)',
                  color: active ? 'var(--accent-fg)' : 'var(--text)',
                  fontSize: 14,
                  fontWeight: active ? 600 : 400,
                  cursor: 'pointer',
                }}
              >
                {label}
              </button>
            );
          })}
        </div>
      )}
      <div style={{ display: 'flex', gap: '0', paddingLeft: stacked ? '12px' : '20px', paddingRight: stacked ? '12px' : '20px', paddingBottom: '10px', flex: 1, minHeight: 0, overflow: 'hidden' }}>
      {/* The column scrolls nothing itself: the artifact tree inside it owns
          the leftover height and scrolls there, so the tree grows with the
          window instead of sitting in a fixed-height box. Stacked, every
          pane keeps its state (tree expansion, editor drafts) by being hidden
          rather than unmounted. */}
      <TreePane
        stacked={stacked}
        stackedPane={stackedPane}
        leftColumnDrawn={leftColumnDrawn}
        error={error}
        setError={setError}
        isBaselineView={isBaselineView}
        isCreating={isCreating}
        setIsCreating={setIsCreating}
        setPendingCreateContext={setPendingCreateContext}
        setPendingCreatePlacement={setPendingCreatePlacement}
        searchText={searchText}
        setSearchText={setSearchText}
        buildFilterSummary={buildFilterSummary}
        setShowFilterPanel={setShowFilterPanel}
        showFilterPanel={showFilterPanel}
        filterPanel={
          <FilterPanel
            searchText={searchText}
            setSearchText={setSearchText}
            searchExact={searchExact}
            setSearchExact={setSearchExact}
            filterLogic={filterLogic}
            setFilterLogic={setFilterLogic}
            filterRows={filterRows}
            setFilterRows={setFilterRows}
            finiteFields={finiteFields}
            getFieldUniqueValues={getFieldUniqueValues}
            fieldOptions={fieldOptions}
            filterPresetName={filterPresetName}
            setFilterPresetName={setFilterPresetName}
            filterPresets={filterPresets}
            selectedPreset={selectedPreset}
            setSelectedPreset={setSelectedPreset}
            savePresets={savePresets}
            applyPreset={applyPreset}
          />
        }
        setCollapseAllToken={setCollapseAllToken}
        setExpandAllToken={setExpandAllToken}
        artifacts={artifacts}
        projectId={projectId}
        pendingCreateContext={pendingCreateContext}
        handleCreateArtifact={handleCreateArtifact}
        baselineLoading={baselineLoading}
        filteredArtifacts={filteredArtifacts}
        selectedArtifactId={selectedArtifactId}
        handleSelectArtifact={handleSelectArtifact}
        handleReorderArtifact={handleReorderArtifact}
        handleArtifactContextMenu={handleArtifactContextMenu}
        clipboard={clipboard}
        collapseAllToken={collapseAllToken}
        expandAllToken={expandAllToken}
        qualityScores={qualityScores}
      />

      {/* Resize handle for left column */}
      {!stacked && (
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
      )}

      <div style={{ display: stacked && stackedPane === 'tree' ? 'none' : 'flex', flex: 1, gap: '0', minWidth: 0, overflow: 'hidden' }}>
        <div
          ref={documentPaneRef}
          role="region"
          aria-label="Artifact document"
          {...documentSwipe}
          style={{ flex: 1, minWidth: 0, overflow: 'auto', display: stacked && stackedPane !== 'document' ? 'none' : 'flex', flexDirection: 'column', paddingLeft: stacked ? 0 : '10px', paddingRight: stacked ? 0 : selectedArtifact ? '5px' : '10px' }}
        >
        {/* The document and its editor read at the measure on a wide screen;
            the column itself keeps the notes handle at the window's edge. */}
        <div className={stacked ? undefined : 'measure'} style={{ width: '100%' }}>
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
            projectAttachments={projectAttachments}
            onUploadAttachment={handleUploadAttachment}
            onUploadAttachmentVersion={handleUploadAttachmentVersion}
            onRenameAttachment={handleRenameAttachment}
            onAttachmentRestored={handleAttachmentRestored}
            onDeleteAttachment={handleDeleteAttachment}
            isUploadLoading={uploadingAttachmentId === editingArtifact.id}
            uploadPercent={uploadPercent}
            links={allLinks}
            linked={linkedArtifacts}
            parentArtifacts={parentArtifacts}
            parentProjectName={parentProject?.name}
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
              // The gate covers the controls only. Without them the artifact
              // reads exactly as it does now, and an ?artifact= link from a
              // colleague on the nightly channel still opens everywhere.
              nav={
                steppingOn && readingPlace && readingPlace.total > 1
                  ? {
                      position: readingPlace.position,
                      total: readingPlace.total,
                      label: selectedArtifact.ref
                        ? `${selectedArtifact.ref} ${selectedArtifact.title}`
                        : selectedArtifact.title,
                      onStep: stepToArtifact,
                    }
                  : undefined
              }
              onEdit={handleEditArtifact}
              onDelete={handleDeleteArtifact}
              onRestore={(restored) => {
                updateArtifact(restored);
                handleSelectArtifact(restored.id);
              }}
              previewVersion={previewVersion}
              onPreviewChange={setPreviewVersion}
              // A baseline's artifact shares its id with the live one, so
              // its header acts on nothing.
              readOnly={isBaselineView}
            />
            <ArtifactDetails
              artifact={selectedArtifact} 
              links={activeLinks} 
              linked={linkedArtifacts}
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
        </div>

        {/* The notes column stays whether or not an artifact is selected: its
            comments tab needs one, its assistant tab does not. Its width is
            only spent when pinned — auto-hide floats it over the document on
            hover, and hidden leaves just the strip that brings it back. */}
        {!stacked && !notesPinned && (
          <button
            type="button"
            className="panel-edge-strip"
            aria-label={panelStripLabel('the notes panel', notesOpen)}
            aria-expanded={notesOpen}
            onMouseEnter={() => !viewport.coarsePointer && setNotesHovered(true)}
            onMouseLeave={() => setNotesHovered(false)}
            onClick={() => setNotesRevealed((shown) => !shown)}
            title={`${panelStripLabel('the notes panel', notesOpen)} (${panelModeLabel(
              notesMode
            )}) — the button inside the panel changes the mode`}
            style={{
              width: panelStripWidth(viewport.coarsePointer),
              minWidth: panelStripWidth(viewport.coarsePointer),
              borderLeft: '1px solid var(--border)',
              borderTop: 'none',
              borderRight: 'none',
              borderBottom: 'none',
              padding: 0,
              background: 'var(--surface-alt)',
              cursor: 'pointer',
              display: 'flex',
              alignItems: 'flex-start',
              justifyContent: 'center',
              paddingTop: 12,
              color: 'var(--text-muted)',
            }}
          >
            <span className="panel-edge-strip-knob" aria-hidden="true">
              {notesOpen ? '\u203a' : '\u2039'}
            </span>
          </button>
        )}
            {/* Resize handle — only a pinned column has a width to drag. */}
            {!stacked && notesPinned && (
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
            {/* Clicking away closes a panel that was revealed on purpose: an
                overlay with no way out but the edge strip is a trap. */}
            {!stacked && notesRevealed && !notesPinned && (
              <div
                onClick={() => setNotesRevealed(false)}
                style={{ position: 'fixed', inset: 0, zIndex: 899 }}
              />
            )}
            {(stacked ? stackedPane === 'notes' : notesOpen) && (
            <div
              onMouseEnter={() => !stacked && notesMode === 'autohide' && setNotesHovered(true)}
              onMouseLeave={() => !stacked && notesMode === 'autohide' && setNotesHovered(false)}
              style={{
                ...(stacked
                  ? { flex: 1, minWidth: 0 }
                  : { width: `${rightColumnWidth}px`, minWidth: '250px', maxWidth: '600px' }),
                overflow: 'hidden',
                // Unpinned, the panel floats over the document rather than
                // reflowing it whenever the pointer crosses the edge.
                ...(notesPinned || stacked
                  ? {}
                  : {
                      position: 'fixed',
                      right: panelStripWidth(viewport.coarsePointer),
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
                onReferenceClick={handleReferenceClick}
                onToggle={stacked ? () => setStackedPane('document') : cycleNotesMode}
                modeLabel={stacked ? 'Notes' : panelModeLabel(notesMode)}
                nextModeLabel={stacked ? 'Document' : panelModeLabel(nextPanelMode(notesMode))}
                onArtifactsChanged={() => {
                  // The assistant just added something: show it without
                  // making the reader go and look for it.
                  loadArtifacts();
                  loadLinks();
                }}
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
        <AttachmentViewer attachment={figureInView} onClose={() => setFigureInView(null)} />
      )}
    </div>
  );
};

export default ModuleView;
