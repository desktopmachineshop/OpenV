// The workspace Runs page (#379 bug 168): the workspace's agent runs that
// belong to no project (a whole-workspace automation's, a product invented
// from the projects list, those a project's delete left behind), which no
// project's Runs page lists. Here rather than in its view because the
// workspace menu, the notification bell and the projects list link to it,
// and components do not import views.

/** Gates the page, its workspace menu entry and the links to it. */
export const WORKSPACE_RUNS_FEATURE = 'workspace-runs';

export const WORKSPACE_RUNS_PATH = '/org/runs';

/** The page with a run open beside the list, as notificationPath (Go) builds it. */
export const workspaceRunPath = (runId: string): string => `${WORKSPACE_RUNS_PATH}?run=${runId}`;
