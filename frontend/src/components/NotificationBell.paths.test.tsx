// Refactor plan step S10 (invariant I18, quirk Q7): where the bell takes a
// member for each notification the Go code delivers, beside where the email
// and the web push for the same notification take them.
//
// The notifications are the Go goldens under
// internal/notify/testdata/notifications/<type>/, which the Go test
// TestNotificationContent writes from the real delivery paths. The bell is
// fed each one's SSE data line, the JSON it receives live, and the row is
// clicked; the Go link is the push payload's url (notificationPath in
// internal/notify/email.go), which the email's "Open it in OpenV" link must
// equal once its base is stripped. The table pins pathForNotification
// (NotificationBell.tsx) for every notification type beside
// notificationPath. They drifted apart (Q7) until the R7 fix for #379's bugs
// 58 and 59 sent every link to the page the bell opens, so the test now
// also fails when one differs. X4b later replaces the mirrored cases with a
// shared fixture that keeps separate go and ts expectations.
//
// Regenerate (a deliberate, release-noted behavior change only):
//   npx vitest run src/components/NotificationBell.paths.test.tsx -u
// A Go-side change regenerates the Go goldens first:
//   UPDATE_GOLDEN=1 go test ./internal/notify -count=1 -run '^TestNotificationContent$'
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { NotificationBell } from './NotificationBell';
import { notificationsAPI } from '../api/client';

vi.mock('../api/client', () => ({
  notificationsAPI: {
    list: vi.fn(),
    markRead: vi.fn(),
    markAllRead: vi.fn(),
    clearAll: vi.fn(),
    deleteCleared: vi.fn(),
    setFlagged: vi.fn(),
    streamUrl: () => 'http://localhost/api/v1/notifications/stream',
  },
}));

// vi.mock factories are hoisted above the imports, so what they close over
// is hoisted with them.
const { mockNavigate } = vi.hoisted(() => ({ mockNavigate: vi.fn() }));
vi.mock('react-router-dom', () => ({
  useNavigate: () => mockNavigate,
  useSearchParams: () => [new URLSearchParams(), () => {}],
}));

vi.mock('../hooks/useViewport', () => ({
  useViewport: () => ({ isPhone: false, isCompact: false }),
}));

vi.mock('./ui', () => ({
  useConfirm: () => () => Promise.resolve(true),
}));

// EventSource is not in jsdom, and the bell opens one on mount.
class FakeEventSource {
  addEventListener() {}
  close() {}
}
(globalThis as any).EventSource = FakeEventSource;
(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const api = vi.mocked(notificationsAPI);

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../..');
const GOLDENS = 'internal/notify/testdata/notifications';
const TYPES_DIR = 'internal/domain/notifications';
// The base the Go test gives the email dispatcher (ncLinkBase), without its
// trailing slash, which the dispatcher trims.
const LINK_BASE = 'https://openv.example.test';
const REGENERATE = 'npx vitest run src/components/NotificationBell.paths.test.tsx -u';

const read = (rel: string) => fs.readFileSync(path.join(REPO, rel), 'utf8');

// The notification types: every exported Type* string constant of the Go
// package, read from its source as the Go completeness test does.
function goTypes(): string[] {
  const out: string[] = [];
  const files = fs
    .readdirSync(path.join(REPO, TYPES_DIR))
    .filter((f) => f.endsWith('.go') && !f.endsWith('_test.go'))
    .sort();
  for (const f of files) {
    for (const m of read(`${TYPES_DIR}/${f}`).matchAll(/^\s*(Type[A-Z][A-Za-z0-9]*)\s*=\s*"([^"]+)"/gm)) {
      out.push(m[2]);
    }
  }
  return out;
}

// One delivery of a golden, keyed by scenario and "k of n".
const HEADER = (marker: string) => new RegExp(`^${marker} (.*) \\| delivery (\\d+ of \\d+) \\| to (\\S+)$`);

function blocks(text: string, marker: string): Map<string, string[]> {
  const out = new Map<string, string[]>();
  let current: string[] | null = null;
  const header = HEADER(marker);
  for (const line of text.split('\n')) {
    const m = header.exec(line);
    if (m) {
      current = [];
      out.set(`${m[1]} | ${m[2]}`, current);
    } else if (current) {
      current.push(line);
    }
  }
  return out;
}

interface Delivery {
  type: string;
  key: string;
  notification: any;
  push: string;
  email: string;
}

function deliveries(type: string): Delivery[] {
  const sse = blocks(read(`${GOLDENS}/${type}/sse.txt`), '===');
  const email = blocks(read(`${GOLDENS}/${type}/email.txt`), '========');
  const push = JSON.parse(read(`${GOLDENS}/${type}/push.json`));
  const out: Delivery[] = [];
  for (const d of push.deliveries) {
    const key = `${d.scenario} | ${d.delivery}`;
    const data = (sse.get(key) || []).find((l) => l.startsWith('data: '));
    if (!data) throw new Error(`${GOLDENS}/${type}/sse.txt has no frame for ${key}`);
    const lines = email.get(key) || [];
    const at = lines.indexOf('Open it in OpenV:');
    const urls = (d.sends as { payload: string }[]).map((s) => JSON.parse(s.payload).url);
    if (urls.length === 0 || at < 0) {
      throw new Error(`${GOLDENS}/${type}: ${key} has no push or no email link to compare with`);
    }
    out.push({
      type,
      key,
      notification: JSON.parse(data.slice('data: '.length)),
      push: urls[0],
      email: lines[at + 1],
    });
  }
  return out;
}

