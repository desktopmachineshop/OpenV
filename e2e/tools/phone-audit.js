#!/usr/bin/env node
// Layout audit of every screen at phone and desktop sizes.
//
// Renders a production build on a Pixel 5 (Chromium), an iPhone 13 (WebKit)
// and a set of desktop profiles (1024 px laptop to 4K, plus HiDPI) against a
// mocked API, opens the modals and sheets reachable from each screen, and
// reports per screen:
//   - elements extending past the viewport's right edge (page overflow),
//   - containers whose content is clipped horizontally (scrollWidth >
//     clientWidth with overflow hidden/auto/scroll), excluding known
//     scrollers such as .table-scroll and .tab-strip,
//   - interactive elements with a tap target under 32 px,
//   - visible text under 12 px,
//   - on desktop profiles only: text blocks running wider than a readable
//     measure, form fields stretched across the screen, and raster images
//     drawn larger than their pixels (blur on HiDPI),
//   - page errors.
// It is the regression tool for the class of defect fixed in the phone
// polish pass (docs/plans/mobile-support.md §8): run it after any layout
// change and compare the summary with the previous run.
//
// Usage (from e2e/, with the frontend build served statically):
//   cd frontend && CI=true npm run build && npx serve -s build -l 5000
//   cd e2e && npm run audit:phone        # Pixel 5
//   cd e2e && npm run audit:desktop      # every desktop profile
// Environment:
//   BASE_URL       where the build is served (default http://127.0.0.1:5000)
//   OUT            directory for audit.json and screenshots
//                  (default test-results/phone-audit)
//   ENGINES        comma list of android, iphone, desktop (default android);
//                  desktop:<profile+...> restricts the desktop pass, e.g.
//                  desktop:laptop-1366+fhd-1920 (see DESKTOP_PROFILES)
//   TAGS           comma list of screen tags to restrict the run
//   CHROMIUM_PATH  executablePath override for Chromium
//   SHOT_SIZE      exact-size screenshot mode for the PWA manifest's
//                  screenshots, which must declare fixed pixel dimensions:
//                  <cssWidth>x<cssHeight>@<dpr> replaces the phone profile's
//                  viewport (e.g. 360x640@3 => 1080x1920 PNGs)
//   SHOT_FULLPAGE  0 clips each screenshot to the viewport instead of the
//                  full page, which is what a fixed size needs
// Exit code is 1 when a screen fails to render or throws a page error;
// layout findings are reported, not failed, because some (ag-grid's
// virtualised columns, deliberate scrollers) are expected.
const { chromium, webkit, devices } = require('playwright');
const fs = require('fs');
const path = require('path');
const OUT = process.env.OUT || path.join(__dirname, '..', 'test-results', 'phone-audit');
const BASE_URL = process.env.BASE_URL || 'http://127.0.0.1:5000';
fs.mkdirSync(OUT, { recursive: true });
const now = '2026-09-01T09:00:00Z';
const user = { id: 'u1', email: 'sam@example.com', name: 'Sam Example', avatar_url: '' };
const org = { id: 'org1', name: "Sam's Space", slug: 'sam', type: 'personal', plan: 'free', role: 'admin', created_at: now };
const org2 = { id: 'org2', name: 'Northwind Machines Engineering Group', slug: 'northwind', type: 'company', plan: 'free', role: 'admin', created_at: now };
const project = { id: 'p1', org_id: 'org1', name: 'Benchtop CNC Mill Product Definition', description: 'Desktop 3-axis mill for prototyping shops. A longer description to check wrapping on narrow screens.', agent_auth: 'user-account', created_at: now, updated_at: now };
let sort = 0;
const art = (id, type, title, body, parent, status, attrs) => ({ id, project_id: 'p1', parent_id: parent || null, type, ref: id.toUpperCase(), title, body, sort_order: ++sort, status: status || 'draft', attributes: attrs || {}, version: 2, valid_from: now, valid_to: null, created_at: now, updated_at: now });
const artifacts = [
  art('hdg-1', 'heading', 'Stakeholder needs', ''),
  art('need-1', 'user-need', 'Cut aluminium parts without a machinist', 'As a prototyping engineer I need to machine small aluminium parts from a CAD file in one afternoon.', 'hdg-1', 'approved'),
  art('hdg-2', 'heading', 'Functional requirements', ''),
  art('req-1', 'requirement', 'Work envelope of at least 300 by 200 by 100 millimetres', 'The mill shall provide a work envelope of at least 300 mm × 200 mm × 100 mm (X × Y × Z).', 'hdg-2', 'approved', { priority: 'must', verification_method: 'inspection', verification_status: 'verified' }),
  art('req-2', 'requirement', 'Positioning accuracy', 'The mill shall position the tool within ±0.02 mm of the commanded position over the full envelope at 20 °C.', 'hdg-2', 'in_review', { priority: 'must', verification_method: 'test' }),
  art('hdg-4', 'heading', 'Verification', ''),
  art('tc-1', 'test-case', 'Accuracy survey with a ballbar', 'Run the circular ballbar test at three heights.', 'hdg-4', 'approved'),
];
const byId = Object.fromEntries(artifacts.map((a) => [a.id, a]));
const link = (id, from, to, type) => ({ id, from_id: from, to_id: to, type, suspect: id === 'l2', attributes: null, version: 1, valid_from: now, valid_to: null, created_at: now, updated_at: now });
const links = [link('l1', 'req-1', 'need-1', 'derives-from'), link('l2', 'req-2', 'need-1', 'derives-from'), link('l5', 'tc-1', 'req-2', 'verifies')];
const agent = (id, name) => ({ id, org_id: 'org1', name, slug: name.toLowerCase().replace(/\s+/g, '-'), description: 'Drafts and reviews requirements against the house style, then proposes changes for approval.', provider: 'claude', model: 'claude-sonnet-5', effort: 'medium', write_mode: 'proposal', system_prompt: 'You are…', tools: [], locked: false, seed_key: null, created_at: now, updated_at: now });
const agents = [agent('a1', 'Requirements Analyst'), agent('a2', 'V&V Engineer'), agent('a3', 'Developer')];
const run = (id, status) => ({ id, org_id: 'org1', project_id: 'p1', agent_id: 'a1', agent_name: 'Requirements Analyst', status, prompt: 'Review the safety requirements for testability and propose fixes where the wording is weak.', final_text: status === 'succeeded' ? 'Reviewed 6 requirements; proposed 2 rewrites.' : '', error: status === 'failed' ? 'runner lost' : '', launched_by: 'u1', launched_by_name: 'Sam Example', created_at: now, updated_at: now, started_at: now, finished_at: now, usage: { input_tokens: 12000, output_tokens: 800, cost_usd: 0.12 } });
const runs = [run('r1', 'succeeded'), run('r2', 'running'), run('r3', 'failed')];
const workItems = [
  { id: 'w1', project_id: 'p1', title: 'Write the enclosure interlock test procedure', description: 'Cover door open at speed.', column: 'backlog', sort_order: 0, assignee_type: 'user', assignee_id: 'u1', artifact_ids: ['req-2'], created_at: now, updated_at: now },
  { id: 'w2', project_id: 'p1', title: 'Draft spindle requirements', description: '', column: 'in-progress', sort_order: 0, assignee_type: 'agent', assignee_id: 'a1', agent_run_id: 'r2', artifact_ids: [], created_at: now, updated_at: now },
  { id: 'w3', project_id: 'p1', title: 'Review noise requirement', description: '', column: 'review', sort_order: 0, assignee_type: 'user', assignee_id: null, artifact_ids: ['req-1'], created_at: now, updated_at: now },
];
const notifications = [
  { id: 'n1', user_id: 'u1', type: 'proposal_pending', title: 'Proposal awaiting review', body: 'Requirements Analyst proposed a change to REQ-2 Positioning accuracy.', read: false, link: '/projects/p1/review', created_at: now },
  { id: 'n2', user_id: 'u1', type: 'run_failed', title: 'Run failed', body: 'Developer run r3 failed: runner lost.', read: true, link: '/projects/p1/agent-runs', created_at: now },
];
const testRun = { id: 'tr1', project_id: 'p1', name: 'Accuracy survey September', description: 'Ballbar at three heights', status: 'completed', started_at: now, completed_at: now, created_at: now, updated_at: now };
const baseline = { id: 'b1', project_id: 'p1', name: 'Release candidate 1', created_at: now };
const interview = { id: 'i1', project_id: 'p1', title: 'Lab manager interview', description: 'Safety and noise', status: 'open', invite_token: 'tok', created_at: now, updated_at: now };
const automation = { id: 'au1', org_id: 'org1', project_id: 'p1', name: 'Nightly requirements lint', agent_id: 'a1', kind: 'cron', enabled: true, prompt_template: 'Lint everything', cron_expr: '0 2 * * *', catch_up: false, event_type: '', event_filter: {}, cooldown_seconds: 0, max_runs_per_hour: 0, created_at: now, updated_at: now };
const crewTeam = { id: 't1', org_id: 'org1', name: "Founder's Dev Team", description: 'Analyst → Developer → Reviewer', nodes: [{ id: 'n1', team_id: 't1', kind: 'agent', agent_id: 'a1', label: 'Analyst', x: 80, y: 80 }, { id: 'n2', team_id: 't1', kind: 'agent', agent_id: 'a3', label: 'Developer', x: 320, y: 80 }], edges: [{ id: 'e1', team_id: 't1', from_node_id: 'n1', to_node_id: 'n2', type: 'handoff' }], created_at: now, updated_at: now };
const usage = { window_days: 30, totals: { runs: 12, input_tokens: 240000, output_tokens: 18000, cost_usd: 3.4 }, month_to_date_usd: 3.4, monthly_budget_usd: 20, by_agent: [{ agent_id: 'a1', agent_name: 'Requirements Analyst', runs: 8, input_tokens: 180000, output_tokens: 12000, cost_usd: 2.6 }], by_day: [{ day: '2026-09-01', runs: 3, cost_usd: 0.9 }] };
const coverageOld = { summary: { total_requirements: 3, verified: 1, covered: 2, uncovered: 1, coverage_pct: 66.7 }, requirements: [{ id: 'req-1', ref: 'REQ-1', title: 'Work envelope', status: 'verified', test_cases: [{ id: 'tc-1', ref: 'TC-1', title: 'Accuracy survey with a ballbar', last_result: 'pass' }] }, { id: 'req-2', ref: 'REQ-2', title: 'Positioning accuracy', status: 'uncovered', test_cases: [] }] };
// The community pool behind the new-project wizard's random mode, with vote
// counts so the Top 5 filters have something to lay out.
const sharedProduct = (id, name, votes, votesWeek) => ({ id, category: 'kitchen appliance', name, description: `${name} recognises Kevin and locks itself until he owns up.`, vision: `${name} becomes the reason the office bean jar survives a Tuesday.`, problem: 'Beans vanish overnight and nobody admits to owning the grinder.', target_users: 'coffee-obsessed office workers whose beans keep leaving with Kevin', votes, votes_week: votesWeek, voted: false });
const sharedProducts = [sharedProduct('sp1', 'Kevinproof', 12, 5), sharedProduct('sp2', 'Crustodian', 9, 4), sharedProduct('sp3', 'Loafwatch', 7, 2), sharedProduct('sp4', 'Mugshot Registry', 4, 1), sharedProduct('sp5', 'Fridge Amnesty Box', 2, 1)];
const routes = [
  [/\/api\/v1\/shared-products/, (url) => (/sort=top/.test(url) ? sharedProducts.slice(0, 5) : sharedProducts)],
  [/\/api\/v1\/auth\/me/, user],
  [/\/api\/v1\/auth\/config/, { google_enabled: false, oidc_enabled: false }],
  [/\/api\/v1\/orgs$/, { orgs: [org, org2], active_org: 'org1' }],
  [/\/api\/v1\/orgs\/[^/]+\/usage/, usage],
  [/\/api\/v1\/orgs\/[^/]+\/worker-status/, { workers: [{ id: 'wk1', name: 'Sam laptop', personal: true, hosted: false, user_name: 'Sam Example', online: true, revoked: false, last_used_at: now }], queue: { queued: 1, oldest_queued_seconds: 40, queued_repo_access: 0 } }],
  [/\/api\/v1\/projects\/p1\/download\/options/, { sections: [{ id: 'hdg-2', ref: 'HDG-2', number: '2', title: 'Functional requirements', artifacts: 2 }], types: [{ type: 'requirement', count: 2 }, { type: 'user-need', count: 1 }], attachments: [] }],
  [/\/api\/v1\/baselines\/b1\/diff/, { base: { id: 'b1', name: 'Release candidate 1' }, target: { id: 'live', name: 'Live project' }, added: [{ id: 'req-1', type: 'requirement', title: 'Work envelope' }], removed: [], modified: [{ id: 'req-2', type: 'requirement', old_title: 'Positioning accuracy', new_title: 'Positioning accuracy (tightened)', title_changed: true, body_changed: true, type_changed: false, status_changed: false, parent_changed: false }], links_added: [], links_removed: [] }],
  [/\/api\/v1\/orgs\/[^/]+\/members/, [{ user_id: 'u1', email: user.email, name: user.name, role: 'admin', joined_at: now }]],
  [/\/api\/v1\/orgs\/[^/]+\/teams/, []],
  [/\/api\/v1\/orgs\/[^/]+\/runners/, []],
  [/\/api\/v1\/orgs\/[^/]+\/worker-keys/, []],
  [/\/api\/v1\/orgs\/[^/]+\/provider-keys/, []],
  [/\/api\/v1\/orgs\/[^/]+\/quality-rules/, { effective: { convention: 'shall', severities: {} }, workspace: null, project: null, summary: 'shall', catalog: { conventions: ['shall', 'rfc2119'], rules: [], severities: ['error', 'warning', 'info', 'off'], defaults: { convention: 'shall', severities: {} }, labels: {} } }],
  [/\/api\/v1\/orgs\/org1$/, org],
  [/\/api\/v1\/orgs\//, {}],
  [/\/api\/v1\/meta\/artifact-types/, [{ type: 'requirement', label: 'Requirement' }, { type: 'heading', label: 'Heading' }, { type: 'user-need', label: 'User Need' }, { type: 'test-case', label: 'Test Case' }]],
  [/\/api\/v1\/meta\/link-types/, []],
  [/\/api\/v1\/meta\/attribute-definitions/, []],
  [/\/api\/v1\/attribute-definitions/, []],
  [/\/api\/v1\/templates/, []],
  [/\/api\/v1\/guided-sessions/, []],
  [/\/api\/v1\/projects\/p1\/guided/, []],
  [/\/api\/v1\/projects\/p1\/profile/, { product_name: 'Benchtop CNC Mill', one_liner: 'Desktop 3-axis mill', audience: 'Prototyping shops', problem: 'Waiting a week for the shop', solution: 'Mill on the desk', personas: [] }],
  [/\/api\/v1\/projects\/p1\/interviews/, [interview]],
  [/\/api\/v1\/projects\/p1\/interview-sessions/, []],
  [/\/api\/v1\/projects\/p1\/baselines\/b1\/(diff|compare)/, { added: [artifacts[3]], removed: [], modified: [{ before: artifacts[4], after: { ...artifacts[4], title: 'Positioning accuracy (tightened)' } }] }],
  [/\/api\/v1\/projects\/p1\/baselines/, [baseline]],
  [/\/api\/v1\/projects\/p1\/quality-rules/, { effective: { convention: 'shall', severities: {} }, workspace: null, project: null, summary: 'shall', catalog: { conventions: ['shall', 'rfc2119'], rules: [], severities: ['error', 'warning', 'info', 'off'], defaults: { convention: 'shall', severities: {} }, labels: {} } }],
  [/\/api\/v1\/projects\/p1\/quality/, { entries: [] }],
  [/\/api\/v1\/projects\/p1\/vv\/coverage/, { project_id: 'p1', entries: [{ requirement_id: 'req-1', title: 'Work envelope of at least 300 by 200 by 100 millimetres', verification_method: 'inspection', verification_status: 'verified', test_case_ids: ['tc-1'], latest_results: { 'tc-1': 'pass' }, rollup: 'pass' }, { requirement_id: 'req-2', title: 'Positioning accuracy', verification_method: 'test', verification_status: '', test_case_ids: [], latest_results: {}, rollup: 'uncovered' }], summary: { total: 2, covered: 1, uncovered: 1, verified: 1, pass: 1, fail: 0 } }],
  [/\/api\/v1\/projects\/p1\/vv\/gaps/, { uncovered: ['req-2'], unverified: ['req-2'], failing: [] }],
  [/\/api\/v1\/projects\/p1\/vv\/matrix/, [{ requirement_id: 'req-1', title: 'Work envelope of at least 300 by 200 by 100 millimetres', user_need_ids: ['need-1'], design_ids: [], test_case_ids: ['tc-1'], latest_results: { 'tc-1': 'pass' }, hazard_ids: [] }, { requirement_id: 'req-2', title: 'Positioning accuracy', user_need_ids: ['need-1'], design_ids: [], test_case_ids: [], latest_results: {}, hazard_ids: [] }]],
  [/\/api\/v1\/projects\/p1\/test-runs\/tr1/, testRun],
  [/\/api\/v1\/test-runs\/tr1\/results/, [{ id: 'res1', run_id: 'tr1', test_case_id: 'tc-1', status: 'pass', notes: 'Ballbar within spec at all heights.', evidence: [], executed_at: now }]],
  [/\/api\/v1\/test-runs\/tr1/, testRun],
  [/\/api\/v1\/projects\/p1\/test-runs/, [testRun]],
  [/\/api\/v1\/projects\/p1\/work-items/, workItems],
  [/\/api\/v1\/work-items\/w1/, { ...workItems[0], activity: [{ id: 'act1', work_item_id: 'w1', kind: 'comment', actor: 'Sam Example', content: 'Started drafting.', payload: {}, created_at: now }] }],
  [/\/api\/v1\/projects\/p1\/members/, [{ user_id: 'u1', email: user.email, name: user.name, role: 'owner' }]],
  [/\/api\/v1\/projects\/p1\/team-access/, []],
  [/\/api\/v1\/projects\/p1\/repo-connections/, [{ id: 'rc1', project_id: 'p1', name: 'openv', remote_url: 'https://github.com/desktopmachineshop/OpenV.git', default_branch: 'master', credential_strategy: 'host', created_at: now, updated_at: now }]],
  [/\/api\/v1\/projects\/p1\/activity/, { entries: [{ id: 'ev1', event_type: 'artifact.updated', actor: 'Sam Example', summary: 'Updated REQ-2 Positioning accuracy', created_at: now }] }],
  [/\/api\/v1\/projects\/p1\/impact/, { project_id: 'p1', artifact_id: 'req-2', direction: 'both', downstream: [{ type: 'test-case', count: 1, nodes: [{ artifact_id: 'tc-1', title: 'Accuracy survey with a ballbar', type: 'test-case', distance: 1, via: 'verifies', path: ['req-2', 'tc-1'] }] }], upstream: [{ type: 'user-need', count: 1, nodes: [{ artifact_id: 'need-1', title: 'Cut aluminium parts without a machinist', type: 'user-need', distance: 1, via: 'derives-from', path: ['req-2', 'need-1'] }] }], total: 2 }],
  [/\/api\/v1\/projects\/p1\/review-queue/, { suspect_links: [links[1]], in_review: [artifacts[4]] }],
  [/\/api\/v1\/projects\/p1$/, project],
  [/\/api\/v1\/projects\/p1\//, []],
  [/\/api\/v1\/projects$/, [project, { ...project, id: 'p2', name: 'Second project' }]],
  [/\/api\/v1\/artifacts\?/, artifacts],
  [/\/api\/v1\/artifacts\/[^/]+\/versions/, (url) => [byId[url.match(/artifacts\/([^/]+)\/versions/)[1]]]],
  [/\/api\/v1\/artifacts\/[^/]+\/quality/, { score: 96, band: 'good', findings: [] }],
  [/\/api\/v1\/artifacts\/([^/]+)\/links/, (url) => { const id = url.match(/artifacts\/([^/]+)\/links/)[1]; return links.filter((l) => l.from_id === id || l.to_id === id); }],
  [/\/api\/v1\/artifacts\/[^/]+\/attachments/, []],
  [/\/api\/v1\/artifacts\/[^/]+\/comments/, []],
  [/\/api\/v1\/artifacts\/([^/?]+)(\?|$)/, (url) => byId[url.match(/artifacts\/([^/?]+)/)[1]] || {}],
  [/\/api\/v1\/links/, links],
  [/\/api\/v1\/notifications\/stream/, null],
  [/\/api\/v1\/notifications/, { notifications, unread: 1 }],
  [/\/api\/v1\/agents/, agents],
  [/\/api\/v1\/teams\/t1/, crewTeam],
  [/\/api\/v1\/teams/, [crewTeam]],
  [/\/api\/v1\/crew-templates/, []],
  [/\/api\/v1\/automations/, [automation]],
  [/\/api\/v1\/agent-runs\/r1\/(messages|events|log)/, []],
  [/\/api\/v1\/agent-runs\/r1/, runs[0]],
  [/\/api\/v1\/agent-runs/, runs],
  [/\/api\/v1\/proposals/, [{ id: 'pr1', org_id: 'org1', project_id: 'p1', run_id: 'r1', agent_name: 'Requirements Analyst', kind: 'update_artifact', target_id: 'req-2', target_ref: 'REQ-2', summary: 'Tighten the accuracy wording', payload: { title: 'Positioning accuracy', body: 'The mill shall…' }, status: 'pending', created_at: now }]],
  [/\/api\/v1\/runner-sessions/, []],
  [/\/api\/v1\/chatter/, []],
  [/\/api\/v1\/search/, { hits: [] }],
];
async function mock(page) {
  await page.route('**/api/**', async (route) => {
    const url = route.request().url();
    for (const [re, body] of routes) if (re.test(url)) {
      if (body === null) return route.abort();
      const b = typeof body === 'function' ? body(url) : body;
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(b) });
    }
    return route.fulfill({ status: 200, contentType: 'application/json', body: route.request().method() === 'GET' ? '[]' : '{}' });
  });
}
const AUDIT = (opts) => {
  const vw = window.innerWidth;
  const desktop = !!(opts && opts.desktop);
  const inScroller = (el) => {
    for (let p = el.parentElement; p && p !== document.body; p = p.parentElement) {
      const o = getComputedStyle(p).overflowX;
      if (o === 'auto' || o === 'scroll') return true;
      if (p.classList.contains('ag-root-wrapper')) return true;
    }
    return false;
  };
  const vis = (el) => { const r = el.getBoundingClientRect(); const cs = getComputedStyle(el); return r.width > 0 && r.height > 0 && cs.visibility !== 'hidden' && cs.display !== 'none'; };
  const desc = (el) => `${el.tagName.toLowerCase()}${el.id ? '#' + el.id : ''}${el.className && typeof el.className === 'string' ? '.' + el.className.trim().split(/\s+/).slice(0, 2).join('.') : ''} "${(el.getAttribute('aria-label') || el.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 40)}"`;
  const out = { overflow: [], clipped: [], small: [], tiny: [], lines: [], stretched: [], blurry: [], scrollWidth: document.documentElement.scrollWidth, vw, dpr: window.devicePixelRatio, coarse: matchMedia('(hover: none) and (pointer: coarse)').matches };
  // Desktop-only measures: a text block wider than this is hard to read; a
  // field wider than this is a form stretched across the screen.
  const MEASURE = 900;
  const FIELD = 720;
  const all = Array.from(document.querySelectorAll('body *'));
  for (const el of all) {
    if (!vis(el)) continue;
    const r = el.getBoundingClientRect();
    const cs = getComputedStyle(el);
    // Something past the right edge is a defect unless an ancestor scrolls
    // sideways on purpose (a table wrapper, the board's column strip, an
    // ag-grid viewport): those are reachable by a swipe.
    if (r.right > vw + 1 && r.left < vw && !inScroller(el)) out.overflow.push({ el: desc(el), right: Math.round(r.right), left: Math.round(r.left) });
    // A clipped container is a defect unless it is a scroller, a deliberate
    // one-line ellipsis, a text field with a long value, or ag-grid's
    // virtualised viewports.
    if (/(hidden|clip)/.test(cs.overflowX) && el.scrollWidth > el.clientWidth + 2 && cs.textOverflow !== 'ellipsis' && !el.matches('input') && !el.closest('pre, code, .table-scroll, .tab-strip, textarea, svg, canvas, .ag-root-wrapper')) {
      out.clipped.push({ el: desc(el), scrollWidth: el.scrollWidth, clientWidth: el.clientWidth, ellipsis: cs.textOverflow === 'ellipsis' });
    }
    // Tap targets: 32 px is the floor for a control; a checkbox or radio
    // passes at 18 px because its label is the target too; a link that
    // runs inline in a sentence is text, not a control.
    if (el.matches('button, a[href], input:not([type=hidden]), select, textarea, [role=button], [role=tab], [role=radio], [role=checkbox]') && !el.closest('svg')) {
      const isTick = el.matches('input[type=checkbox], input[type=radio]');
      const inlineLink = el.matches('a') && cs.display === 'inline';
      // A mouse needs less than a finger: 24 px is the click floor on desktop.
      const floor = isTick ? (desktop ? 13 : 18) : desktop ? 24 : 32;
      if (!inlineLink && (r.height < floor || r.width < floor)) out.small.push({ el: desc(el), w: Math.round(r.width), h: Math.round(r.height) });
    }
    if (desktop) {
      // A paragraph-sized text block laid out wider than a readable measure.
      if (el.matches('p, li, dd, blockquote, h1, h2, h3, h4, label, span, div') && r.width > MEASURE && !el.closest('pre, code, table, textarea, .ag-root-wrapper, [contenteditable]')) {
        const ownText = Array.from(el.childNodes).filter((n) => n.nodeType === 3).map((n) => n.textContent).join(' ').replace(/\s+/g, ' ').trim();
        // Only text that actually wraps: enough characters to fill more than
        // one line at this width, and rendered on more than one line.
        const lineHeight = parseFloat(cs.lineHeight) || parseFloat(cs.fontSize) * 1.4;
        if (ownText.length > 200 && r.height > lineHeight * 1.5) out.lines.push({ el: desc(el), width: Math.round(r.width), chars: ownText.length });
      }
      // A single-line field stretched across the screen.
      if (el.matches('input:not([type=checkbox]):not([type=radio]):not([type=range]):not([type=file]):not([type=hidden]), select') && r.width > FIELD && !el.closest('.ag-root-wrapper')) {
        out.stretched.push({ el: desc(el), width: Math.round(r.width) });
      }
      // A raster drawn larger than its pixels: blurry, worst on HiDPI.
      if (el.matches('img') && el.naturalWidth > 0 && !/\.svg(\?|$)/i.test(el.currentSrc || el.src || '')) {
        const cssPixelsAvailable = el.naturalWidth / window.devicePixelRatio;
        if (r.width > cssPixelsAvailable * 1.05) out.blurry.push({ el: desc(el), rendered: Math.round(r.width), natural: el.naturalWidth, dpr: window.devicePixelRatio, src: (el.currentSrc || el.src || '').split('/').pop() });
      }
    }
    const fs = parseFloat(cs.fontSize);
    if (fs < 12 && el.childNodes.length && Array.from(el.childNodes).some((n) => n.nodeType === 3 && n.textContent.trim())) out.tiny.push({ el: desc(el), fontSize: fs });
  }
  const dedupe = (arr, key) => { const seen = new Set(); return arr.filter((x) => { const k = key(x); if (seen.has(k)) return false; seen.add(k); return true; }); };
  out.overflow = dedupe(out.overflow, (x) => x.el).slice(0, 12);
  out.clipped = dedupe(out.clipped, (x) => x.el).slice(0, 12);
  out.small = dedupe(out.small, (x) => x.el).slice(0, 25);
  out.tiny = dedupe(out.tiny, (x) => x.el + x.fontSize).slice(0, 15);
  out.lines = dedupe(out.lines, (x) => x.el).slice(0, 12);
  out.stretched = dedupe(out.stretched, (x) => x.el).slice(0, 12);
  out.blurry = dedupe(out.blurry, (x) => x.el).slice(0, 12);
  return out;
};
// press taps on a touch context and clicks elsewhere, so one screen list
// opens on phones and desktops alike.
const press = async (locator) => {
  if (locator.page().context()._options.hasTouch) return locator.tap();
  return locator.click();
};
const SCREENS = [
  { tag: 'login', path: '/login' },
  { tag: 'register', path: '/login?mode=register' },
  { tag: 'projects', path: '/projects' },
  { tag: 'projects-switcher', path: '/projects', open: async (p) => { const b = p.getByRole('button', { name: /Space|workspace/i }).first(); if (await b.count()) await press(b); } },
  { tag: 'projects-new', path: '/projects', open: async (p) => press(p.getByRole('button', { name: '+ New Project' }).first()) },
  // Random-product mode with a Top 5 leaderboard open: a vote control, a
  // segmented filter and a five-row list, all inside the create form.
  { tag: 'projects-random-top', path: '/projects', open: async (p) => { await press(p.getByRole('button', { name: '+ New Project' }).first()); await p.waitForTimeout(200); await p.selectOption('#mode', 'random'); await p.waitForTimeout(400); await press(p.getByRole('button', { name: 'Top 5 all time' })); await p.waitForTimeout(400); } },
  { tag: 'user-menu', path: '/projects', open: async (p) => { await press(p.locator('[title="Sam Example"]').first()); } },
  { tag: 'user-settings', path: '/projects', open: async (p) => { await press(p.locator('[title="Sam Example"]').first()); await p.waitForTimeout(300); await press(p.getByText('Settings', { exact: true }).first()); } },
  { tag: 'drawer-user-settings', phone: true, path: '/projects/p1', open: async (p) => { await press(p.getByRole('button', { name: 'Project menu' })); await p.waitForTimeout(300); await press(p.getByText('Sam Example').last()); } },
  { tag: 'overview', path: '/projects/p1' },
  { tag: 'drawer', phone: true, path: '/projects/p1', open: async (p) => press(p.getByRole('button', { name: 'Project menu' })) },
  { tag: 'notifications', path: '/projects/p1', open: async (p) => press(p.getByRole('button', { name: /notifications/i }).first()) },
  { tag: 'help', path: '/projects/p1', open: async (p) => press(p.getByRole('button', { name: 'Help' }).first()) },
  { tag: 'requirements', path: '/projects/p1/requirements' },
  { tag: 'requirements-doc', path: '/projects/p1/requirements', open: async (p) => { const ex = p.getByRole('button', { name: 'Expand all' }); if (await ex.count()) await press(ex.first()); await press(p.getByText('Positioning accuracy').first()); await p.waitForTimeout(600); } },
  { tag: 'requirements-new', path: '/projects/p1/requirements', open: async (p) => press(p.getByRole('button', { name: '+ New Artifact' })) },
  { tag: 'requirements-edit', path: '/projects/p1/requirements', open: async (p) => { const ex = p.getByRole('button', { name: 'Expand all' }); if (await ex.count()) await press(ex.first()); await press(p.getByText('Positioning accuracy').first()); await p.waitForTimeout(500); await press(p.getByRole('button', { name: 'Edit', exact: true }).first()); } },
  // On a phone the toolbar folds into an action sheet; a desktop has the
  // Download button in the toolbar itself.
  { tag: 'requirements-download', path: '/projects/p1/requirements', open: async (p) => { await press(p.getByRole('button', { name: 'Requirements actions' })); await press(p.getByRole('button', { name: /Download/ }).first()); }, openDesktop: async (p) => press(p.getByRole('button', { name: /Download/ }).first()) },
  { tag: 'requirements-menu', path: '/projects/p1/requirements', open: async (p) => { const ex = p.getByRole('button', { name: 'Expand all' }); if (await ex.count()) await press(ex.first()); await press(p.getByRole('button', { name: /Actions for Positioning/ })); } },
  { tag: 'baseline-compare', path: '/projects/p1/baselines/b1/compare' },
  { tag: 'guided', path: '/projects/p1/guided' },
  { tag: 'guided-assistant', path: '/projects/p1/guided', open: async (p) => { const b = p.getByRole('button', { name: /assistant/i }).first(); if (await b.count()) await press(b); } },
  { tag: 'interviews', path: '/projects/p1/interviews' },
  { tag: 'vv', path: '/projects/p1/vv' },
  { tag: 'test-run', path: '/projects/p1/vv/runs/tr1' },
  { tag: 'matrix', path: '/projects/p1/matrix' },
  { tag: 'impact', path: '/projects/p1/impact?artifact=req-2' },
  { tag: 'review', path: '/projects/p1/review' },
  { tag: 'board', path: '/projects/p1/board' },
  { tag: 'board-card', path: '/projects/p1/board', open: async (p) => { await press(p.getByText('Write the enclosure interlock').first()); } },
  { tag: 'crew', path: '/projects/p1/crew' },
  { tag: 'crew-network', path: '/projects/p1/crew/network' },
  { tag: 'automations', path: '/projects/p1/automations' },
  { tag: 'automations-new', path: '/projects/p1/automations', open: async (p) => { const b = p.getByRole('button', { name: /New automation|\+ New/i }).first(); if (await b.count()) await press(b); } },
  { tag: 'agent-runs', path: '/projects/p1/agent-runs' },
  { tag: 'agent-run-detail', path: '/projects/p1/agent-runs', open: async (p) => { const b = p.getByText('Review the safety requirements').first(); if (await b.count()) await press(b); } },
  { tag: 'agents', path: '/projects/p1/agents' },
  { tag: 'agents-edit', path: '/projects/p1/agents', open: async (p) => { const b = p.getByText('Requirements Analyst').first(); if (await b.count()) await press(b); } },
  { tag: 'activity', path: '/projects/p1/activity' },
  { tag: 'settings', path: '/projects/p1/settings' },
  { tag: 'settings-agents', path: '/projects/p1/settings?tab=agents' },
  { tag: 'settings-access', path: '/projects/p1/settings?tab=access' },
  { tag: 'org-settings', path: '/org/settings' },
  { tag: 'org-members', path: '/org/settings', open: async (p) => { const t = p.getByRole('tab', { name: /Members/ }); if (await t.count()) await press(t); } },
  { tag: 'org-runners', path: '/org/settings', open: async (p) => { const t = p.getByRole('tab', { name: /Runners/ }); if (await t.count()) await press(t); } },
  { tag: 'org-usage', path: '/org/settings', open: async (p) => { const t = p.getByRole('tab', { name: /Usage|Budget/ }); if (await t.count()) await press(t); } },
  { tag: 'org-quality', path: '/org/settings', open: async (p) => { const t = p.getByRole('tab', { name: /Quality/ }); if (await t.count()) await press(t); } },
  { tag: 'manual', path: '/manual' },
  { tag: 'landing', path: '/' },
];
// phoneDevice is the Pixel 5 profile, or the exact viewport SHOT_SIZE names.
const phoneDevice = () => {
  const raw = process.env.SHOT_SIZE;
  if (!raw) return devices['Pixel 5'];
  const m = /^(\d+)x(\d+)(?:@(\d+(?:\.\d+)?))?$/.exec(raw.trim());
  if (!m) { console.error(`SHOT_SIZE must look like 360x640@3, got ${raw}`); process.exit(2); }
  return {
    ...devices['Pixel 5'],
    viewport: { width: Number(m[1]), height: Number(m[2]) },
    deviceScaleFactor: m[3] ? Number(m[3]) : 1,
  };
};

