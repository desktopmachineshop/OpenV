import React from 'react';
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { mockApi } from '../../test/mockApi';
import { orgsAPI, planAPI } from '../../api/client';
import { OrgPlanCalendarTab } from './OrgPlanCalendarTab';

vi.mock('../../api/client', async (orig) => mockApi(await orig()));

// The workspace's plan calendar: an admin sets the working days and anyone's
// time off; a member sees the days and records only their own.

const org = { id: 'org1', name: 'Shop' } as any;
let container: HTMLDivElement;
let root: Root;
const flush = () => act(async () => { await Promise.resolve(); await Promise.resolve(); });
const mount = async (isAdmin: boolean) => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => root.render(<OrgPlanCalendarTab org={org} isAdmin={isAdmin} currentUserId="u2" />));
  await flush();
};

beforeEach(() => {
  (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
  vi.clearAllMocks();
  vi.mocked(planAPI.workspaceCalendar).mockResolvedValue({
    data: { working_days: [1, 2, 3, 4, 5], time_off: [{ id: 't1', type: 'user', assignee_id: 'u1', start: '2027-08-02', end: '2027-08-13', name: 'Summer' }] },
  } as any);
  vi.mocked(orgsAPI.members.list).mockResolvedValue({ data: [{ user_id: 'u1', user_name: 'Ann' }, { user_id: 'u2', user_name: 'Bo' }] } as any);
  for (const fn of [planAPI.setWorkspaceWorkingDays, planAPI.addTimeOff, planAPI.removeTimeOff]) vi.mocked(fn as any).mockResolvedValue({ data: {} } as any);
});
afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe('the workspace plan calendar', () => {
  it('lets an admin set the working days and remove anyone’s time off', async () => {
    await mount(true);
    expect(container.textContent).toContain('Ann');
    expect(container.textContent).toContain('Summer');
    await act(async () => (container.querySelector('button[aria-label="Saturday"]') as HTMLButtonElement).click());
    await flush();
    expect(planAPI.setWorkspaceWorkingDays).toHaveBeenCalledWith('org1', [1, 2, 3, 4, 5, 6]);
    await act(async () => (container.querySelector('button[aria-label="Remove the time off of Ann"]') as HTMLButtonElement).click());
    await flush();
    expect(planAPI.removeTimeOff).toHaveBeenCalledWith('org1', 't1');
  });

  it('shows a member the days, and offers only their own time off', async () => {
    await mount(false);
    expect((container.querySelector('button[aria-label="Saturday"]') as HTMLButtonElement).disabled).toBe(true);
    expect(container.querySelector('button[aria-label="Remove the time off of Ann"]')).toBeNull();
    const who = container.querySelector('select[aria-label="Who is away"]') as HTMLSelectElement;
    expect(who.disabled).toBe(true);
    expect(who.value).toBe('u2');
  });
});