let container: HTMLDivElement;
let root: Root;

const flush = async () => {
  await act(async () => {
    await Promise.resolve();
  });
};

const click = async (el: Element) => {
  await act(async () => {
    el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
  await flush();
};

// Where the bell navigates when its one row is clicked.
async function bellPath(n: any): Promise<string> {
  api.list.mockResolvedValue({ data: { notifications: [n], unread_count: 1 } } as any);
  mockNavigate.mockClear();
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
  try {
    await act(async () => {
      root.render(<NotificationBell />);
    });
    await flush();
    await click(container.querySelector('button[aria-label^="Notifications"]')!);
    const title = Array.from(container.querySelectorAll('div')).find(
      (el) => el.children.length === 0 && el.textContent === n.title
    );
    if (!title) throw new Error(`the bell shows no row titled ${JSON.stringify(n.title)}`);
    await click(title);
    expect(mockNavigate).toHaveBeenCalledTimes(1);
    return mockNavigate.mock.calls[0][0];
  } finally {
    act(() => root.unmount());
    container.remove();
  }
}

const pad = (rows: string[][]): string[] => {
  const widths = rows[0].map((_, i) => Math.max(...rows.map((r) => r[i].length)));
  return rows.map((r) => r.map((c, i) => (i === r.length - 1 ? c : c.padEnd(widths[i]))).join('  ').trimEnd());
};

describe("the bell's deep link for every notification type", () => {
  it('matches the pinned table beside the email and push link (quirk Q7)', async () => {
    const types = goTypes();
    expect(types.length).toBeGreaterThanOrEqual(12);
    const unpinned = types.filter((t) =>
      ['sse.txt', 'email.txt', 'push.json'].some((f) => !fs.existsSync(path.join(REPO, GOLDENS, t, f)))
    );
    expect(
      unpinned,
      `notification types with no Go goldens under ${GOLDENS}: add a scenario to ncScenarios in ` +
        "internal/notify/notification_content_test.go, then run UPDATE_GOLDEN=1 go test ./internal/notify -count=1 " +
        `-run '^TestNotificationContent$' and ${REGENERATE}`
    ).toEqual([]);
    const all = types.flatMap((t) => deliveries(t));

    // The email and the push carry the same Go link; only the email's has
    // the deployment's base in front.
    const split = all.filter((d) => d.email !== LINK_BASE + d.push);
    expect(split.map((d) => `${d.type}: ${d.key}: email ${d.email}, push ${d.push}`)).toEqual([]);

    // One row per distinct link a type's notifications carry, in type order.
    const seen = new Map<string, string[]>();
    for (const d of all) {
      const ref = JSON.stringify(d.notification.entity_ref);
      const key = `${d.type}\t${ref}`;
      if (seen.has(key)) continue;
      const n = {
        ...d.notification,
        id: `n-${seen.size + 1}`,
        created_at: '2026-10-03T09:00:00Z',
      };
      const bell = await bellPath(n);
      seen.set(key, [d.type, String(d.notification.entity_ref?.kind ?? ''), bell, d.push, bell === d.push ? 'same' : 'differs']);
    }
    const rows = [...seen.values()];
    const missing = types.filter((t) => !rows.some((r) => r[0] === t));
    expect(missing).toEqual([]);

    const unique = [...new Set(rows.map((r) => r.slice(0, 5).join('\t')))].map((r) => r.split('\t'));
    // The email and the push open the page the bell does (quirk Q7, fixed
    // under R7 for #379's bugs 58 and 59).
    expect(unique.filter((r) => r[4] !== 'same').map((r) => `${r[0]} ${r[1]}: bell ${r[2]}, email and push ${r[3]}`)).toEqual([]);
    const text = [
      '# Where the bell (pathForNotification, NotificationBell.tsx) and the email and web push',
      '# (notificationPath, internal/notify/email.go) send a member, for every notification the Go',
      '# goldens under internal/notify/testdata/notifications deliver: refactor plan step S10.',
      '# They open the same page (quirk Q7, fixed under R7 for #379\'s bugs 58 and 59).',
      `# Regenerate: ${REGENERATE}`,
      `# ${types.length} types; ${unique.length} distinct links; ${unique.filter((r) => r[4] === 'differs').length} differ.`,
      '',
      ...pad([['type', 'entity_ref.kind', 'bell', 'email and push', ''], ...unique]),
    ].join('\n');
    await expect(text + '\n').toMatchFileSnapshot('./__snapshots__/NotificationBell.paths.txt');
  }, 60_000);
});
