import type { FeatureKey } from './generated/contract';

// Feature gates (REQ-137) with no module of their own to sit beside: those
// the call sites named by bare strings, and the to-do list's, which a view
// and two components share, so that no component imports the view for it
// (pain point fe-requirements-13). In release.Registry's order
// (internal/domain/release/features.go). Each is typed by the generated
// contract's FeatureKey (refactor plan X4b), so a key Go renames or drops
// fails tsc here, as useFeature fails with one. The other *_FEATURE consts
// stay beside the module that owns their gate, typed the same way.

/** Parent projects, refining requirements and roll-up (flow-down). */
export const FLOW_DOWN_FEATURE: FeatureKey = 'flow-down';

/** Artifact owners, reference parties and owner-filtered downloads. */
export const ARTIFACT_OWNERS_FEATURE: FeatureKey = 'artifact-owners';

/** Share links: a public read-only view, or reviewer access. */
export const SHARE_LINKS_FEATURE: FeatureKey = 'share-links';

/**
 * The gate the to-do list page and the note's "Add to-do" control sit
 * behind, until every supported stable release carries them.
 */
export const TODO_LIST_FEATURE: FeatureKey = 'todo-list';

/** Subscribing a workspace to a plan from its settings. */
export const WORKSPACE_BILLING_FEATURE: FeatureKey = 'workspace-billing';

/** Automations an admin makes for the whole workspace. */
export const WORKSPACE_AUTOMATIONS_FEATURE: FeatureKey = 'workspace-automations';

/**
 * Connected apps (REQ-44): attaching an AI client to a workspace over the
 * remote MCP endpoint, the consent page, and the list in your settings.
 */
export const REMOTE_MCP_FEATURE: FeatureKey = 'remote-mcp';

/**
 * One connected app reaching more than one workspace: several chosen on the
 * consent page, and changed later under Connected apps.
 */
export const CONNECTED_APP_WORKSPACES_FEATURE: FeatureKey = 'connected-app-workspaces';

// A project's target releases (MVP, V1.0, ...): the Releases page and the
// release range in the artifact editor.
export const TARGET_RELEASES_FEATURE: FeatureKey = 'target-releases';
/**
 * The compact layout: the project shell's icon rail and top bar, the phone
 * tab bar, and the ? / F1 help shortcut with a help button on every page
 * that has a top bar. Shared by ProjectLayout, ProjectList and Navbar.
 */
export const COMPACT_LAYOUT_FEATURE: FeatureKey = 'compact-layout';

/**
 * The redesign's project pages: Home, Design, Tests and Baselines. Each
 * reads only what the API already serves; the menu lists them, and each page
 * renders, only while the gate is on.
 */
export const REDESIGN_PAGES_FEATURE: FeatureKey = 'redesign-pages';

/**
 * The Agents page as a hierarchy of cards (issue 733): a lead at the top,
 * groups of specialists beneath it with each card's numbers over the last 7
 * days, and Edit mode, which arranges who reports to whom. Off, the page
 * keeps its list; an agent's place is kept either way.
 */
export const AGENT_HIERARCHY_FEATURE: FeatureKey = 'agent-hierarchy';

/**
 * To-dos of their own (issue #731): one paragraph to tick off, kept off the
 * board. A note raises one, the project's To-dos page lists them, and My
 * to-dos gathers a person's across the workspace. Without it a note raises a
 * board card, as before.
 */
export const TODOS_FEATURE: FeatureKey = 'todos';

/**
 * The flat seeded team of agents (internal/seeds): the V&V Assistant, the
 * Requirements, Test and Link Auditors and the interviewer. The server seeds
 * it by the workspace's channel; the app reads nothing behind it, since the
 * Agents page shows whichever team the workspace has.
 */
export const FLAT_AGENT_TEAM_FEATURE: FeatureKey = 'flat-agent-team';

/** Reinstating an archived artifact from its page (POST /artifacts/{id}/reinstate). */
export const ARTIFACT_REINSTATE_FEATURE: FeatureKey = 'artifact-reinstate';
