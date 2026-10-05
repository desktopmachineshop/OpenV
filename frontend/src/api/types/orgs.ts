// The types api/orgs.ts sends and receives.
// Workspaces (orgs): members, teams, invitations, usage, limits and
// features, and who may reach a project (its members and team grants).
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import type { BillingSnapshot } from './billing';

// A pending invitation as an admin sees it on the Members tab.
export interface OrgInvitation {
  id: string;
  org_id: string;
  email: string;
  role: 'admin' | 'member';
  expires_at: string;
  created_at: string;
  invited_by_name?: string;
}

// The answer to creating an invitation: the row, the one-time link (shown
// once — the server keeps only its hash), and whether it was emailed. With
// no SMTP configured the admin passes the link on themselves.
//
// `reason` explains an invitation that was NOT emailed for some other
// cause — today, an unchanged invitation to the same address created less
// than an hour ago, which is handed back instead of mailing a second link.
// Such an answer carries no `link`: the only copy went out in the first
// mail, and minting a new one is what re-inviting at a different role does.
export interface OrgInvitationCreated {
  invitation: OrgInvitation;
  link: string;
  emailed: boolean;
  reason?: string;
}

export interface ProjectMember {
  project_id: string;
  user_id: string;
  // reviewer (REQ-150) reads like a viewer and may comment, but changes
  // nothing.
  role: 'owner' | 'editor' | 'reviewer' | 'viewer';
  user_name?: string;
  user_email?: string;
  avatar_url?: string;
}

// ---------------------------------------------------------------------------
// Organizations / workspaces
// ---------------------------------------------------------------------------

export interface Org {
  id: string;
  name: string;
  slug: string;
  type: 'personal' | 'company';
  plan: string;
  role: 'admin' | 'member';
  created_at: string;
  // Monthly spend budget (issue #186). null/undefined = no budget set.
  monthly_budget_usd?: number | null;
  // Last budget-alert dedupe state (YYYY-MM and the highest % threshold
  // already alerted that month); present only once an alert has fired.
  budget_alert_month?: string;
  budget_alert_threshold?: number;
  // Present on soft-deleted workspaces (listDeleted); restorable for 30 days
  // from this time, then permanently purged.
  deleted_at?: string;
  // True when a workspace logo is stored; fetch it via orgsAPI.logoUrl.
  has_logo?: boolean;
  // Release channel (REQ-136): the effective channel, and whether the plan
  // pins it. Company plans may set release_channel via orgsAPI.update
  // ('nightly', 'stable', or '' for the plan default).
  release_channel?: 'nightly' | 'stable';
  release_channel_locked?: boolean;
  /** The subscription snapshot; status "none" where there is no billing
   *  relationship, which is every workspace on a self-hosted deployment. */
  billing?: BillingSnapshot;
  // The stable release turned on for a stable-channel workspace ('' until
  // the first stable is cut and turns on), and its upgrade window
  // (upgrade_day 0 = releases turn on at the cut) (REQ-138).
  stable_release?: string;
  upgrade_day?: number;
  upgrade_hour?: number;
  upgrade_timezone?: string;
}

export interface OrgMember {
  org_id: string;
  user_id: string;
  role: 'admin' | 'member';
  user_name: string;
  user_email: string;
  avatar_url: string;
}

export interface OrgTeam {
  id: string;
  org_id: string;
  name: string;
  description: string;
  members: OrgMember[];
}

export interface TeamGrant {
  project_id: string;
  org_team_id: string;
  role: string;
  team_name: string;
}

// Workspace usage rollup: the same runs aggregated by agent and by day.
export interface OrgUsageTotals {
  runs: number;
  tokens_in: number;
  tokens_out: number;
  cost_usd: number;
}

export interface OrgAgentUsage extends OrgUsageTotals {
  agent_slug: string;
  agent_name: string;
}

export interface OrgDailyUsage extends OrgUsageTotals {
  day: string; // YYYY-MM-DD (UTC)
}

export interface OrgUsageSummary {
  days: number;
  totals: OrgUsageTotals;
  by_agent: OrgAgentUsage[];
  by_day: OrgDailyUsage[];
  // Current calendar month spend (UTC), what the budget bar measures against.
  month_to_date_cost_usd: number;
}

/** One workspace limit, as the limits endpoint reports it. */
export interface LimitUsage {
  key: string;
  label: string;
  description: string;
  unit: 'count' | 'mb' | 'minutes' | 'cpus' | '';
  /** The ceiling. Meaningless when `unlimited` is true. */
  limit: number;
  unlimited: boolean;
  /** Present only for limits whose usage can be counted. */
  used?: number;
  /** A ceiling nothing raises — no plan, no setting. Shown as a fact rather
   *  than as a warning that the workspace is full. */
  fixed?: boolean;
  /** resource | count | flag. A flag has no number, only `included`. */
  kind?: 'resource' | 'count' | 'flag';
  /** A flag's reading: whether the workspace's plan, or on a self-hosted
   *  deployment the deployment, includes the thing. */
  included?: boolean;
}

export interface WorkspaceLimits {
  org_id: string;
  /** The billed plan. */
  plan: string;
  /** The plan the limits were resolved from: the billed plan while a
   *  subscription is in good standing, the free tier once it lapses. */
  entitled_plan: string;
  /** none | trialing | active | past_due | canceled | … — a member sees that
   *  there is a payment problem without seeing anything about money. */
  plan_status: string;
  /** Keeps the alpha terms through its own limit overrides. */
  grandfathered: boolean;
  /** True while the workspace holds more than its plan allows: every write
   *  is refused until it upgrades or trims; reads and export never are. */
  read_only?: boolean;
  /** The limits it is past, by key. */
  over_plan?: string[];
  /** Decides which remedy to offer: a plan upgrade, or a setting to change. */
  self_hosted: boolean;
  limits: LimitUsage[];
}

// The caller's feature gates in one workspace (REQ-137): resolved from the
// workspace's channel and the stable release it has turned on, or the newest
// stable when the caller previews it (REQ-138).
export interface OrgFeatures {
  channel: 'nightly' | 'stable';
  stable_release: string;
  preview: boolean;
  features: Record<string, boolean>;
  // A stable release that is cut but not yet turned on for the workspace,
  // and when it will be.
  next_stable_release?: string;
  next_stable_at?: string;
}
