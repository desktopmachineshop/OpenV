import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { useHelpActivity, type HelpActivity } from './useHelpActivity';

// The help panel's activity signals: data-help-key and data-help-pane on the
// page, read from focus and presses, never from the help UI, cleared with
// the page.

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let seen: HelpActivity = {};
const Probe: React.FC<{ enabled: boolean; page: string }> = ({ enabled, page }) => {
  seen = useHelpActivity(enabled, page);
  return null;
};

describe('useHelpActivity', () => {
  let host: HTMLDivElement;
  let page: HTMLDivElement;
  let root: Root;

  const render = (enabled = true, path = '/p') => act(() => root.render(<Probe enabled={enabled} page={path} />));

  beforeEach(() => {
    host = document.createElement('div');
    page = document.createElement('div');
    page.innerHTML = `
      <div role="tabpanel" data-help-pane="settings.members">
        <section data-help-key="settings.share-links">
          <input id="label">
          <button id="create">Create link</button>
          <span id="text">Share links</span>
        </section>
        <input id="plain">
      </div>
      <button id="tab" role="tab" data-help-pane="settings.general">General</button>
      <button id="bad" data-help-key="not a key">x</button>
      <div data-help-ui=""><button id="help">?</button></div>`;
    document.body.append(host, page);
    root = createRoot(host);
    seen = {};
  });
  afterEach(() => {
    act(() => root.unmount());
    host.remove();
    page.remove();
  });

  const el = (id: string) => page.querySelector<HTMLElement>(`#${id}`)!;

  it('reads focus and pane from focus, and action from a press inside a key', () => {
    render();
    act(() => el('label').focus());
    expect(seen).toEqual({ focus: 'settings.share-links', pane: 'settings.members' });
    act(() => el('create').click());
    expect(seen).toEqual({ focus: 'settings.share-links', action: 'settings.share-links', pane: 'settings.members' });
    // Focus leaving the keyed section clears the focus; action and pane stay.
    act(() => el('plain').focus());
    expect(seen).toEqual({ action: 'settings.share-links', pane: 'settings.members' });
    // A tab names the pane it shows.
    act(() => el('tab').click());
    expect(seen.pane).toBe('settings.general');
  });

  it('does not count a click on text as an action', () => {
    render();
    act(() => el('text').click());
    expect(seen.action).toBeUndefined();
    expect(seen.pane).toBe('settings.members');
  });

  it('ignores the help UI and values that are not keys', () => {
    render();
    act(() => el('label').focus());
    act(() => el('help').focus());
    act(() => el('help').click());
    expect(seen.focus).toBe('settings.share-links');
    act(() => el('bad').click());
    expect(seen.action).toBeUndefined();
  });

  it('starts afresh on a new page, and tracks nothing when disabled', () => {
    render();
    act(() => el('create').click());
    expect(seen.action).toBe('settings.share-links');
    render(true, '/q');
    expect(seen).toEqual({});
    render(false, '/q');
    act(() => el('create').click());
    expect(seen).toEqual({});
  });
});
