import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { AgentGlyph } from './AgentGlyph';
import { Modal } from './Modal';
import { InvitationRows, MemberRows } from '../org/OrgMembersRows';
import type { OrgInvitation, OrgMember } from '../../api/client';
import { useAppStore } from '../../state/store';

// The last pieces of the refreshed interface (ui-refresh) on a phone: a form
// dialog is a sheet the height of the screen with its own Close button while
// a short dialog stays a card; an agent is marked by the menu's icon in
// place of the robot emoji; and the workspace's members and invitations are
// rows, not a table.

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const initialWidth = window.innerWidth;
let container: HTMLDivElement;
let root: Root;

const gates = (refresh: boolean) =>
  useAppStore.setState({
    features: {
      channel: 'nightly' as const,
      stable_release: '',
      preview: false,
      features: { 'compact-layout': true, 'ui-refresh': refresh },
    },
  } as never);

const mount = async (ui: React.ReactElement, width = 390) => {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: width });
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => root.render(ui));
};

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: initialWidth });
});

const click = (el: Element | null) =>
  act(async () => {
    el!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });

describe('Modal on a phone', () => {
  it('is a full-height sheet with a Close button for a form', async () => {
    gates(true);
    const onClose = vi.fn();
    await mount(
      <Modal title="Record a capture" width={560} onClose={onClose}>
        <input aria-label="Title" />
      </Modal>
    );
    const dialog = container.querySelector<HTMLElement>('[role="dialog"]')!;
    expect(dialog.style.height).toBe('100%');
    expect(dialog.style.borderRadius).toBe('0px');
    expect(dialog.querySelector('h3')?.textContent).toBe('Record a capture');
    await click(dialog.querySelector('button[aria-label="Close dialog"]'));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('keeps a short dialog as a card', async () => {
    gates(true);
    await mount(
      <Modal title="Move" width={420} onClose={() => {}}>
        <p>Where to?</p>
      </Modal>
    );
    const dialog = container.querySelector<HTMLElement>('[role="dialog"]')!;
    expect(dialog.style.height).toBe('');
    expect(dialog.querySelector('button[aria-label="Close dialog"]')).toBeNull();
  });

  it('is a card on a computer, and on a phone without the key', async () => {
    gates(true);
    await mount(
      <Modal title="Record a capture" width={560} onClose={() => {}}>
        <p>x</p>
      </Modal>,
      1280
    );
    expect(container.querySelector('button[aria-label="Close dialog"]')).toBeNull();
    act(() => root.unmount());
    container.remove();
    gates(false);
    await mount(
      <Modal title="Record a capture" width={560} onClose={() => {}}>
        <p>x</p>
      </Modal>
    );
    expect(container.querySelector('button[aria-label="Close dialog"]')).toBeNull();
  });
});

describe('AgentGlyph', () => {
  it('draws the Agents icon with the key and the emoji without it', async () => {
    gates(true);
    await mount(<AgentGlyph />);
    expect(container.querySelector('svg[data-agent-glyph]')).not.toBeNull();
    expect(container.textContent).toBe('');
    act(() => root.unmount());
    container.remove();
    gates(false);
    await mount(<AgentGlyph />);
    expect(container.querySelector('svg')).toBeNull();
    expect(container.textContent).toBe('🤖');
  });
});

describe('Members as rows', () => {
  const members = [
    { user_id: 'u1', user_name: 'Sam Example', user_email: 'sam@example.test', role: 'admin' },
    { user_id: 'u2', user_name: 'Kim Other', user_email: 'kim@example.test', role: 'member' },
  ] as OrgMember[];

  it('lists each member with a role picker and Remove, and Leave for yourself', async () => {
    const onRole = vi.fn();
    const onRemove = vi.fn();
    await mount(<MemberRows members={members} selfId="u1" isAdmin onRole={onRole} onRemove={onRemove} />);
    expect(container.querySelector('table')).toBeNull();
    const items = Array.from(container.querySelectorAll('[role="listitem"]'));
    expect(items).toHaveLength(2);
    expect(items[0].textContent).toContain('Sam Example (you)');
    expect(items[0].querySelector('button')?.textContent).toBe('Leave');
    expect(items[1].querySelector('button')?.textContent).toBe('Remove');
    await click(items[1].querySelector('button'));
    expect(onRemove).toHaveBeenCalledWith(members[1]);
    expect(items[1].querySelector('select')?.getAttribute('aria-label')).toBe('Role of Kim Other');
  });

  it('shows a member their own Leave only, and invitations with Revoke', async () => {
    await mount(<MemberRows members={members} selfId="u2" isAdmin={false} onRole={() => {}} onRemove={() => {}} />);
    expect(container.querySelector('select')).toBeNull();
    expect(Array.from(container.querySelectorAll('button')).map((b) => b.textContent)).toEqual(['Leave']);
    act(() => root.unmount());
    container.remove();
    const onRevoke = vi.fn();
    const inv = { id: 'i1', email: 'new@example.test', role: 'member', expires_at: '2026-12-01T00:00:00Z' } as OrgInvitation;
    await mount(<InvitationRows invitations={[inv]} onRevoke={onRevoke} />);
    await click(container.querySelector('button'));
    expect(onRevoke).toHaveBeenCalledWith(inv);
  });
});
