import { AppNotification } from '../api/client';
import { workspaceRunPath } from './agents/workspaceRuns';

// Where the bell opens a notification. Kept apart from NotificationBell so
// the mapping is a pure function over the notification. notificationPath
// (internal/notify/email.go) builds the email's link and the web push's url
// to the same page, case for case (quirk Q7).

// pathForNotification maps entity_ref to an app route. Unknown kinds fall
// back to the project overview (or the projects list without a project).
// workspaceRuns is whether the active workspace has the workspace Runs page
// (the workspace-runs feature).
export const pathForNotification = (n: AppNotification, workspaceRuns: boolean): string => {
  const ref = n.entity_ref || {};
  // Workspace alerts are not project-scoped: the budget's opens the usage
  // tab, and the cloud runner minutes' the Billing tab its text points at.
  if (ref.kind === 'org_usage') return '/org/settings?tab=usage';
  if (ref.kind === 'org_limits') return '/org/settings?tab=billing';
  // A platform release is not scoped to anything: it opens the notes.
  if (ref.kind === 'release') return '/whats-new';
  // A dedicated instance leaving its support window is a workspace matter.
  if (ref.kind === 'support_window') return '/org/settings';
  // Membership and privilege changes land on the people list they are about:
  // the workspace's members tab, or the project's own.
  if (ref.kind === 'membership') return '/org/settings?tab=members';
  if (ref.kind === 'project_membership' && ref.project_id) {
    return `/projects/${ref.project_id}/settings?tab=members`;
  }
  const projectId = ref.project_id;
  if (!projectId) {
    // A run with no project is listed on the workspace Runs page, and
    // nowhere before the workspace has it (#379 bug 168).
    if (workspaceRuns && ref.kind === 'run' && ref.run_id) return workspaceRunPath(ref.run_id);
    return '/projects';
  }
  switch (ref.kind) {
    case 'run':
    case 'proposal':
      // Proposals are reviewed from the runs view (run detail panel).
      return `/projects/${projectId}/agent-runs${ref.run_id ? `?run=${ref.run_id}` : ''}`;
    case 'interview':
      return `/projects/${projectId}/interviews`;
    case 'artifact':
      return `/projects/${projectId}/requirements`;
    default:
      return `/projects/${projectId}`;
  }
};
