// Where the floating V&V Assistant shows (#726), and what it calls the page
// beneath it when the page has registered nothing more specific.
import { PAGES } from '../../pages';

export interface AssistantRoute {
  /** Whether the launcher is drawn on this path. */
  show: boolean;
  /** The project in the URL; null on the workspace Home. */
  projectId: string | null;
  /** The page, for the "Looking at:" line: "V&V", "Board", "Home"… */
  pageLabel: string;
}

const HIDDEN: AssistantRoute = { show: false, projectId: null, pageLabel: '' };

// Deeper routes the menu does not list, named for what they show.
const DEEP_LABELS: Record<string, string> = {
  'vv/runs': 'Test run',
  'baselines/compare': 'Baseline comparison',
  'crew/network': 'Crew',
};

// The workspace's own pages, which show the assistant as the project's do.
const WORKSPACE_LABELS: Record<string, string> = {
  'org/plan/tasks': 'My tasks',
  'org/plan': 'Workspace plan',
  'org/todos': 'My to-dos',
  'org/agents': 'Agents',
  'org/runs': 'Workspace runs',
  'org/settings': 'Workspace settings',
  'whats-new': "What's new",
  admin: 'Platform admin',
};

const pageLabelFor = (rest: string[]): string => {
  const first = rest[0] || '';
  if (rest.length >= 2) {
    const deep = DEEP_LABELS[`${first}/${rest[rest.length - 1]}`] || DEEP_LABELS[`${first}/${rest[1]}`];
    if (deep) return deep;
  }
  const page = PAGES.find((p) => p.scope === 'project' && p.path === first);
  return page?.nav?.label || 'Overview';
};

/**
 * Read a pathname: the workspace Home (/projects), the workspace's own
 * pages and every project page show the assistant, except the guided
 * wizard, which has the assistant docked beside its form already. The
 * sign-in pages and the public site do not.
 */
export const assistantRoute = (pathname: string): AssistantRoute => {
  const parts = pathname.replace(/\/+$/, '').split('/').filter(Boolean);
  // A workspace page has no project in its address: the conversation is
  // the one of the project the panel names, as on Home.
  const workspace = WORKSPACE_LABELS[parts.slice(0, 3).join('/')] || WORKSPACE_LABELS[parts.slice(0, 2).join('/')] || WORKSPACE_LABELS[parts[0] || ''];
  if (workspace) return { show: true, projectId: null, pageLabel: workspace };
  if (parts[0] !== 'projects') return HIDDEN;
  if (parts.length === 1) return { show: true, projectId: null, pageLabel: 'Home' };
  const projectId = decodeURIComponent(parts[1]);
  const rest = parts.slice(2);
  if (rest[0] === 'guided') return { ...HIDDEN, projectId };
  return { show: true, projectId, pageLabel: pageLabelFor(rest) };
};

/**
 * The message that asks the assistant to draft test cases for gaps the
 * V&V dashboard lists (#728). It names each gap by reference, title, kind
 * and id, so the turn needs nothing the reader has to explain again, and the
 * transcript shows what the assistant was given.
 */
export interface GapForAssistant {
  id: string;
  ref?: string;
  title: string;
  /** The gap's kind as the card says it: "no test", "unverified", "failing"… */
  why: string;
}

export const draftTestsMessage = (gaps: GapForAssistant[]): string => {
  const refs = gaps.map((g) => g.ref || g.title).join(', ');
  const head =
    gaps.length === 1
      ? `Drafting test cases for 1 gap: ${refs}.`
      : `Drafting test cases for ${gaps.length} gaps: ${refs}.`;
  const lines = gaps.map(
    (g) => `- ${g.ref ? `${g.ref} ` : ''}${g.title} (${g.why}; artifact id ${g.id})`
  );
  return [
    head,
    '',
    'These gaps are from the V&V dashboard:',
    ...lines,
    '',
    'For each one, draft the test cases that would verify it, each with a verifies link to the requirement, as suggestions I can review. Where a gap is a failing or unverified test rather than a missing one, say what is wrong and propose the fix instead.',
  ].join('\n');
};
