import { useCallback, useEffect, useState } from 'react';
import {
  artifactAPI,
  linkAPI,
  attachmentAPI,
  qualityAPI,
  projectAPI,
  Artifact,
  Attachment,
  Link,
  LinkedArtifact,
  Project,
  ProjectExport,
} from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import type { QualityRowInfo } from '../../components/ArtifactList';
import type { Setter } from './shared';

export interface ModuleDataOptions {
  projectId: string;
  /** The store's artifact list, which the module tree is built from. */
  setArtifacts: (artifacts: Artifact[]) => void;
  /** The shell's error banner. */
  setError: Setter<string>;
  /** useBaselineView's list load, the third of the project loads. */
  loadBaselines: () => Promise<void>;
  /** useBaselineView's state, which opening a project puts back on live. */
  setActiveBaselineId: Setter<string>;
  setBaselineData: Setter<ProjectExport | null>;
}

/**
 * What the module loads for its project: the artifacts (into the store), the
 * links and the far ends of cross-project links, the parent project and its
 * requirements, the quality scores and every figure in the project. The
 * parent-project effect runs first, then the project load, which also loads
 * the baselines and goes back to the live project; the loaders are returned
 * so the shell's handlers reload what they change.
 *
 * Every useCallback and useEffect keeps the dependency array it had in
 * ModuleView. The setters passed in are React state setters, stable for the
 * life of the component, so the two arrays that leave them out keep firing as
 * they did.
 */
export function useModuleData({
  projectId,
  setArtifacts,
  setError,
  loadBaselines,
  setActiveBaselineId,
  setBaselineData,
}: ModuleDataOptions) {
  const [allLinks, setAllLinks] = useState<Link[]>([]);
  // Flow-down (REQ-145): the far end of every link crossing into another
  // project, and the parent project's requirements a requirement here may
  // refine.
  const [linkedArtifacts, setLinkedArtifacts] = useState<LinkedArtifact[]>([]);
  const [parentProject, setParentProject] = useState<Project | null>(null);
  const [parentArtifacts, setParentArtifacts] = useState<Artifact[]>([]);
  // Every figure in the project, as opposed to the selected artifact's. It is
  // what lets a "##" citation be offered while writing and opened while
  // reading, neither of which can wait to find out which artifact holds the
  // figure in question.
  const [projectAttachments, setProjectAttachments] = useState<Attachment[]>([]);
  // Per-requirement quality scores keyed by artifact id (issue #217); drives
  // the score badge on requirement rows.
  const [qualityScores, setQualityScores] = useState<Record<string, QualityRowInfo>>({});

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
    // setError is the shell's state setter, stable; the array is ModuleView's.
    // eslint-disable-next-line react-hooks/exhaustive-deps
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
    // setActiveBaselineId and setBaselineData are useBaselineView's state
    // setters, stable; the array is ModuleView's.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId, loadArtifacts, loadLinks, loadBaselines, loadQuality, loadProjectAttachments]);

  return {
    allLinks,
    setAllLinks,
    linkedArtifacts,
    parentProject,
    parentArtifacts,
    projectAttachments,
    setProjectAttachments,
    qualityScores,
    loadArtifacts,
    loadLinks,
    loadQuality,
  };
}
