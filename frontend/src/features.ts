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
