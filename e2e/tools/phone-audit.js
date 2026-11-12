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
//   - page errors,
//   - how much of the window is chrome: the height above the element a page
//     marks data-page-content (where its own content starts) plus the tab
//     bar below, in pixels and as a share of the window. A page with no
//     marker is measured from the shell's <main>. Printed beside the
//     screen's flags; a screen over CHROME_BUDGET percent of the window
//     (default 35) fails the run.
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
//   CHROME_BUDGET  the most of the window chrome may take, in percent (default 35)
//   CHROMIUM_PATH  executablePath override for Chromium
//   SHOT_SIZE      exact-size screenshot mode for the PWA manifest's
//                  screenshots, which must declare fixed pixel dimensions:
//                  <cssWidth>x<cssHeight>@<dpr> replaces the phone profile's
//                  viewport (e.g. 360x640@3 => 1080x1920 PNGs)
//   COLOR_SCHEME   dark renders every screen in the dark scheme (default light)
//   SHOT_FULLPAGE  0 clips each screenshot to the viewport instead of the
//                  full page, which is what a fixed size needs
// Exit code is 1 when a screen fails to render or throws a page error;
// layout findings are reported, not failed, because some (ag-grid's
// virtualised columns, deliberate scrollers) are expected.
const { chromium, webkit, devices } = require('playwright');
const fs = require('fs');
const path = require('path');
const OUT = process.env.OUT || path.join(__dirname, '..', 'test-results', 'phone-audit');
const CHROME_BUDGET = Number(process.env.CHROME_BUDGET) || 35;
const BASE_URL = process.env.BASE_URL || 'http://127.0.0.1:5000';
const COLOR_SCHEME = process.env.COLOR_SCHEME === 'dark' ? 'dark' : 'light';
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
const run = (id, status) => ({ id, org_id: 'org1', project_id: 'p1', agent_id: 'a1', agent_name: 'Requirements Analyst', status, tokens_in: 12000, tokens_out: 800, cost_usd: 0.12, prompt: 'Review the safety requirements for testability and propose fixes where the wording is weak.', final_text: status === 'succeeded' ? 'Reviewed 6 requirements; proposed 2 rewrites.' : '', error: status === 'failed' ? 'runner lost' : '', launched_by: 'u1', launched_by_name: 'Sam Example', created_at: now, updated_at: now, started_at: now, finished_at: now, usage: { input_tokens: 12000, output_tokens: 800, cost_usd: 0.12 } });
const runs = [run('r1', 'succeeded'), run('r2', 'running'), run('r3', 'failed')];
const workItems = [
  { id: 'w1', project_id: 'p1', title: 'Write the enclosure interlock test procedure', description: 'Cover door open at speed.', column: 'backlog', sort_order: 0, assignee_type: 'user', assignee_id: 'u1', artifact_ids: ['req-2'], created_at: now, updated_at: now },
  { id: 'w2', project_id: 'p1', title: 'Draft spindle requirements', description: '', column: 'in-progress', sort_order: 0, assignee_type: 'agent', assignee_id: 'a1', agent_run_id: 'r2', artifact_ids: [], created_at: now, updated_at: now },
  { id: 'w3', project_id: 'p1', title: 'Review noise requirement', description: '', column: 'review', sort_order: 0, assignee_type: 'user', assignee_id: null, artifact_ids: ['req-1'], created_at: now, updated_at: now },
];
// A leased cloud runner, and the same payload with the pool free: the runner
// card is the phone's lease control (REQ-108).
const runnerPool = { total: 3, idle: 2, leased: 1 };
const runnerSessionIdle = { enabled: true, session: null, pool: runnerPool };
const runnerSessionLeased = {
  enabled: true,
  session: { id: 'rs1', org_id: 'org1', user_id: 'u1', status: 'active', idle_minutes: 20, created_at: now },
  // Real time, not the frozen fixture clock: the card counts down to it.
  deadline: new Date(Date.now() + 23 * 60 * 1000).toISOString(),
  seconds_remaining: 1380,
  pool: runnerPool,
};
// A relayed vendor sign-in waiting for the code the member pastes back.
const providerLogin = {
  id: 'pl1',
  provider: 'claude-code',
  target: 'user',
  status: 'awaiting_code',
  auth_url: 'https://claude.ai/oauth/authorize?client_id=openv&redirect_uri=http%3A%2F%2F127.0.0.1%3A45123%2Fcallback&state=abc123',
  detail: 'Open the sign-in link, authorize, then paste the code you are given back here.',
  created_at: now,
  updated_at: now,
};
const notifications = [
  { id: 'n1', user_id: 'u1', type: 'proposal_pending', title: 'Proposal awaiting review', body: 'Requirements Analyst proposed a change to REQ-2 Positioning accuracy.', read: false, link: '/projects/p1/review', created_at: now },
  { id: 'n2', user_id: 'u1', type: 'run_failed', title: 'Run failed', body: 'Developer run r3 failed: runner lost.', read: true, link: '/projects/p1/agent-runs', created_at: now },
];
// tr2 is the same campaign still in progress: a completed run is read-only,
// so its evidence picker is deliberately unreachable and cannot be audited.
const openTestRun = { id: 'tr2', project_id: 'p1', name: 'Accuracy survey September', description: 'Ballbar at three heights', status: 'in-progress', started_at: now, created_at: now, updated_at: now };
const evidenceFile = { id: 'evf1', bundle_id: 'ev1', filename: 'sweep-20260910.wav', mime_type: 'audio/wav', file_size: 48000000, sha256: 'a1b2c3', created_at: now };
const evidenceCitation = { id: 'evc1', bundle_id: 'ev1', test_result_id: 'res1', note: 'channel 2 from 00:12', created_at: now, bundle_ref: 'EVD-1', bundle_title: 'Noise sweep, 90 minutes, all load conditions', test_case_id: 'tc-1', test_case_title: 'Ballbar circularity', test_case_ref: 'TC-1', run_id: 'tr1', run_name: 'Design verification rev B' };
const evidenceBundle = { id: 'ev1', project_id: 'p1', ref: 'EVD-1', title: 'Noise sweep, 90 minutes, all load conditions', summary: 'One continuous capture covering idle, half and full load on the anechoic rig.', captured_at: now, captured_by: 'J. Patel, acoustics lab', conditions: { rig: 'anechoic chamber 2', ambient: '21.5 C', calibrated: '2026-09-01' }, created_at: now, updated_at: now, file_count: 1, total_size: 48000000, files: [evidenceFile], citations: [evidenceCitation] };
const testRun = { id: 'tr1', project_id: 'p1', name: 'Accuracy survey September', description: 'Ballbar at three heights', status: 'completed', started_at: now, completed_at: now, created_at: now, updated_at: now };
// Carries an author, so the picker renders the longest label it can:
// name, date and who captured it, all inside one <option>.
const baseline = { id: 'b1', project_id: 'p1', name: 'Release candidate 1', created_at: now,
  created_by: 'u1', created_by_name: 'Sam Example' };
