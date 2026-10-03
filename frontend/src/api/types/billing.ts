// The types api/billing.ts sends and receives.
// Plans and prices, and a workspace's billing: checkout, plan changes
// and the billing portal.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

/** One interval's amounts per lowercase currency code, in minor units. */
export interface PublicPrice {
  amounts: Record<string, number>;
  tax_behavior?: string;
}

export interface PublicPlan {
  plan: string;
  /** The amount is per member; a flat plan bills once. */
  per_seat: boolean;
  intervals: Record<string, PublicPrice>;
}

/** The pricing page's catalogue: what is for sale, as the platform last
 *  confirmed it with the billing provider. billing_enabled is false where
 *  no provider is configured, and the page shows its usual copy. */
export interface PublicPlans {
  billing_enabled: boolean;
  as_of?: string;
  currencies: string[];
  plans: PublicPlan[];
}

/** The mirrored subscription snapshot on a workspace. Never money, never a
 *  provider id: what decides entitlement and what the Billing tab shows. */
export interface BillingSnapshot {
  /** none | trialing | active | past_due | unpaid | paused | canceled | incomplete | disputed */
  status: string;
  interval?: string;
  seats?: number;
  period_end?: string;
  cancel_at_period_end?: boolean;
  grandfathered?: boolean;
  synced_at?: string;
}

/** What the Billing tab reads. */
export interface BillingState {
  org_id: string;
  /** The billed plan. */
  plan: string;
  /** The plan the limits resolve from now. */
  entitled_plan: string;
  /** A platform admin's plan: nothing to buy. */
  granted: boolean;
  billing: BillingSnapshot;
  self_hosted: boolean;
  plans: PublicPlans;
}
