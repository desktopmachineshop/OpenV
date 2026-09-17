import React from 'react';

/**
 * Things a member should know BEFORE starting a vendor CLI sign-in.
 *
 * A sign-in that cannot succeed for this account is not a failure worth
 * discovering at the end of the flow. Anything here is rendered above the
 * Connect button wherever that provider's sign-in is offered.
 */

/**
 * The Gemini CLI's tier wall (issue #360).
 *
 * On 18 June 2026 Google stopped serving Gemini CLI to the free, Google One,
 * AI Pro and AI Ultra tiers and moved them to Antigravity CLI. OpenV's sign-in
 * drives Code Assist OAuth, so for an account on one of those tiers Google's
 * own page answers with a deprecation notice and nothing on this side can get
 * past it.
 *
 * The API already appends the same explanation to a FAILED Gemini sign-in
 * (geminiTierNote, internal/runner/geminicli.go). Saying it here as well is
 * the point: a wall you are told about only after walking into it is not much
 * of a warning, and both ways round it — an account on a Code Assist Standard
 * or Enterprise licence, or a workspace Gemini API key — are choices to make
 * before starting rather than after.
 */
export const GEMINI_TIER_CAUTION: React.ReactNode = (
  <>
    <b>Google account tiers:</b> since 18 June 2026 the Gemini CLI signs in only accounts on a{' '}
    <b>Gemini Code Assist Standard or Enterprise</b> licence. The free, Google One, AI Pro and AI
    Ultra tiers moved to Antigravity CLI, and Google answers a sign-in from one of them with a
    deprecation page that no sign-in here can get past. Set a Gemini API key on the workspace
    instead (Workspace settings → Providers): it is still served, and it is what the Antigravity
    CLI agent runs on.
  </>
);

/** The caution for a provider, or undefined where there is nothing to say. */
export const providerCaution = (provider: string): React.ReactNode | undefined =>
  provider === 'gemini-cli' ? GEMINI_TIER_CAUTION : undefined;