const interview = { id: 'i1', project_id: 'p1', title: 'Lab manager interview', description: 'Safety and noise', status: 'open', invite_token: 'tok', created_at: now, updated_at: now };
const automation = { id: 'au1', org_id: 'org1', project_id: 'p1', name: 'Nightly requirements lint', agent_id: 'a1', kind: 'cron', enabled: true, prompt_template: 'Lint everything', cron_expr: '0 2 * * *', catch_up: false, event_type: '', event_filter: {}, cooldown_seconds: 0, max_runs_per_hour: 0, created_at: now, updated_at: now };
const usage = { window_days: 30, totals: { runs: 12, input_tokens: 240000, output_tokens: 18000, cost_usd: 3.4 }, month_to_date_usd: 3.4, monthly_budget_usd: 20, by_agent: [{ agent_id: 'a1', agent_name: 'Requirements Analyst', runs: 8, input_tokens: 180000, output_tokens: 12000, cost_usd: 2.6 }], by_day: [{ day: '2026-09-01', runs: 3, cost_usd: 0.9 }] };
const coverageOld = { summary: { total_requirements: 3, verified: 1, covered: 2, uncovered: 1, coverage_pct: 66.7 }, requirements: [{ id: 'req-1', ref: 'REQ-1', title: 'Work envelope', status: 'verified', test_cases: [{ id: 'tc-1', ref: 'TC-1', title: 'Accuracy survey with a ballbar', last_result: 'pass' }] }, { id: 'req-2', ref: 'REQ-2', title: 'Positioning accuracy', status: 'uncovered', test_cases: [] }] };
// The community pool behind the new-project wizard's random mode, with vote
// counts so the Top 5 filters have something to lay out.
const sharedProduct = (id, name, votes, votesWeek) => ({ id, category: 'kitchen appliance', name, description: `${name} recognises Kevin and locks itself until he owns up.`, vision: `${name} becomes the reason the office bean jar survives a Tuesday.`, problem: 'Beans vanish overnight and nobody admits to owning the grinder.', target_users: 'coffee-obsessed office workers whose beans keep leaving with Kevin', votes, votes_week: votesWeek, voted: false });
const sharedProducts = [sharedProduct('sp1', 'Kevinproof', 12, 5), sharedProduct('sp2', 'Crustodian', 9, 4), sharedProduct('sp3', 'Loafwatch', 7, 2), sharedProduct('sp4', 'Mugshot Registry', 4, 1), sharedProduct('sp5', 'Fridge Amnesty Box', 2, 1)];
// The redesign pages (Home, Design, Tests, Baselines): design items in a
// tree, more test cases with results in two runs, events, a proposal and a
// second baseline with an older text, so every part of each page lays out.
const rdArtifacts = [
  ...artifacts,
  art('des-1', 'design-item', 'Machine frame', 'A cast-iron frame carries the three axes and the spindle; it sets the work envelope and the stiffness the accuracy depends on.', null, 'approved', { material: 'EN-GJL-250 cast iron', mass_kg: '38', owner: 'Sam Example' }),
  art('des-2', 'design-item', 'Motion system', 'Three ball-screw axes on linear rails.', 'des-1', 'approved'),
  art('des-3', 'design-item', 'X axis drive', 'A 12 mm ball screw driven by a closed-loop NEMA 23 stepper.', 'des-2', 'in_review', { supplier_part: 'BSF1204-C7', lead_mm: '4' }),
  art('des-4', 'design-item', 'Y axis drive', '', 'des-2'),
  art('des-5', 'design-item', 'Z axis drive', '', 'des-2'),
  art('des-6', 'design-item', 'Spindle', '', 'des-1'),
  art('des-7', 'design-item', 'Enclosure and interlock', '', null),
  art('tc-2', 'test-case', 'Repeatability over 50 moves', '1. Home all axes\n2. Move to the target 50 times\n3. Measure with a dial gauge', 'hdg-4', 'approved', { execution_method: 'physical' }),
  art('tc-3', 'test-case', 'Envelope travel limits', '1. Jog each axis to its limits\n2. Record the travel', 'hdg-4', 'approved', { execution_method: 'manual' }),
  art('tc-4', 'test-case', 'Controller step-loss detection', '', 'hdg-4'),
];
const rdLinks = [
  ...links,
  link('l6', 'des-1', 'req-1', 'satisfies'),
  link('l7', 'des-1', 'req-2', 'satisfies'),
  link('l8', 'tc-2', 'req-2', 'verifies'),
  link('l9', 'tc-3', 'req-1', 'verifies'),
  link('l10', 'tc-4', 'req-2', 'verifies'),
];
const rdCoverage = { project_id: 'p1', entries: [
  { requirement_id: 'req-1', title: 'Work envelope of at least 300 by 200 by 100 millimetres', verification_method: 'inspection', verification_status: 'verified', test_case_ids: ['tc-3'], latest_results: { 'tc-3': 'pass' }, rollup: 'pass' },
  { requirement_id: 'req-2', title: 'Positioning accuracy', verification_method: 'test', verification_status: '', test_case_ids: ['tc-1', 'tc-2', 'tc-4'], latest_results: { 'tc-1': 'pass', 'tc-2': 'fail', 'tc-4': 'blocked' }, rollup: 'fail' },
], summary: { total: 2, covered: 2, uncovered: 0, verified: 1, pass: 1, fail: 1 } };
const rdResult = (id, run, tc, status) => ({ id, run_id: run, test_case_id: tc, test_case_version: 2, status, notes: '', evidence: [], executed_at: now });
const rdEvent = (id, type, who, entity, payload, kind) => ({ id, event_type: type, project_id: 'p1', actor: `user:${who}`, actor_kind: kind || 'user', actor_name: who, entity_name: entity, payload, created_at: now });
const rdBaseline0 = { id: 'b0', project_id: 'p1', name: 'Concept review', created_at: '2026-08-02T09:00:00Z', created_by: 'u1', created_by_name: 'Sam Example' };
const rdRoutes = [
  [/\/api\/v1\/artifacts\?/, rdArtifacts],
  [/\/api\/v1\/links/, rdLinks],
  [/\/api\/v1\/projects\/p1\/vv\/coverage/, rdCoverage],
  [/\/api\/v1\/projects\/p1\/test-runs/, [openTestRun, testRun]],
  [/\/api\/v1\/test-runs\/tr2\/results/, [rdResult('x1', 'tr2', 'tc-1', 'pass'), rdResult('x2', 'tr2', 'tc-2', 'fail')]],
  [/\/api\/v1\/test-runs\/tr1\/results/, [rdResult('x3', 'tr1', 'tc-1', 'pass'), rdResult('x4', 'tr1', 'tc-3', 'pass'), rdResult('x5', 'tr1', 'tc-4', 'blocked')]],
  [/\/api\/v1\/projects\/p1\/review-queue/, { suspect_links: [{ id: 'l2', type: 'derives-from', from_id: 'req-2', from_title: 'Positioning accuracy', from_type: 'requirement', to_id: 'need-1', to_title: 'Cut aluminium parts without a machinist', to_type: 'user-need', updated_at: now }], in_review_artifacts: [byId['req-2']] }],
  [/\/api\/v1\/events\?.*chatter/, [rdEvent('c1', 'chatter.created', 'Priya Shah', 'REQ-1', { artifact_id: 'req-1', entry_type: 'comment', message: 'Does the envelope include the vice?' })]],
  [/\/api\/v1\/events/, [
    rdEvent('e1', 'proposal.created', 'Requirements Analyst', 'REQ-2', { op: 'update_artifact' }, 'agent'),
    rdEvent('e2', 'testrun.recorded', 'Sam Example', 'TC-2', { status: 'fail' }),
    rdEvent('e3', 'artifact.status_changed', 'Sam Example', 'REQ-1', { to: 'approved' }),
    rdEvent('e4', 'artifact.updated', 'Priya Shah', 'REQ-2', {}),
    rdEvent('e5', 'baseline.captured', 'Sam Example', 'Release candidate 1', { name: 'Release candidate 1' }),
  ]],
  [/\/api\/v1\/proposals/, [{ id: 'pr1', project_id: 'p1', run_id: 'r1', op: 'update_artifact', target_id: 'req-2', payload: { title: 'Positioning accuracy' }, status: 'pending', review_note: '', created_at: now }, { id: 'pr2', project_id: 'p1', run_id: 'r1', op: 'create_artifact', payload: { title: 'Spindle runout test' }, status: 'pending', review_note: '', created_at: now }]],
  [/\/api\/v1\/projects\/p1\/baselines/, [baseline, rdBaseline0]],
  [/\/api\/v1\/baselines\/[^/]+\/diff/, { base: { id: 'b1', name: 'Release candidate 1' }, target: { id: 'live', name: 'Live project' },
    added: [{ id: 'tc-2', type: 'test-case', title: 'Repeatability over 50 moves' }, { id: 'des-3', type: 'design-item', title: 'X axis drive' }],
    removed: [{ id: 'req-9', type: 'requirement', title: 'Manual lubrication schedule' }],
    modified: [
      { id: 'req-2', type: 'requirement', old_title: 'Positioning accuracy', new_title: 'Positioning accuracy', title_changed: false, body_changed: true, type_changed: false, status_changed: true, parent_changed: false },
      { id: 'req-1', type: 'requirement', old_title: 'Work envelope', new_title: 'Work envelope of at least 300 by 200 by 100 millimetres', title_changed: true, body_changed: false, type_changed: false, status_changed: false, parent_changed: false },
    ],
    links_added: [{ from_id: 'tc-2', to_id: 'req-2', type: 'verifies', from_title: 'Repeatability over 50 moves', to_title: 'Positioning accuracy' }], links_removed: [] }],
  [/\/api\/v1\/baselines\/[^/]+$/, { artifacts: [
    ...artifacts.filter((a) => a.id !== 'req-2' && a.id !== 'req-1'),
    { ...byId['req-1'], title: 'Work envelope' },
    { ...byId['req-2'], body: 'The mill shall position the tool within ±0.05 mm of the commanded position at 20 °C.' },
    art('req-9', 'requirement', 'Manual lubrication schedule', 'The mill shall need lubrication by hand once a week.', 'hdg-2'),
  ], links: [] }],
];
const routes = [
  [/\/api\/v1\/shared-products/, (url) => (/sort=top/.test(url) ? sharedProducts.slice(0, 5) : sharedProducts)],
  [/\/api\/v1\/auth\/me/, user],
  [/\/api\/v1\/auth\/config/, { google_enabled: false, oidc_enabled: false }],
  // What's new: a numbered release with two of the three groups filled, and
  // one of the dated releases from before OpenV had version numbers, which
  // has no groups at all.
  [/\/api\/v1\/release/, {
    version: '0.2.0', date: '2026-09-14',
    notes: ['Export a traceability matrix to Excel from the V&V tab.', 'Baselines load again on a large project.'],
    categories: [
      { name: 'New features', notes: ['Export a traceability matrix to Excel from the V&V tab.'] },
      { name: 'Bug fixes', notes: ['Baselines load again on a large project.'] },
    ],
    markdown: '',
    releases: [
      {
        version: '0.2.0', date: '2026-09-14',
        notes: ['Export a traceability matrix to Excel from the V&V tab.', 'Baselines load again on a large project.'],
        categories: [
          { name: 'New features', notes: ['Export a traceability matrix to Excel from the V&V tab.'] },
          { name: 'Bug fixes', notes: ['Baselines load again on a large project.'] },
        ],
        markdown: '',
      },
      {
        version: '2026-09-12', date: '2026-09-12',
        notes: ['Upload your own profile picture from Personal settings. It replaces the picture your sign-in provider supplied and stays put across sign-ins.'],
        categories: [{ name: 'Changes', notes: ['Upload your own profile picture from Personal settings. It replaces the picture your sign-in provider supplied and stays put across sign-ins.'] }],
        markdown: '',
      },
    ],
  }],
  [/\/api\/v1\/orgs$/, { orgs: [org, org2], active_org: 'org1' }],
  [/\/api\/v1\/orgs\/[^/]+\/usage/, usage],
  [/\/api\/v1\/orgs\/[^/]+\/limits/, {
    org_id: 'org1', plan: 'single', self_hosted: false,
    limits: [
      // The personal-workspace shape: a full bar that is a fact rather than a
      // warning, with the longest description in the panel.
      { key: 'max_members', label: 'Workspace members', description: 'A personal workspace is only ever you. Create a shared workspace to work with other people.', unit: 'count', limit: 1, unlimited: false, used: 1, fixed: true },
      { key: 'max_projects', label: 'Projects', description: 'How many projects this workspace can hold.', unit: 'count', limit: 10, unlimited: false, used: 9 },
      { key: 'max_shared_workspaces', label: 'Shared workspaces', description: 'How many shared workspaces you can create. Your personal workspace is never counted.', unit: 'count', limit: 0, unlimited: true, used: 2 },
      { key: 'evidence_storage_mb', label: 'Test evidence storage', description: 'Total size of the test evidence files this workspace has uploaded.', unit: 'mb', limit: 2048, unlimited: false, used: 2048 },
      { key: 'runner_session_minutes', label: 'Cloud runner lease', description: 'How long a leased cloud runner lasts before it is reclaimed.', unit: 'minutes', limit: 60, unlimited: false },
      { key: 'runner_memory_mb', label: 'Hosted runner memory', description: "Memory available to this workspace's always-on hosted runner.", unit: 'mb', limit: 2048, unlimited: false },
    ],
  }],
  [/\/api\/v1\/orgs\/[^/]+\/worker-status/, { workers: [{ id: 'wk1', name: 'Sam laptop', personal: true, hosted: false, user_name: 'Sam Example', online: true, revoked: false, last_used_at: now }], queue: { queued: 1, oldest_queued_seconds: 40, queued_repo_access: 0 } }],
  [/\/api\/v1\/projects\/p1\/download\/options/, { sections: [{ id: 'hdg-2', ref: 'HDG-2', number: '2', title: 'Functional requirements', artifacts: 2 }], types: [{ type: 'requirement', count: 2 }, { type: 'user-need', count: 1 }], attachments: [] }],
  [/\/api\/v1\/baselines\/b1\/diff/, { base: { id: 'b1', name: 'Release candidate 1' }, target: { id: 'live', name: 'Live project' }, added: [{ id: 'req-1', type: 'requirement', title: 'Work envelope' }], removed: [], modified: [{ id: 'req-2', type: 'requirement', old_title: 'Positioning accuracy', new_title: 'Positioning accuracy (tightened)', title_changed: true, body_changed: true, type_changed: false, status_changed: false, parent_changed: false }], links_added: [], links_removed: [] }],
  // Hosted runners on, none provisioned: the Runners tab draws the
  // admin's provider-key enable form.
  [/\/api\/v1\/orgs\/[^/]+\/hosted-runner/, { enabled: true, online: false, container_state: '', record: null }],
  [/\/api\/v1\/orgs\/[^/]+\/runner-session/, runnerSessionIdle],
  [/\/api\/v1\/orgs\/[^/]+\/members/, [{ user_id: 'u1', email: user.email, name: user.name, role: 'admin', joined_at: now }]],
  [/\/api\/v1\/orgs\/[^/]+\/teams/, []],
  [/\/api\/v1\/orgs\/[^/]+\/runners/, []],
  [/\/api\/v1\/orgs\/[^/]+\/worker-keys/, []],
  [/\/api\/v1\/orgs\/[^/]+\/provider-keys/, []],
  [/\/api\/v1\/orgs\/[^/]+\/quality-rules/, { effective: { convention: 'shall', severities: {} }, workspace: null, project: null, summary: 'shall', catalog: { conventions: ['shall', 'rfc2119'], rules: ['weak-word', 'vague-quantifier', 'placeholder', 'passive-voice', 'long-sentence', 'not-testable'], severities: ['error', 'warning', 'info', 'off'], defaults: { convention: 'shall', severities: { placeholder: 'error', 'passive-voice': 'info', 'long-sentence': 'off' } }, labels: { shall: 'The system shall …', rfc2119: 'MUST, SHOULD, MAY', 'weak-word': 'Words like “appropriate” or “user-friendly” that cannot be checked.', 'vague-quantifier': 'Quantities like “some” or “several” with no number.', placeholder: 'TBD, TODO or XXX left in the text.', 'passive-voice': 'An obligation with no one named to meet it.', 'long-sentence': 'A sentence too long to read as one obligation.', 'not-testable': 'Nothing a test could measure.' } } }],
  // The workspace's feature gates, ahead of the org catch-all below, which
  // would otherwise answer {} and crash every useFeature on the page. All
  // on, so gated cards and controls render with their buttons.
  [/\/api\/v1\/orgs\/[^/]+\/features/, { channel: 'nightly', stable_release: '', preview: false, features: { 'flow-down': true, 'artifact-owners': true, 'assistant-project-edits': true, 'default-workspace': true, 'figure-titles': true, 'artifact-stepping': true, 'workspace-runs': true, 'compact-layout': true, 'target-releases': true, 'redesign-pages': true, 'review-queue-decisions': true, 'project-review-round': true, 'editor-pickers': true, 'gantt-plan': true, 'floating-assistant': true, 'ui-refresh': true, 'chip-search': true, 'requirements-tree': true } }],
  [/\/api\/v1\/orgs\/org1$/, org],
  [/\/api\/v1\/orgs\//, {}],
  [/\/api\/v1\/meta\/artifact-types/, [{ type: 'requirement', label: 'Requirement' }, { type: 'heading', label: 'Heading' }, { type: 'user-need', label: 'User Need' }, { type: 'test-case', label: 'Test Case' }]],
  [/\/api\/v1\/meta\/link-types/, []],
  [/\/api\/v1\/meta\/attribute-definitions/, []],
  [/\/api\/v1\/attribute-definitions/, []],
  [/\/api\/v1\/templates/, []],
  // A session with a transcript, so the assistant's chat renders something.
  // The reply is deliberately markdown — bold, a list, inline code and an
  // arrow — because that is what the model writes and the panel has to show
  // it as formatting rather than as literal asterisks.
  [/\/api\/v1\/guided-sessions\/gs1\/messages/, [
    { id: 'gm1', session_id: 'gs1', role: 'user', content: 'What headings should the personas sit under?', created_at: now },
    { id: 'gm2', session_id: 'gs1', role: 'assistant', created_at: now, content:
      'The three needs you have are already parented correctly — from the live project they map one-to-one:\n\n' +
      '- **PER-1 Chief Engineer** → NEED-2 (drop-in replacement)\n' +
      '- **PER-2 Commercial** → NEED-1 (source from more than one vendor)\n' +
      '- **PER-3 Customer** → NEED-3 (run a Fanuc robot)\n\n' +
      'Two things before you start adding:\n\n' +
      '1. I cannot re-parent needs myself — tell me the new needs per persona and I will draft each as a `need`.\n' +
      '2. Keep the wording testable.\n' },
    { id: 'gm3', session_id: 'gs1', role: 'user', content: 'Tighten REQ-2, move it under the accuracy heading, and add a test for it.', created_at: now },
    // Beside the project the assistant proposes changes as cards: an edit,
    // a move and a new artifact of a type the wizard never had.
    { id: 'gm4', session_id: 'gs1', role: 'assistant', created_at: now, content:
      'Three changes, applied in order:\n\n' +
      '```openv-suggestion\n{"kind":"edit","ref":"REQ-2","body":"The system shall position the tool within ±0.02 mm of the commanded point across the full work envelope.","attributes":{"verification_method":"test"}}\n```\n' +
      '```openv-suggestion\n{"kind":"move","ref":"REQ-2","parent":"HDG-2","position":"first"}\n```\n' +
      '```openv-suggestion\n{"kind":"artifact","type":"test-case","title":"Ballbar accuracy sweep at three feed rates","body":"1. Mount the ballbar.\\n2. Run the sweep at 500, 1500 and 3000 mm/min.\\n3. Record the radial error.","attributes":{"execution_method":"physical"},"parent":"HDG-3","after":"TC-1"}\n```\n' +
      'Say the word and I will draft the evidence bundle the sweep should produce.' },
  ]],
  [/\/api\/v1\/guided-sessions/, [{ id: 'gs1', project_id: 'p1', status: 'in-progress', current_step: 1, answers: {}, created_at: now, updated_at: now }]],
  [/\/api\/v1\/projects\/p1\/guided/, []],
  [/\/api\/v1\/projects\/p1\/profile/, { product_name: 'Benchtop CNC Mill', one_liner: 'Desktop 3-axis mill', audience: 'Prototyping shops', problem: 'Waiting a week for the shop', solution: 'Mill on the desk', personas: [] }],
  [/\/api\/v1\/projects\/p1\/interviews/, [interview]],
  [/\/api\/v1\/projects\/p1\/interview-sessions/, []],
  [/\/api\/v1\/projects\/p1\/baselines\/b1\/(diff|compare)/, { added: [artifacts[3]], removed: [], modified: [{ before: artifacts[4], after: { ...artifacts[4], title: 'Positioning accuracy (tightened)' } }] }],
  [/\/api\/v1\/projects\/p1\/baselines/, [baseline]],
  [/\/api\/v1\/projects\/p1\/quality-rules/, { effective: { convention: 'shall', severities: {} }, workspace: null, project: null, summary: 'shall', catalog: { conventions: ['shall', 'rfc2119'], rules: ['weak-word', 'vague-quantifier', 'placeholder', 'passive-voice', 'long-sentence', 'not-testable'], severities: ['error', 'warning', 'info', 'off'], defaults: { convention: 'shall', severities: { placeholder: 'error', 'passive-voice': 'info', 'long-sentence': 'off' } }, labels: { shall: 'The system shall …', rfc2119: 'MUST, SHOULD, MAY', 'weak-word': 'Words like “appropriate” or “user-friendly” that cannot be checked.', 'vague-quantifier': 'Quantities like “some” or “several” with no number.', placeholder: 'TBD, TODO or XXX left in the text.', 'passive-voice': 'An obligation with no one named to meet it.', 'long-sentence': 'A sentence too long to read as one obligation.', 'not-testable': 'Nothing a test could measure.' } } }],
  [/\/api\/v1\/projects\/p1\/quality/, { entries: [] }],
  // Releases: MVP released, V1.0 in progress with one requirement carried
  // over and one introduced, and a burn-up of a few days.
  [/\/api\/v1\/projects\/[^/]+\/priority-detection/, { enabled: true }],
  [/\/api\/v1\/plan\/master/, { today: '2026-10-20', start: '2026-10-01', end: '2026-12-01', links: [], projects: [{ project_id: 'p1', name: 'Benchtop CNC Mill Product Definition', start: '2026-10-01', end: '2026-11-10', summary: { progress: 34, tasks: 4, done: 1, late: 1 }, milestones: [], tasks: [{ id: 'a', title: 'Order the spindle', wbs: '2', start: '2026-10-01', end: '2026-10-12', progress: 0, done: false, late: true, critical: false }, { id: 'b', title: 'Build the gantry', wbs: '1', start: '2026-10-05', end: '2026-11-10', progress: 67, done: false, late: false, critical: true }] }, { project_id: 'p2', name: 'Second project', start: '2026-10-15', end: '2026-12-01', summary: { progress: 10, tasks: 2, done: 0, late: 0 }, milestones: [], tasks: [{ id: 'c', title: 'Survey', wbs: '1', start: '2026-10-15', end: '2026-11-05', progress: 10, done: false, late: false, critical: false }, { id: 'd', title: 'Report', wbs: '2', start: '2026-10-20', end: '2026-12-01', progress: 0, done: false, late: false, critical: true }] }] }],
  [/\/api\/v1\/projects\/p1\/plan$/, { project_id: 'p1', working_days: [1, 2, 3, 4, 5], holidays: [], critical_slack_days: 0, scheduling_mode: 'manual', project_start: null, project_finish: null, schedule_from: 'start', multiple_critical_paths: false, currency: '', budget: null, status_date: null, active_baseline_id: null, status_day: '2026-10-20', baseline_id: null, baselines: [], earned_value: { bac: 0, pv: 0, ev: 0, ac: 0, sv: 0, cv: 0, spi: 0, cpi: 0, eac: 0 }, resources: [], people: [], today: '2026-10-20', start: '2026-10-01', end: '2026-12-01', tasks: [{ id: 'wi-1', title: 'Build the gantry', column: 'in-progress', assignee_type: 'user', assignee_id: null, parent_id: null, depth: 0, wbs: '1', start: null, end: null, span_start: '2026-10-01', span_end: '2026-11-10', duration_days: 10, effort_days: null, float_days: 5, critical: false, critical_subtasks: true, float_until: null, elapsed: false, deadline: null, deadline_missed: false, mode: 'manual', mode_own: '', constraint_type: 'asap', constraint_date: null, constraint_violated: false, early_start: null, early_finish: null, late_start: null, late_finish: null, free_float_days: null, assignments: [], fixed_cost: 0, cost: 0, effort_driven: false, overallocated: false, actual_start: null, actual_finish: null, percent_complete: null, actual_cost: null, baseline: null, start_variance_days: null, finish_variance_days: null, earned_value: null, behind_schedule: false, progress: 67, done: false, late: false, todos_done: 0, todos_total: 0, subtasks_done: 1, subtasks_total: 2, milestone: null, milestone_inherited: false, todos: [], depends_on: [], relationships: [], drives: [], conflicts: [], external_links: [] }, { id: 'wi-2', title: 'Machine the rails', column: 'done', assignee_type: 'user', assignee_id: null, parent_id: 'wi-1', depth: 1, wbs: '1.1', start: '2026-10-01', end: '2026-10-14', span_start: '2026-10-01', span_end: '2026-10-14', duration_days: 10, effort_days: null, float_days: 5, critical: true, critical_subtasks: false, float_until: null, elapsed: false, deadline: null, deadline_missed: false, mode: 'manual', mode_own: '', constraint_type: 'asap', constraint_date: null, constraint_violated: false, early_start: null, early_finish: null, late_start: null, late_finish: null, free_float_days: null, assignments: [], fixed_cost: 0, cost: 0, effort_driven: false, overallocated: false, actual_start: null, actual_finish: null, percent_complete: null, actual_cost: null, baseline: null, start_variance_days: null, finish_variance_days: null, earned_value: null, behind_schedule: false, progress: 100, done: true, late: false, todos_done: 0, todos_total: 0, subtasks_done: 0, subtasks_total: 0, milestone: null, milestone_inherited: false, todos: [], depends_on: [], relationships: [], drives: [], conflicts: [], external_links: [] }, { id: 'wi-3', title: 'Fit the linear bearings and square the gantry to the bed', column: 'todo', assignee_type: 'user', assignee_id: null, parent_id: 'wi-1', depth: 1, wbs: '1.2', start: '2026-10-15', end: '2026-11-10', span_start: '2026-10-15', span_end: '2026-11-10', duration_days: 10, effort_days: null, float_days: 5, critical: true, critical_subtasks: false, float_until: null, elapsed: false, deadline: null, deadline_missed: false, mode: 'manual', mode_own: '', constraint_type: 'asap', constraint_date: null, constraint_violated: false, early_start: null, early_finish: null, late_start: null, late_finish: null, free_float_days: null, assignments: [], fixed_cost: 0, cost: 0, effort_driven: false, overallocated: false, actual_start: null, actual_finish: null, percent_complete: null, actual_cost: null, baseline: null, start_variance_days: null, finish_variance_days: null, earned_value: null, behind_schedule: false, progress: 33, done: false, late: false, todos_done: 1, todos_total: 3, subtasks_done: 0, subtasks_total: 0, milestone: null, milestone_inherited: false, todos: [{ id: 'td-1', text: 'Shim the left rail', done: false, due_date: '2026-10-22' }, { id: 'td-2', text: 'Check squareness with the granite square', done: false, due_date: null }, { id: 'td-3', text: 'Clean the bearing blocks', done: true, due_date: null }], depends_on: ['wi-2'], relationships: [{ task_id: 'wi-2', type: 'FS', lag_days: 0, lag_unit: 'd', conflict: false }], drives: [], conflicts: [], external_links: [] }, { id: 'wi-4', title: 'Order the spindle', column: 'todo', assignee_type: 'user', assignee_id: null, parent_id: null, depth: 0, wbs: '2', start: null, end: '2026-10-10', span_start: '2026-10-10', span_end: '2026-10-10', duration_days: 10, effort_days: null, float_days: 5, critical: false, critical_subtasks: false, float_until: null, elapsed: false, deadline: null, deadline_missed: false, mode: 'manual', mode_own: '', constraint_type: 'asap', constraint_date: null, constraint_violated: false, early_start: null, early_finish: null, late_start: null, late_finish: null, free_float_days: null, assignments: [], fixed_cost: 0, cost: 0, effort_driven: false, overallocated: false, actual_start: null, actual_finish: null, percent_complete: null, actual_cost: null, baseline: null, start_variance_days: null, finish_variance_days: null, earned_value: null, behind_schedule: false, progress: 0, done: false, late: true, todos_done: 0, todos_total: 0, subtasks_done: 0, subtasks_total: 0, milestone: null, milestone_inherited: false, todos: [], depends_on: [], relationships: [], drives: [], conflicts: [], external_links: [] }], milestones: [{ key: 'ms-1', kind: 'custom', id: 'ms-1', release_id: null, name: 'Definition complete', description: '', date: '2026-10-15', default_criteria: false, criteria: [{ metric: 'needs_addressed', label: 'User needs addressed', target: 100, actual: 100, count: 3, total: 3, met: true, percent: 100 }], progress: 100, met: true, late: false, tasks: [] }, { key: 'release:rel-2', kind: 'release', id: '', release_id: 'rel-2', name: 'V1.0', description: 'The first release for customers', date: '2026-12-01', release_status: 'in_progress', default_criteria: true, criteria: [{ metric: 'requirements_approved', label: 'Requirements approved', target: 100, actual: 50, count: 1, total: 2, met: false, percent: 50 }, { metric: 'requirements_verified', label: 'Requirements verified', target: 100, actual: 50, count: 1, total: 2, met: false, percent: 50 }], progress: 50, met: false, late: false, tasks: [] }], dependencies: [{ work_item_id: 'wi-3', depends_on_id: 'wi-2', type: 'FS' }], metrics: [{ metric: 'needs_addressed', label: 'User needs addressed', count: 3, total: 3, percent: 100 }], metric_catalogue: [{ key: 'needs_addressed', label: 'User needs addressed', description: 'User needs that at least one requirement derives from.' }], summary: { tasks: 4, done: 1, late: 1, unscheduled: 0, progress: 34, critical: 0, effort_days: 0, cost: 0, overallocated: 0 } }],
  [/\/api\/v1\/projects\/p1\/vv\/releases\/[^/]+$/, { release: { id: 'rel-2', project_id: 'p1', name: 'V1.0', description: 'The first release for customers', target_date: '2026-12-01', status: 'in_progress', position: 1, released_at: null, created_at: '2026-10-01T09:00:00Z', updated_at: '2026-10-01T09:00:00Z' }, summary: { total: 2, introduced: 1, approved: 1, verified: 1, ready: 1, by_status: { approved: 1, draft: 1 }, by_rollup: { pass: 1, uncovered: 1 } }, entries: [{ requirement_id: 'req-1', ref: 'REQ-1', title: 'Work envelope of at least 300 by 200 by 100 millimetres', status: 'approved', rollup: 'pass', range: { from: 'rel-1' }, introduced: false, test_case_ids: ['tc-1'], latest_results: { 'tc-1': 'pass' } }, { requirement_id: 'req-2', ref: 'REQ-2', title: 'Positioning accuracy', status: 'draft', rollup: 'uncovered', range: { from: 'rel-2', until: 'rel-2' }, introduced: true, test_case_ids: [], latest_results: {} }], burn_up: [{ date: '2026-10-01', scope: 1, verified: 0 }, { date: '2026-10-08', scope: 2, verified: 0 }, { date: '2026-10-15', scope: 2, verified: 1 }, { date: '2026-10-22', scope: 2, verified: 1 }] }],
  [/\/api\/v1\/projects\/p1\/vv\/releases/, { releases: [{ release: { id: 'rel-1', project_id: 'p1', name: 'MVP', description: '', target_date: '2026-10-15', status: 'released', position: 0, released_at: '2026-10-15T12:00:00Z', created_at: '2026-09-01T09:00:00Z', updated_at: '2026-10-15T12:00:00Z' }, summary: { total: 1, introduced: 1, approved: 1, verified: 1, ready: 1, by_status: { approved: 1 }, by_rollup: { pass: 1 } } }, { release: { id: 'rel-2', project_id: 'p1', name: 'V1.0', description: 'The first release for customers', target_date: '2026-12-01', status: 'in_progress', position: 1, released_at: null, created_at: '2026-10-01T09:00:00Z', updated_at: '2026-10-01T09:00:00Z' }, summary: { total: 2, introduced: 1, approved: 1, verified: 1, ready: 1, by_status: { approved: 1, draft: 1 }, by_rollup: { pass: 1, uncovered: 1 } } }], unassigned: 0, unassigned_ids: [] }],
  [/\/api\/v1\/projects\/p1\/vv\/coverage/, { project_id: 'p1', entries: [{ requirement_id: 'req-1', title: 'Work envelope of at least 300 by 200 by 100 millimetres', verification_method: 'inspection', verification_status: 'verified', test_case_ids: ['tc-1'], latest_results: { 'tc-1': 'pass' }, rollup: 'pass' }, { requirement_id: 'req-2', title: 'Positioning accuracy', verification_method: 'test', verification_status: '', test_case_ids: [], latest_results: {}, rollup: 'uncovered' }], summary: { pass: 1, uncovered: 1 } }],
  [/\/api\/v1\/projects\/p1\/vv\/gaps/, { requirements_without_method: [], requirements_without_test_case: ['req-2'], requirements_unverified: [], requirements_failing: [], orphan_test_cases: [], needs_without_requirement: [], hazards_unmitigated: [] }],
  [/\/api\/v1\/projects\/p1\/vv\/matrix/, { rows: [{ requirement_id: 'req-1', title: 'Work envelope of at least 300 by 200 by 100 millimetres', user_need_ids: ['need-1'], design_ids: [], test_case_ids: ['tc-1'], latest_results: { 'tc-1': 'pass' }, hazard_ids: [] }, { requirement_id: 'req-2', title: 'Positioning accuracy', user_need_ids: ['need-1'], design_ids: [], test_case_ids: [], latest_results: {}, hazard_ids: [] }, { requirement_id: 'req-3', title: 'Repeatability under thermal drift', user_need_ids: ['need-1'], design_ids: ['des-1'], test_case_ids: ['tc-1', 'tc-2'], latest_results: { 'tc-1': 'pass', 'tc-2': 'fail' }, hazard_ids: [] }, { requirement_id: 'req-4', title: 'Spindle speed range', user_need_ids: ['need-1'], design_ids: ['des-1', 'des-3'], test_case_ids: ['tc-1'], latest_results: { 'tc-1': 'pass' }, hazard_ids: ['haz-1'] }, { requirement_id: 'req-5', title: 'Emergency stop', user_need_ids: ['need-1'], design_ids: ['des-3'], test_case_ids: ['tc-2'], latest_results: { 'tc-2': 'blocked' }, hazard_ids: ['haz-1'] }] }],
  // Evidence: one capture cited by the run's result, which is the shape the
  // feature exists for (one rig session, several test cases).
  [/\/api\/v1\/projects\/p1\/evidence-bundles/, [evidenceBundle]],
  [/\/api\/v1\/evidence-bundles\/ev1/, evidenceBundle],
  [/\/api\/v1\/test-runs\/tr2\/citations/, { res2: [{ ...evidenceCitation, id: 'evc2', test_result_id: 'res2', run_id: 'tr2' }] }],
  [/\/api\/v1\/test-runs\/tr2\/results/, [{ id: 'res2', run_id: 'tr2', test_case_id: 'tc-1', status: 'pass', notes: 'Ballbar within spec at all heights.', evidence: [], executed_at: now }]],
  [/\/api\/v1\/test-runs\/tr2/, openTestRun],
  [/\/api\/v1\/test-runs\/tr1\/citations/, { res1: [evidenceCitation] }],
  [/\/api\/v1\/test-results\/res1\/citations/, [evidenceCitation]],
  [/\/api\/v1\/projects\/p1\/test-runs\/tr1/, testRun],
  [/\/api\/v1\/test-runs\/tr1\/results/, [{ id: 'res1', run_id: 'tr1', test_case_id: 'tc-1', status: 'pass', notes: 'Ballbar within spec at all heights.', evidence: [], executed_at: now }]],
  [/\/api\/v1\/test-runs\/tr1/, testRun],
  [/\/api\/v1\/projects\/p1\/test-runs/, [openTestRun, testRun]],
  [/\/api\/v1\/projects\/p1\/work-items/, workItems],
  // A card's own to-do list, ahead of the card itself, whose pattern it also matches.
  [/\/api\/v1\/work-items\/w1\/todos/, []],
  [/\/api\/v1\/work-items\/w1/, { item: workItems[0], activity: [{ id: 'act1', work_item_id: 'w1', kind: 'comment', actor: 'Sam Example', content: 'Started drafting.', payload: {}, created_at: now }] }],
  [/\/api\/v1\/projects\/p1\/members/, [{ user_id: 'u1', email: user.email, name: user.name, role: 'owner' }]],
  [/\/api\/v1\/projects\/p1\/team-access/, []],
  [/\/api\/v1\/projects\/p1\/repo-connections/, [{ id: 'rc1', project_id: 'p1', name: 'openv', remote_url: 'https://github.com/desktopmachineshop/OpenV.git', default_branch: 'master', credential_strategy: 'host', created_at: now, updated_at: now }]],
  [/\/api\/v1\/projects\/p1\/activity/, { entries: [{ id: 'ev1', event_type: 'artifact.updated', actor: 'Sam Example', summary: 'Updated REQ-2 Positioning accuracy', created_at: now }] }],
  [/\/api\/v1\/projects\/p1\/impact/, { project_id: 'p1', artifact_id: 'req-2', direction: 'both', downstream: [{ type: 'requirement', count: 1, nodes: [{ artifact_id: 'req-3', title: 'Repeatability under thermal drift', type: 'requirement', distance: 1, via: 'decomposes-to', path: ['req-2', 'req-3'] }] }, { type: 'design-item', count: 1, nodes: [{ artifact_id: 'des-1', title: 'Ball-screw axis drive', type: 'design-item', distance: 1, via: 'satisfies', path: ['req-2', 'des-1'] }] }, { type: 'test-case', count: 2, nodes: [{ artifact_id: 'tc-1', title: 'Accuracy survey with a ballbar', type: 'test-case', distance: 1, via: 'verifies', path: ['req-2', 'tc-1'] }, { artifact_id: 'tc-2', title: 'Warm-up drift run', type: 'test-case', distance: 2, via: 'verifies', path: ['req-2', 'req-3', 'tc-2'] }] }, { type: 'hazard', count: 1, nodes: [{ artifact_id: 'haz-1', title: 'Axis crash at rapid traverse', type: 'hazard', distance: 2, via: 'mitigates', path: ['req-2', 'des-1', 'haz-1'] }] }], upstream: [{ type: 'user-need', count: 1, nodes: [{ artifact_id: 'need-1', title: 'Cut aluminium parts without a machinist', type: 'user-need', distance: 1, via: 'derives-from', path: ['req-2', 'need-1'] }] }], total: 6 }],
  [/\/api\/v1\/projects\/p1\/review-queue/, { suspect_links: [{ ...links[1], from_title: 'Positioning accuracy', from_type: 'requirement', to_title: 'Cut aluminium parts without a machinist', to_type: 'user-need' }], in_review_artifacts: [artifacts[4], artifacts.find((a) => a.type === 'requirement' && a !== artifacts[4]) || artifacts[5]] }],
  [/\/api\/v1\/projects\/p1$/, project],
  [/\/api\/v1\/projects\/p1\//, []],
  [/\/api\/v1\/projects$/, [project, { ...project, id: 'p2', name: 'Second project' }]],
  [/\/api\/v1\/artifacts\?/, (url) => { const t = new URL(url).searchParams.get('type'); return t ? artifacts.filter((a) => a.type === t) : artifacts; }],
  [/\/api\/v1\/artifacts\/[^/]+\/versions/, (url) => [byId[url.match(/artifacts\/([^/]+)\/versions/)[1]]]],
  [/\/api\/v1\/artifacts\/[^/]+\/quality/, { score: 96, band: 'good', findings: [] }],
  [/\/api\/v1\/artifacts\/([^/]+)\/links/, (url) => { const id = url.match(/artifacts\/([^/]+)\/links/)[1]; return links.filter((l) => l.from_id === id || l.to_id === id); }],
  [/\/api\/v1\/artifacts\/[^/]+\/attachments/, []],
  [/\/api\/v1\/artifacts\/[^/]+\/comments/, []],
  [/\/api\/v1\/artifacts\/([^/?]+)(\?|$)/, (url) => byId[url.match(/artifacts\/([^/?]+)/)[1]] || {}],
  [/\/api\/v1\/links/, links],
  [/\/api\/v1\/notifications\/stream/, null],
  [/\/api\/v1\/notifications/, { notifications, unread: 1 }],
  [/\/api\/v1\/agent-stats/, { days: 7, since: now, agents: [] }],
  [/\/api\/v1\/agents/, agents],
  [/\/api\/v1\/automations/, [automation]],
  // The log stream: aborted, not answered with JSON — WebKit reports a
  // wrong-MIME EventSource as a page error, and the panel's polling
  // fallback is what a mocked run should exercise anyway.
  [/\/api\/v1\/agent-runs\/[^/]+\/stream/, null],
  [/\/api\/v1\/agent-runs\/r\d\/(messages|events|log|tree)/, []],
  [/\/api\/v1\/agent-runs\/(r\d)(\?|$)/, (url) => runs.find((r) => r.id === url.match(/agent-runs\/(r\d)/)[1]) || runs[0]],
  [/\/api\/v1\/agent-runs/, runs],
  [/\/api\/v1\/proposals/, [{ id: 'pr1', org_id: 'org1', project_id: 'p1', run_id: 'r1', agent_name: 'Requirements Analyst', op: 'update_artifact', kind: 'update_artifact', target_id: 'req-2', target_ref: 'REQ-2', summary: 'Tighten the accuracy wording', payload: { title: 'Positioning accuracy', body: 'The mill shall position the tool within ±0.01 mm of the commanded position over the full envelope at 20 °C.' }, status: 'pending', created_at: now }]],
  [/\/api\/v1\/runner-sessions/, []],
  [/\/api\/v1\/provider-logins/, providerLogin],
  [/\/api\/v1\/provider-settings/, []],
  [/\/api\/v1\/chatter/, []],
  [/\/api\/v1\/search/, { hits: [] }],
];
// How a route table's entry is answered: null aborts the request (the log
// stream, which must not be answered with JSON), a function is called with
// the URL, anything else is the JSON body. Both tables answer the same way,
// so both go through here.
const fulfil = (route, body) => {
  if (body === null) return route.abort();
  const b = typeof body === 'function' ? body(route.request().url()) : body;
  return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(b) });
};
async function mock(page) {
  await page.route('**/api/**', async (route) => {
    const url = route.request().url();
    for (const [re, body] of routes) if (re.test(url)) return fulfil(route, body);
    return route.fulfill({ status: 200, contentType: 'application/json', body: route.request().method() === 'GET' ? '[]' : '{}' });
  });
}
// A screen may override a route for itself (a leased runner, a failed run):
// registered after the shared table, so Playwright tries it first and falls
// back to the shared one for everything else.
async function mockScreen(page, extra) {
  if (!extra || !extra.length) return;
  await page.route('**/api/**', async (route) => {
    const url = route.request().url();
    for (const [re, body] of extra) if (re.test(url)) return fulfil(route, body);
    return route.fallback();
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
    // one-line ellipsis, a text field with a long value, ag-grid's
    // virtualised viewports, or a surface that is panned by dragging (a
    // diagram marks itself data-pan-surface).
    if (/(hidden|clip)/.test(cs.overflowX) && el.scrollWidth > el.clientWidth + 2 && cs.textOverflow !== 'ellipsis' && !el.matches('input') && !el.closest('pre, code, .table-scroll, .tab-strip, textarea, svg, canvas, .ag-root-wrapper, [data-pan-surface]')) {
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
  // Chrome: what stands between the top of the window and the page's own
  // content, plus the tab bar under it.
  // A page that marks no content start, or whose mark is pushed off the
  // window by a form open above it, is measured from the shell's <main>.
  const content = Array.from(document.querySelectorAll('[data-page-content]')).find((el) => vis(el) && el.getBoundingClientRect().top < window.innerHeight) || Array.from(document.querySelectorAll('main')).find(vis);
  if (content) {
    const bar = document.querySelector('.ov-tabbar');
    const top = Math.max(0, Math.round(content.getBoundingClientRect().top));
    const bottom = bar && vis(bar) ? Math.round(bar.getBoundingClientRect().height) : 0;
    out.chrome = { top, bottom, share: Math.round(((top + bottom) / window.innerHeight) * 100) };
  }
  // Gutter: how far the page's own content sits from the left and top edges
  // of the shell's <main>, so pages can be held to one margin.
  const mainEl = Array.from(document.querySelectorAll('main')).find(vis);
  if (mainEl) {
    const mr = mainEl.getBoundingClientRect();
    let left = Infinity, top = Infinity, right = -Infinity;
    for (const el of mainEl.querySelectorAll('*')) {
      if (!vis(el)) continue;
      const hasText = Array.from(el.childNodes).some((n) => n.nodeType === 3 && n.textContent.trim());
      if (!hasText && !el.matches('input, select, textarea, button, img, svg')) continue;
      const r = el.getBoundingClientRect();
      if (r.width === 0 || r.bottom < mr.top || r.top > window.innerHeight || r.right <= mr.left || r.left >= mr.right) continue;
      left = Math.min(left, r.left - mr.left);
      top = Math.min(top, r.top - mr.top);
      if (r.right <= mr.right + 1) right = Math.max(right, r.right);
    }
    if (left !== Infinity) out.gutter = { left: Math.round(left), top: Math.round(top), right: Math.round(mr.right - right) };
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
// The project menu on a phone: the More tab of the bottom bar in the
// refreshed shell, the header's menu button before it.
const openMenu = async (p) => {
  const more = p.getByRole('button', { name: 'More', exact: true });
  await press((await more.count()) ? more : p.getByRole('button', { name: 'Project menu' }));
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
  { tag: 'user-settings', path: '/projects', open: async (p) => { await press(p.locator('[title="Sam Example"]').first()); await p.waitForTimeout(300); await press(p.getByRole('button', { name: 'Settings', exact: true }).first()); } },
  { tag: 'drawer-user-settings', phone: true, path: '/projects/p1', open: async (p) => { await openMenu(p); await p.waitForTimeout(300); await press(p.getByText('Sam Example').last()); } },
  { tag: 'overview', path: '/projects/p1' },
  { tag: 'drawer', phone: true, path: '/projects/p1', open: async (p) => openMenu(p) },
  { tag: 'notifications', path: '/projects/p1', open: async (p) => press(p.getByRole('button', { name: /notifications/i }).first()) },
  { tag: 'help', path: '/projects/p1', open: async (p) => { const help = p.getByRole('button', { name: 'Help' }); if (!(await help.count())) { await openMenu(p); await p.waitForTimeout(300); } await press(help.first()); } },
  { tag: 'requirements', path: '/projects/p1/requirements' },
  { tag: 'requirements-doc', path: '/projects/p1/requirements', open: async (p) => { const ex = p.getByRole('button', { name: 'Expand all' }); if (await ex.count()) await press(ex.first()); await press(p.getByText('Positioning accuracy').first()); await p.waitForTimeout(600); } },
  { tag: 'requirements-new', path: '/projects/p1/requirements', open: async (p) => press(p.getByRole('button', { name: '+ New Artifact' })) },
  { tag: 'requirements-edit', path: '/projects/p1/requirements', open: async (p) => { const ex = p.getByRole('button', { name: 'Expand all' }); if (await ex.count()) await press(ex.first()); await press(p.getByText('Positioning accuracy').first()); await p.waitForTimeout(500); await press(p.getByRole('button', { name: 'Edit', exact: true }).first()); } },
  // On a phone the toolbar folds into an action sheet; a desktop has the
  // Download button in the toolbar itself.
  { tag: 'requirements-download', path: '/projects/p1/requirements', open: async (p) => { await press(p.getByRole('button', { name: 'Requirements actions' })); await press(p.getByRole('button', { name: /Download/ }).first()); }, openDesktop: async (p) => { const menu = p.getByRole('button', { name: /^(Working copy|Baseline)/ }); if (await menu.count()) await press(menu.first()); await press(p.getByRole('button', { name: /Download/ }).first()); } },
  { tag: 'requirements-menu', path: '/projects/p1/requirements', open: async (p) => { const ex = p.getByRole('button', { name: 'Expand all' }); if (await ex.count()) await press(ex.first()); await press(p.getByRole('button', { name: /Actions for Positioning/ })); } },
  // The tree and the layers (a phone's outline and its drill-down by level).
  { tag: 'requirements-tree', path: '/projects/p1/requirements?view=tree&artifact=req-2' },
  { tag: 'requirements-layers', path: '/projects/p1/requirements?view=layers&artifact=req-2', open: async (p) => { const down = p.getByRole('button', { name: /Functional requirements/ }); if (await down.count()) await press(down.first()); } },
  { tag: 'baseline-compare', path: '/projects/p1/baselines/b1/compare' },
  { tag: 'guided', path: '/projects/p1/guided' },
  { tag: 'guided-assistant', path: '/projects/p1/guided', open: async (p) => { const b = p.getByRole('button', { name: /assistant/i }).first(); if (await b.count()) await press(b); } },
  { tag: 'interviews', path: '/projects/p1/interviews' },
  { tag: 'vv', path: '/projects/p1/vv' },
  { tag: 'releases', path: '/projects/p1/releases' },
  { tag: 'gantt', path: '/projects/p1/gantt' },
  { tag: 'gantt-network', wide: true, path: '/projects/p1/gantt', open: async (p) => { await p.selectOption('select[aria-label="View"]', 'network'); await p.waitForTimeout(400); } },
  { tag: 'gantt-calendar', wide: true, path: '/projects/p1/gantt', open: async (p) => { await p.selectOption('select[aria-label="View"]', 'calendar'); await p.waitForTimeout(400); } },
  { tag: 'gantt-usage', wide: true, path: '/projects/p1/gantt', open: async (p) => { await p.selectOption('select[aria-label="View"]', 'usage'); await p.waitForTimeout(400); } },
  { tag: 'gantt-timeline', wide: true, path: '/projects/p1/gantt', open: async (p) => { await p.selectOption('select[aria-label="View"]', 'timeline'); await p.waitForTimeout(400); } },
  { tag: 'gantt-master', wide: true, path: '/projects/p1/gantt', open: async (p) => { await p.selectOption('select[aria-label="View"]', 'master'); await p.waitForTimeout(400); } },
  { tag: 'home', path: '/projects/p1/home', routes: rdRoutes },
  { tag: 'design', path: '/projects/p1/design', routes: rdRoutes },
  { tag: 'tests', path: '/projects/p1/tests', routes: rdRoutes },
  { tag: 'baselines', path: '/projects/p1/baselines', routes: rdRoutes },
  { tag: 'test-run', path: '/projects/p1/vv/runs/tr1' },
  { tag: 'evidence', path: '/projects/p1/evidence' },
  { tag: 'evidence-detail', path: '/projects/p1/evidence', open: async (p) => { await press(p.getByText('Noise sweep, 90 minutes, all load conditions').first()); await p.waitForTimeout(400); } },
  { tag: 'evidence-new', path: '/projects/p1/evidence', open: async (p) => press(p.getByRole('button', { name: 'Record a capture' }).first()) },
  { tag: 'test-run-evidence', path: '/projects/p1/vv/runs/tr2', open: async (p) => { const b = p.getByRole('button', { name: /EVD-1|\+ evidence/ }).first(); if (await b.count()) await press(b); await p.waitForTimeout(400); } },
  { tag: 'matrix', path: '/projects/p1/matrix' },
  { tag: 'matrix-row', phone: true, path: '/projects/p1/matrix', open: async (p) => { await press(p.getByRole('button', { name: /Repeatability under thermal drift/ })); } },
  { tag: 'impact', path: '/projects/p1/impact?artifact=req-2' },
  { tag: 'review', path: '/projects/p1/review' },
  { tag: 'board', path: '/projects/p1/board' },
  { tag: 'board-card', path: '/projects/p1/board', open: async (p) => { await press(p.getByText('Write the enclosure interlock').first()); } },
  { tag: 'automations', path: '/projects/p1/automations' },
  { tag: 'automations-new', path: '/projects/p1/automations', open: async (p) => { const b = p.getByRole('button', { name: /New automation|\+ New/i }).first(); if (await b.count()) await press(b); } },
  { tag: 'agent-runs', path: '/projects/p1/agent-runs' },
  { tag: 'agent-run-detail', path: '/projects/p1/agent-runs', open: async (p) => { const b = p.getByText('Review the safety requirements').first(); if (await b.count()) await press(b); } },
  // A failed run's detail, where Retry lives, and a running one, where
  // Cancel does: on a phone both open as a full-screen sheet.
  { tag: 'run-detail-actions', path: '/projects/p1/agent-runs?run=r3' },
  { tag: 'run-detail-cancel', path: '/projects/p1/agent-runs?run=r2' },
  { tag: 'agents', path: '/projects/p1/agents' },
  { tag: 'agents-edit', path: '/projects/p1/agents', open: async (p) => { const b = p.getByText('Requirements Analyst').first(); if (await b.count()) await press(b); } },
  { tag: 'activity', path: '/projects/p1/activity' },
  { tag: 'settings', path: '/projects/p1/settings' },
  { tag: 'settings-agents', path: '/projects/p1/settings?tab=agents' },
  { tag: 'settings-access', path: '/projects/p1/settings?tab=members' },
  // The assistant beside the project, showing the three project-mode cards
  // (edit, move, new artifact) with their buttons: the floating panel (a
  // bottom sheet on a phone) where the floating-assistant gate is on, else
  // the Notes panel's tab, a stacked pane on a phone.
  { tag: 'notes-assistant', path: '/projects/p1/requirements', open: async (p) => {
    const floating = p.getByRole('button', { name: 'Open the V&V Assistant' }).first();
    if (await floating.count()) { await press(floating); await p.waitForTimeout(800); return; }
    const pane = p.getByRole('button', { name: /^Notes$/ }).first();
    if (await pane.count()) { await press(pane); await p.waitForTimeout(300); }
    const tab = p.getByRole('button', { name: 'V&V Assistant' }).first();
    if (await tab.count()) { await press(tab); await p.waitForTimeout(800); }
  } },
  { tag: 'whats-new', path: '/whats-new' },
  { tag: 'org-settings', path: '/org/settings' },
  // The workspace's runs with no project, and one open: a sheet on a phone.
  { tag: 'org-runs', path: '/org/runs' },
  { tag: 'org-runs-detail', path: '/org/runs?run=r3' },
  { tag: 'org-members', path: '/org/settings?tab=members' },
  { tag: 'org-limits', path: '/org/settings?tab=limits' },
  // The tab strip is buttons in a role=tablist, not role=tab elements, so
  // the tab is selected through its URL (?tab=…) rather than a click that
  // silently matched nothing.
  { tag: 'org-runners', path: '/org/settings?tab=worker-keys' },
  // REQ-108, the phone's runner controls: a live lease with its countdown,
  // extend and end; and the relayed vendor sign-in waiting for a paste-back.
  // Both live in personal settings, which opens from the account menu.
  {
    tag: 'org-runner-lease',
    path: '/projects',
    routes: [[/\/api\/v1\/orgs\/[^/]+\/runner-session/, runnerSessionLeased]],
    open: async (p) => {
      await press(p.locator('[title="Sam Example"]').first());
      await p.waitForTimeout(300);
      await press(p.getByRole('button', { name: 'Settings', exact: true }).first());
      await p.waitForTimeout(600);
      // The panel is a fixed full-screen sheet, so the card has to be
      // scrolled to before the screenshot shows it.
      await p.getByRole('heading', { name: 'Cloud runner' }).scrollIntoViewIfNeeded();
      await p.waitForTimeout(200);
    },
  },
  {
    tag: 'runner-relay',
    path: '/projects',
    routes: [[/\/api\/v1\/orgs\/[^/]+\/runner-session/, runnerSessionLeased]],
    open: async (p) => {
      await press(p.locator('[title="Sam Example"]').first());
      await p.waitForTimeout(300);
      await press(p.getByRole('button', { name: 'Settings', exact: true }).first());
      await p.waitForTimeout(600);
      const connect = p.getByRole('button', { name: 'Connect', exact: true }).first();
      await connect.scrollIntoViewIfNeeded();
      await press(connect);
      await p.waitForTimeout(400);
      await p.getByRole('link', { name: /Open sign-in page/ }).scrollIntoViewIfNeeded();
      await p.waitForTimeout(200);
    },
  },
  { tag: 'org-usage', path: '/org/settings?tab=usage' },
  { tag: 'org-quality', path: '/org/settings?tab=quality' },
  { tag: 'org-providers', path: '/org/settings?tab=providers' },
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
    if (!desktop && s.wide) continue;
    const open = desktop && s.openDesktop ? s.openDesktop : s.open;
    // A fresh context per screen: Chromium flips its hover/pointer media
    // flags after a synthetic mouse event, which real phones never see.
    const ctx = await browser.newContext({ ...device, baseURL: BASE_URL, serviceWorkers: 'block', colorScheme: COLOR_SCHEME });
    const page = await ctx.newPage();
    const errors = [];
    page.on('pageerror', (e) => errors.push(String(e).split('\n')[0]));
    await mock(page);
    await mockScreen(page, s.routes);
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
      if (r.chrome && r.chrome.share > CHROME_BUDGET) { flags.push(`CHROME ${r.chrome.share}%>${CHROME_BUDGET}%`); failed = true; }
      const chrome = r.chrome ? `  chrome ${r.chrome.top}+${r.chrome.bottom}px (${r.chrome.share}%)` : '';
      console.log(`${eng} ${r.tag.padEnd(22)} ${flags.join(' ') || 'ok'}${chrome}`);
    }
  }
  console.log(`report: ${OUT}/audit.json`);
  if (failed) process.exit(1);
})().catch((e) => { console.error(e); process.exit(1); });
