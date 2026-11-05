// The requirements page beside the floating V&V Assistant (#725, #726).
import type { Artifact } from '../../api/client';
import { useAssistantPageContext } from '../../components/wizard/assistantStore';

/**
 * Tell the floating assistant what the reader is looking at: the artifact
 * open in the detail pane (which each turn then sends), or the page itself.
 * A baseline's artifact is a snapshot, so it is named as the page rather
 * than sent as the live artifact. `refresh` reloads the page after the
 * assistant adds something to the project.
 */
export function useRequirementsAssistant(artifact: Artifact | undefined, isBaselineView: boolean, refresh: () => void) {
  useAssistantPageContext(
    artifact && !isBaselineView
      ? { label: artifact.ref ? `${artifact.ref} ${artifact.title}` : artifact.title, artifactId: artifact.id }
      : { label: 'Requirements' },
    refresh
  );
}

/** Bring the History section of the detail pane into view (the phone's Comment). */
export const revealHistory = (pane: HTMLElement | null) =>
  pane?.querySelector('.artifact-history-section')?.scrollIntoView?.({ block: 'start', behavior: 'smooth' });
