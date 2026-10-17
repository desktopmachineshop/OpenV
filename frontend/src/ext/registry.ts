import type React from 'react';
import type { Org } from '../api/client';
import { hasCapability } from '../capabilities';

// The frontend's extension registry (docs/extending.md): what a module adds
// to the app without the app naming it. A module registers from
// ext/modules.ts, the one file a build that links modules in replaces, as
// the server's own modules are linked into cmd/server. Each entry may name
// capabilities (GET /api/v1/auth/config): it shows when the deployment
// offers any of them, and always when it names none.

export interface OrgSettingsTab {
  key: string;
  label: string;
  /** The tab it follows; at the end when absent or unknown. */
  after?: string;
  capabilities?: string[];
  Component: React.ComponentType<{ org: Org; isAdmin: boolean }>;
}

export interface AdminSection {
  key: string;
  capabilities?: string[];
  Component: React.ComponentType<{ compact: boolean }>;
}

/**
 * Runs once the session is known, and again whenever the signed-in account
 * changes, with that account's id (null signed out).
 */
export interface StartHook {
  key: string;
  capabilities?: string[];
  run: (userId: string | null) => void;
}

/**
 * A public site: the pages a visitor reads without an account, such as a
 * hosted service's marketing site (docs/plans/hosted-edition-separation.md,
 * 4.3). App.tsx keeps its one hand-written slot for it (K13): Home is the
 * front page for a visitor who is signed out, and Pages renders the site's
 * own routes for any path the app does not route, falling back to what the
 * app does with such a path. A deployment with no site offered opens on
 * sign-in.
 */
export interface Site {
  capabilities?: string[];
  /**
   * The path segments the site serves without a session, as
   * utils/publicPaths.ts matches them ("/faq" covers "/faq/x"). They are
   * public whether or not the deployment offers the site, so a visitor is
   * never sent to sign in for one.
   */
  publicPaths: string[];
  Home: React.ComponentType;
  Pages: React.ComponentType<{ fallback: React.ReactElement }>;
}

const orgSettingsTabs: OrgSettingsTab[] = [];
const adminSections: AdminSection[] = [];
const startHooks: StartHook[] = [];
let site: Site | null = null;

function add<T extends { key: string }>(list: T[], entry: T, kind: string): void {
  if (list.some((e) => e.key === entry.key)) throw new Error(`${kind} ${entry.key} is registered twice`);
  list.push(entry);
}

export const registerOrgSettingsTab = (t: OrgSettingsTab) => add(orgSettingsTabs, t, 'org settings tab');
export const registerAdminSection = (s: AdminSection) => add(adminSections, s, 'admin section');
export const registerStartHook = (h: StartHook) => add(startHooks, h, 'start hook');
export function registerSite(s: Site): void {
  if (site) throw new Error('a site is registered twice');
  site = s;
}

/** Whether the deployment's capabilities show an entry. */
export function offered(capabilities: string[] | null, entry: { capabilities?: string[] }): boolean {
  return !entry.capabilities?.length || entry.capabilities.some((c) => hasCapability(capabilities, c));
}

/**
 * The registered tabs the deployment offers, each placed after the tab it
 * names. One whose key is a core tab's is left out: the core tab keeps it.
 */
export function withOrgSettingsTabs<T extends { key: string; label: string }>(
  core: T[],
  capabilities: string[] | null
): (T | { key: string; label: string })[] {
  const out: (T | { key: string; label: string })[] = [...core];
  for (const t of orgSettingsTabs) {
    if (!offered(capabilities, t) || core.some((c) => c.key === t.key)) continue;
    const at = out.findIndex((x) => x.key === t.after);
    out.splice(at < 0 ? out.length : at + 1, 0, { key: t.key, label: t.label });
  }
  return out;
}

/** The registered tab of key, unless a core tab has that key. */
export const orgSettingsTab = (key: string, core: { key: string }[] = []) =>
  core.some((c) => c.key === key) ? undefined : orgSettingsTabs.find((t) => t.key === key);
export const registeredAdminSections = (capabilities: string[] | null) =>
  adminSections.filter((s) => offered(capabilities, s));
export const registeredStartHooks = (capabilities: string[] | null) =>
  startHooks.filter((h) => offered(capabilities, h));
/** The site, when one is registered and the deployment offers it. */
export const registeredSite = (capabilities: string[] | null): Site | null =>
  site && offered(capabilities, site) ? site : null;
/** The path segments a registered site serves without a session. */
export const sitePublicPaths = (): readonly string[] => site?.publicPaths ?? [];

/** For tests: forget every registration. */
export function resetRegistry(): void {
  orgSettingsTabs.length = 0;
  adminSections.length = 0;
  startHooks.length = 0;
  site = null;
}
