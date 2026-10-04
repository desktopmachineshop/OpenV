package postgres

import "database/sql"

// This file implements the numbered schema-migration ledger (issue #38).
//
// Every schema change after the 0001 baseline is written as a Migration and
// appended to the registry below. The runner applies each numbered migration
// exactly once, inside its own transaction, and records it in the
// schema_migrations table. The ledger row and the DDL commit together, so a
// failed migration leaves neither behind.
//
// Baseline decision: migration 0001 ("baseline") wraps the legacy idempotent
// init chain (InitSchema and the schema_*.go files) and is re-executed on
// EVERY boot, even when the ledger already records it. Rationale:
//   - Databases deployed before the ledger existed have no schema_migrations
//     table but may also be missing recent additive DDL; re-running the
//     baseline preserves today's upgrade semantics for them with no special
//     casing (fresh installs, pre-ledger upgrades, and post-ledger boots all
//     take the same code path).
//   - The cost is a few dozen Exec round-trips of IF NOT EXISTS / guarded
//     DO $$ blocks — milliseconds against a warm local Postgres.
// The baseline is frozen: do not add DDL to InitSchema or the schema_*.go
// files anymore. New schema changes go into numbered migrations (0002+).
//
// BackfillOrgs / PromoteOrgColumns intentionally stay outside the ledger as
// boot-time idempotent data-migration steps (they guard themselves and
// depend on runtime state such as the agents directory), but they run under
// the same boot advisory lock via MigrateAndBackfill so concurrent boots
// cannot interleave with them.

// Migration is one numbered schema change.
type Migration struct {
	// Version is the migration number (0001, 0002, ...). Versions must be
	// unique and strictly ascending in the registry.
	Version int

	// Name is a short human-readable label recorded in the ledger.
	Name string

	// Run applies the migration inside a dedicated transaction. The runner
	// commits the transaction together with the ledger row, so on error the
	// whole migration rolls back and is not recorded. All new migrations
	// (0002 onward) must set Run.
	Run func(tx *sql.Tx) error

	// RunDB is the escape hatch used only by the 0001 baseline: an
	// idempotent body that manages its own statements on the raw connection
	// and is re-executed on every boot (see the baseline decision above).
	// Exactly one of Run / RunDB must be set.
	RunDB func(db *sql.DB) error
}

// migrations is the ordered registry of schema changes. Append new
// migrations at the end with the next version number; never renumber,
// reorder, or edit an entry that has shipped.
var migrations = []Migration{
	{Version: 1, Name: "baseline", RunDB: InitSchema},
	{Version: 2, Name: "unique_personal_org_per_user", Run: m0002UniquePersonalOrgPerUser},
	{Version: 3, Name: "agent_runs_retried_from", Run: m0003AgentRunsRetriedFrom},
	{Version: 4, Name: "artifact_status_column", Run: m0004ArtifactStatusColumn},
	{Version: 5, Name: "links_suspect", Run: m0005LinksSuspect},
	{Version: 6, Name: "notifications", Run: m0006Notifications},
	{Version: 7, Name: "review_queue_indexes", Run: m0007ReviewQueueIndexes},
	{Version: 8, Name: "hot_path_indexes", Run: m0008HotPathIndexes},
	{Version: 9, Name: "artifact_trgm_search", Run: m0009ArtifactTrgmSearch},
	{Version: 10, Name: "drop_redundant_idx_links_active", Run: m0010DropRedundantIdxLinksActive},
	{Version: 11, Name: "org_monthly_budget", Run: m0011OrgMonthlyBudget},
	{Version: 12, Name: "agent_run_failure_taxonomy", Run: m0012AgentRunFailureTaxonomy},
	{Version: 13, Name: "users_email_notifications", Run: m0013UsersEmailNotifications},
	{Version: 14, Name: "agent_run_reproducibility_snapshot", Run: m0014AgentRunReproducibilitySnapshot},
	{Version: 15, Name: "attribute_definitions", Run: m0015AttributeDefinitions},
	{Version: 16, Name: "pgvector_artifact_embeddings", Run: m0016PgvectorArtifactEmbeddings},
	{Version: 17, Name: "agent_proposal_ref", Run: m0017AgentProposalRef},
	{Version: 18, Name: "artifact_stable_refs", Run: m0018ArtifactStableRefs},
	{Version: 19, Name: "org_soft_delete", Run: m0019OrgSoftDelete},
	{Version: 20, Name: "drop_cnc_mill_default_template", Run: m0020DropCncMillDefaultTemplate},
	{Version: 21, Name: "shared_products", Run: m0021SharedProducts},
	{Version: 22, Name: "workspace_project_settings", Run: m0022WorkspaceProjectSettings},
	{Version: 23, Name: "artifact_figures", Run: m0023ArtifactFigures},
	{Version: 24, Name: "users_email_verification", Run: m0024UsersEmailVerification},
	{Version: 25, Name: "org_invitations_and_session_idle", Run: m0025OrgInvitationsAndSessionIdle},
	{Version: 26, Name: "push_subscriptions", Run: m0026PushSubscriptions},
	{Version: 27, Name: "run_partial_text_and_pending_nudge", Run: m0027RunPartialTextAndPendingNudge},
	{Version: 28, Name: "shared_product_votes", Run: m0028SharedProductVotes},
	{Version: 29, Name: "notification_history", Run: m0029NotificationHistory},
	{Version: 30, Name: "evidence_bundles", Run: m0030EvidenceBundles},
	{Version: 31, Name: "org_logo", Run: m0031OrgLogo},
	{Version: 32, Name: "baseline_created_by", Run: m0032BaselineCreatedBy},
	{Version: 33, Name: "user_avatar", Run: m0033UserAvatar},
	{Version: 34, Name: "release_announcements", Run: m0034ReleaseAnnouncements},
	{Version: 35, Name: "org_release_channel", Run: m0035OrgReleaseChannel},
	{Version: 36, Name: "release_schedule", Run: m0036ReleaseSchedule},
	{Version: 37, Name: "release_schedule", Run: m0037ReleaseSchedule},
	{Version: 38, Name: "project_hierarchy_and_owner", Run: m0038ProjectHierarchyAndOwner},
	{Version: 39, Name: "project_share_links", Run: m0039ProjectShareLinks},
	{Version: 40, Name: "users_default_org", Run: m0040UsersDefaultOrg},
	{Version: 41, Name: "attachment_titles", Run: m0041AttachmentTitles},
	{Version: 42, Name: "password_resets", Run: m0042PasswordResets},
	{Version: 43, Name: "work_item_source_note", Run: m0043WorkItemSourceNote},
	{Version: 44, Name: "attachment_version_restored_from", Run: m0044AttachmentVersionRestoredFrom},
	{Version: 45, Name: "organizations_billing", Run: m0045OrganizationsBilling},
	{Version: 46, Name: "users_billing_trial", Run: m0046UsersBillingTrial},
	{Version: 47, Name: "organizations_minutes_alert", Run: m0047OrganizationsMinutesAlert},
	{Version: 48, Name: "agent_runs_claimed_by", Run: m0048AgentRunsClaimedBy},
	{Version: 49, Name: "test_results_history", Run: m0049TestResultsHistory},
	{Version: 50, Name: "timestamptz_client_times", Run: m0050TimestamptzClientTimes},
	{Version: 51, Name: "timestamptz_share_link_expiry", Run: m0051TimestamptzShareLinkExpiry},
	{Version: 52, Name: "crew_node_references", Run: m0052CrewNodeReferences},
}
