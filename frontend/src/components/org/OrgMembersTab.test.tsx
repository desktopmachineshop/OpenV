import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../../test/mockApi';
import { OrgMembersTab } from './OrgMembersTab';
import { orgsAPI } from '../../api/client';

// The Members tab on a stale list: another admin, or the member themselves,
// removed someone after the list loaded. The server refuses a removal or a
// role change for them with ErrNotMember, whose words ("you are not a member
// of this organization") address the caller, so the tab must not show them
// to an admin who is still a member; it re-reads the list and says who left.
vi.mock('../../api/client', async (orig) => mockApi(await orig()));

vi.mock('react-router-dom', () => ({ useNavigate: () => vi.fn() }));

vi.mock('../ui', () => ({
  useConfirm: () => () => Promise.resolve(true),
  ErrorBanner: ({ message }: { message: string }) => (message ? <div role="alert">{message}</div> : null),
}));

vi.mock('../Avatar', () => ({ Avatar: () => null }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const api = vi.mocked(orgsAPI, true);

const org = { id: 'org-1', name: 'Acme' } as any;
const owner = { org_id: 'org-1', user_id: 'owner', role: 'admin', user_name: 'Olive', user_email: 'olive@example.com', avatar_url: '' } as const;
const dana = { org_id: 'org-1', user_id: 'dana', role: 'member', user_name: 'Dana', user_email: 'dana@example.com', avatar_url: '' } as const;

const refusal = (status: number, error: string) =>
  Object.assign(new Error(`Request failed with status code ${status}`), { response: { status, data: { error } } });

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.resetAllMocks();
  api.invitations.list.mockResolvedValue({ data: [] } as any);
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const flush = async () => {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const render = async () => {
  await act(async () => {
    root.render(<OrgMembersTab org={org} isAdmin currentUser={{ id: 'owner' } as any} />);
  });
  await flush();
};

const row = (name: string) =>
  Array.from(container.querySelectorAll('tbody tr')).find((tr) => (tr.textContent || '').includes(name));

const alertText = () => container.querySelector('[role="alert"]')?.textContent || '';

const clickRemove = async (name: string) => {
  const button = Array.from(row(name)!.querySelectorAll('button')).find((b) => b.textContent === 'Remove')!;
  await act(async () => {
    button.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
  await flush();
};

const chooseRole = async (name: string, role: string) => {
  const select = row(name)!.querySelector('select') as HTMLSelectElement;
  await act(async () => {
    select.value = role;
    select.dispatchEvent(new Event('change', { bubbles: true }));
  });
  await flush();
};

describe('OrgMembersTab on a stale member list', () => {
  it('drops a member someone else already removed and names them, not the admin', async () => {
    api.members.list
      .mockResolvedValueOnce({ data: [owner, dana] } as any)
      .mockResolvedValueOnce({ data: [owner] } as any);
    api.members.remove.mockRejectedValue(refusal(400, 'you are not a member of this organization'));
    await render();

    await clickRemove('Dana');

    expect(api.members.remove).toHaveBeenCalledWith('org-1', 'dana');
    expect(container.textContent).not.toContain('you are not a member');
    expect(alertText()).toBe('');
    expect(container.textContent).toContain('Dana is no longer a member of this workspace.');
    expect(row('Dana')).toBeUndefined();
  });

  it('says a role change failed because the member has left, and drops the row', async () => {
    api.members.list
      .mockResolvedValueOnce({ data: [owner, dana] } as any)
      .mockResolvedValueOnce({ data: [owner] } as any);
    api.members.setRole.mockRejectedValue(refusal(400, 'you are not a member of this organization'));
    await render();

    await chooseRole('Dana', 'admin');

    expect(api.members.setRole).toHaveBeenCalledWith('org-1', 'dana', 'admin');
    expect(alertText()).toBe('Failed to change role: Dana is no longer a member of this workspace.');
    expect(row('Dana')).toBeUndefined();
  });

  it("still shows the server's refusal when the member is still there", async () => {
    api.members.list.mockResolvedValue({ data: [owner, dana] } as any);
    api.members.remove.mockRejectedValue(refusal(400, 'cannot remove the last admin of an organization'));
    await render();

    await clickRemove('Dana');

    expect(alertText()).toBe('Failed to remove member: cannot remove the last admin of an organization');
    expect(row('Dana')).toBeDefined();
  });

  it("shows the server's refusal when the list cannot be re-read", async () => {
    api.members.list
      .mockResolvedValueOnce({ data: [owner, dana] } as any)
      .mockRejectedValueOnce(refusal(500, 'failed to list members'));
    api.members.remove.mockRejectedValue(refusal(400, 'you are not a member of this organization'));
    await render();

    await clickRemove('Dana');

    expect(alertText()).toBe('Failed to remove member: you are not a member of this organization');
    expect(row('Dana')).toBeDefined();
  });
});