async function runEngine(engine, device, name, desktop = false) {
  const browser = await engine.launch(
    engine === chromium && process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH } : {}
  );
  const report = [];
  const only = (process.env.TAGS || '').split(',').filter(Boolean);
  for (const s of SCREENS) {
    if (only.length && !only.includes(s.tag)) continue;
    // Phone-only screens (the drawer) have no desktop counterpart.
    if (desktop && s.phone) continue;
    const open = desktop && s.openDesktop ? s.openDesktop : s.open;
    // A fresh context per screen: Chromium flips its hover/pointer media
    // flags after a synthetic mouse event, which real phones never see.
    const ctx = await browser.newContext({ ...device, baseURL: BASE_URL, serviceWorkers: 'block' });
    const page = await ctx.newPage();
    const errors = [];
    page.on('pageerror', (e) => errors.push(String(e).split('\n')[0]));
    await mock(page);
    let openErr = '';
    try {
      await page.goto(s.path); await page.waitForTimeout(900);
      if (open) { try { await open(page); } catch (e) { openErr = String(e).split('\n')[0].slice(0, 120); } await page.waitForTimeout(500); }
      const audit = await page.evaluate(AUDIT, { desktop });
      await page.screenshot({ path: `${OUT}/${name}-${s.tag}.png`, fullPage: process.env.SHOT_FULLPAGE !== '0' });
      report.push({ tag: s.tag, url: page.url().replace(BASE_URL, ''), openErr, errors, ...audit });
    } catch (e) {
      report.push({ tag: s.tag, fatal: String(e).split('\n')[0].slice(0, 160) });
    }
    await ctx.close();
  }
  await browser.close();
  return report;
}
// Desktop profiles: the common laptop panels, the Windows 125 % scaling
// size, full HD, 1440p, 4K at its native 2x scale, and two HiDPI variants
// that catch rasters drawn larger than their pixels.
const DESKTOP_PROFILES = {
  'laptop-1024': { width: 1024, height: 768, scale: 1 },
  'laptop-1280': { width: 1280, height: 800, scale: 1 },
  'laptop-1366': { width: 1366, height: 768, scale: 1 },
  'laptop-1440': { width: 1440, height: 900, scale: 1 },
  'win125-1536': { width: 1536, height: 864, scale: 1 },
  'fhd-1920': { width: 1920, height: 1080, scale: 1 },
  'qhd-2560': { width: 2560, height: 1440, scale: 1 },
  'uhd-3840': { width: 1920, height: 1080, scale: 2 }, // 3840x2160 panel at 200 %
  'hidpi-1440': { width: 1440, height: 900, scale: 2 },
  'hidpi-1920': { width: 1920, height: 1080, scale: 2 },
};
const desktopDevice = (p) => ({
  ...devices['Desktop Chrome'],
  viewport: { width: p.width, height: p.height },
  deviceScaleFactor: p.scale,
});
(async () => {
  const which = (process.env.ENGINES || 'android').split(',').map((s) => s.trim()).filter(Boolean);
  const all = {};
  if (which.includes('android')) all.android = await runEngine(chromium, phoneDevice(), 'android');
  if (which.includes('iphone')) all.iphone = await runEngine(webkit, devices['iPhone 13'], 'iphone');
  const desktopArg = which.find((w) => w === 'desktop' || w.startsWith('desktop:'));
  if (desktopArg) {
    const wanted = desktopArg.includes(':') ? desktopArg.slice('desktop:'.length).split('+').filter(Boolean) : Object.keys(DESKTOP_PROFILES);
    for (const name of wanted) {
      const profile = DESKTOP_PROFILES[name];
      if (!profile) { console.error(`unknown desktop profile ${name}; known: ${Object.keys(DESKTOP_PROFILES).join(', ')}`); process.exit(2); }
      all[name] = await runEngine(chromium, desktopDevice(profile), name, true);
    }
  }
  fs.writeFileSync(`${OUT}/audit.json`, JSON.stringify(all, null, 1));
  let failed = false;
  for (const [eng, rep] of Object.entries(all)) {
    for (const r of rep) {
      const flags = [];
      if (r.fatal) { flags.push('FATAL ' + r.fatal); failed = true; }
      if (r.scrollWidth > r.vw) flags.push(`PAGE-OVERFLOW ${r.scrollWidth}>${r.vw}`);
      if (r.overflow && r.overflow.length) flags.push(`overflow:${r.overflow.length}`);
      if (r.clipped && r.clipped.length) flags.push(`clipped:${r.clipped.length}`);
      if (r.small && r.small.length) flags.push(`small:${r.small.length}`);
      if (r.tiny && r.tiny.length) flags.push(`tiny:${r.tiny.length}`);
      if (r.lines && r.lines.length) flags.push(`lines:${r.lines.length}`);
      if (r.stretched && r.stretched.length) flags.push(`stretched:${r.stretched.length}`);
      if (r.blurry && r.blurry.length) flags.push(`blurry:${r.blurry.length}`);
      if (r.errors && r.errors.length) { flags.push(`errors:${r.errors.length}`); failed = true; }
      if (r.openErr) flags.push('open-failed');
      console.log(`${eng} ${r.tag.padEnd(22)} ${flags.join(' ') || 'ok'}`);
    }
  }
  console.log(`report: ${OUT}/audit.json`);
  if (failed) process.exit(1);
})().catch((e) => { console.error(e); process.exit(1); });
