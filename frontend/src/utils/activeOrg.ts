import { Org } from '../api/client';

/** Feature gate for choosing a default workspace (REQ-137). */
export const DEFAULT_WORKSPACE_FEATURE = 'default-workspace';

/**
 * Which workspace the app opens in once the member's workspaces are known.
 *
 * In order: the workspace this tab was on (session storage, so two tabs can
 * sit in two workspaces and each survive a reload); what the server says is
 * active — the switch made in this sign-in, else the member's chosen default
 * workspace (REQ-156), else their personal one; the last workspace this
 * browser used; the personal workspace; anything at all.
 *
 * The server's answer outranks the browser's memory on purpose: a member who
 * chose the company workspace as their default should land there on a new
 * sign-in even if this browser last sat in their personal one.
 */
export const pickActiveOrg = (
  orgs: Pick<Org, 'id' | 'type'>[],
  tabOrg: string,
  serverActive: string,
  lastUsed: string
): string => {
  const has = (id: string) => !!id && orgs.some((o) => o.id === id);
  if (has(tabOrg)) return tabOrg;
  if (has(serverActive)) return serverActive;
  if (has(lastUsed)) return lastUsed;
  return orgs.find((o) => o.type === 'personal')?.id || orgs[0]?.id || '';
};
