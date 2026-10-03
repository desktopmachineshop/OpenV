// The active workspace (org) as the browser remembers it, under one key in
// two storages: sessionStorage holds the workspace this tab is on (so two
// tabs can sit in two workspaces and each survive a reload), localStorage
// the workspace this browser last used. The store writes both on every
// switch, the app reads both at boot to pick a workspace, and the API
// client reads them on every request to name the workspace to the server.
// Never rename the key: it holds every member's workspace choice.
//
// Nothing is cached here: each read goes to storage, so a switch in this
// tab reaches the very next request. Storage can be unavailable (private
// mode, blocked site data); then nothing is remembered and nothing throws.

const ACTIVE_ORG_KEY = 'openv_active_org';

/** The request header that names the active workspace to the API. */
export const ORG_HEADER = 'X-Org-ID';

/**
 * The remembered workspaces, read now: this tab's (`tabOrg`) and this
 * browser's last used (`lastUsed`), each '' when there is none. They are
 * kept apart because the server's answer ranks between them at boot
 * (pickActiveOrg).
 */
export const readActiveOrg = (): { tabOrg: string; lastUsed: string } => {
  let tabOrg = '';
  let lastUsed = '';
  try {
    tabOrg = sessionStorage.getItem(ACTIVE_ORG_KEY) || '';
    lastUsed = localStorage.getItem(ACTIVE_ORG_KEY) || '';
  } catch {
    // storage unavailable: nothing is remembered
  }
  return { tabOrg, lastUsed };
};

/** Remember `id` as this tab's workspace and this browser's last used. */
export const writeActiveOrg = (id: string): void => {
  try {
    sessionStorage.setItem(ACTIVE_ORG_KEY, id);
    localStorage.setItem(ACTIVE_ORG_KEY, id);
  } catch {
    // storage unavailable — in-memory state still works
  }
};
