import React, { act, useState } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { ChipSearch } from './ChipSearch';
import { EMPTY_SEARCH, SearchSchema, SearchValue } from './searchValue';
import type { SavedSearchesController } from './useSavedSearches';

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const schema: SearchSchema = {
  fields: [
    {
      key: 'status',
      label: 'Status',
      type: 'enum',
      options: [
        { value: 'draft', label: 'Draft' },
        { value: 'in_review', label: 'In review' },
      ],
    },
    { key: 'owner', label: 'Owner', type: 'person', options: [{ value: 'dave', label: 'Dave' }] },
    { key: 'title', label: 'Title', type: 'text' },
  ],
  quickFilters: [{ key: 'mine', label: 'Mine' }],
  groupBy: [{ key: 'status', label: 'Status' }],
  savedScope: 'test',
};

let latest: SearchValue = EMPTY_SEARCH;

const Harness: React.FC<{ initial?: SearchValue; saved?: SavedSearchesController }> = ({ initial = EMPTY_SEARCH, saved }) => {
  const [value, setValue] = useState(initial);
  latest = value;
  return (
    <ChipSearch
      schema={schema}
      value={value}
      onChange={(v) => {
        latest = v;
        setValue(v);
      }}
      ariaLabel="Search this project"
      placeholder="Search…"
      saved={saved}
      quickCounts={{ mine: 3 }}
      optionCounts={{ status: { draft: 5 } }}
    />
  );
};

describe('ChipSearch', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    latest = EMPTY_SEARCH;
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  const render = (el: React.ReactElement) => act(() => root.render(el));
  const input = () => container.querySelector('input[role="combobox"]') as HTMLInputElement;
  const type = (text: string) => {
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
    act(() => {
      setter.call(input(), text);
      input().dispatchEvent(new Event('input', { bubbles: true }));
    });
  };
  const key = (el: Element, k: string, extra: KeyboardEventInit = {}) =>
    act(() => {
      el.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true, ...extra }));
    });
  const click = (el: Element | null) =>
    act(() => {
      (el as HTMLElement).click();
    });
  const byLabel = (label: string) => container.querySelector(`[aria-label="${label}"]`);
  const byText = (role: string, text: string) =>
    Array.from(container.querySelectorAll(`[role="${role}"]`)).find((el) => el.textContent?.includes(text)) ?? null;

  const twoChips: SearchValue = {
    chips: [
      { kind: 'filter', field: 'status', values: ['in_review'], match: 'is' },
      { kind: 'group', key: 'status' },
    ],
    text: '',
  };

  it('shows each chip with a labelled remove button, and removes one at a time', () => {
    render(<Harness initial={twoChips} />);
    const chips = container.querySelectorAll('.ov-search-chip');
    expect(Array.from(chips).map((c) => c.textContent)).toEqual(['StatusIn review×', 'Group byStatus×']);
    click(byLabel('Remove filter Status: In review'));
    expect(latest.chips).toEqual([{ kind: 'group', key: 'status' }]);
    expect(byLabel('Remove grouping Group by: Status')).not.toBeNull();
  });

  it('takes the last chip off with Backspace in an empty box, and not while there is text', () => {
    render(<Harness initial={{ ...twoChips, text: 'x' }} />);
    key(input(), 'Backspace');
    expect(latest.chips).toHaveLength(2);
    type('');
    key(input(), 'Backspace');
    expect(latest.chips).toEqual([twoChips.chips[0]]);
  });

  it('suggests chips while typing and turns the chosen one into a chip', () => {
    render(<Harness />);
    act(() => input().focus());
    type('dav');
    const options = Array.from(container.querySelectorAll('[role="option"]')).map((o) => o.textContent);
    expect(options).toEqual(['Owner: Dave', 'Search Owner for: dav', 'Search Title for: dav']);
    expect(input().getAttribute('aria-expanded')).toBe('true');
    key(input(), 'ArrowDown');
    key(input(), 'ArrowDown');
    expect(input().getAttribute('aria-activedescendant')).toMatch(/offer-1$/);
    key(input(), 'Enter');
    expect(latest).toEqual({ chips: [{ kind: 'filter', field: 'owner', values: ['dav'], match: 'contains' }], text: '' });
  });

  it('opens a menu of filters, groupings and saved searches from the caret, and Escape closes it', () => {
    const save = vi.fn().mockResolvedValue({ id: 's9', name: 'Mine in review', value: EMPTY_SEARCH, shared: false, mine: true });
    const saved: SavedSearchesController = {
      items: [{ id: 's1', name: 'Release 1.4', value: { chips: [{ kind: 'quick', key: 'mine' }], text: '' }, shared: true, mine: false }],
      loading: false,
      error: '',
      save,
      remove: vi.fn(),
    };
    render(<Harness saved={saved} />);
    const caret = byLabel('Filters, group by and saved searches') as HTMLElement;
    click(caret);
    expect(caret.getAttribute('aria-expanded')).toBe('true');
    const menus = Array.from(container.querySelectorAll('[role="menu"]')).map((m) => m.getAttribute('aria-label'));
    expect(menus).toEqual(['Filters', 'Group by', 'Favourites']);
    expect(document.activeElement?.textContent).toContain('Mine');

    click(byText('menuitemcheckbox', 'Mine'));
    expect(latest.chips).toEqual([{ kind: 'quick', key: 'mine' }]);
    click(byText('menuitem', 'Status'));
    click(byText('menuitemcheckbox', 'Draft'));
    expect(latest.chips[1]).toEqual({ kind: 'filter', field: 'status', values: ['draft'], match: 'is' });
    // A shared search from a colleague lists without a delete button.
    expect(byLabel('Delete saved search Release 1.4')).toBeNull();
    click(byText('menuitemcheckbox', 'Release 1.4'));
    expect(latest.chips).toEqual([{ kind: 'saved', id: 's1', name: 'Release 1.4', value: saved.items[0].value }]);

    key(document.activeElement ?? caret, 'Escape');
    expect(container.querySelector('[role="menu"]')).toBeNull();
    expect(document.activeElement).toBe(caret);
  });

  it('counts each value of a field, and keeps the panel inside the window', () => {
    const rect = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      return (this.classList.contains('ov-chip-search-menu') ? { left: -120, right: 570, top: 0, bottom: 0, width: 690, height: 0 } : { left: 0, right: 0, top: 0, bottom: 0, width: 0, height: 0 }) as DOMRect;
    });
    render(<Harness />);
    click(byLabel('Filters, group by and saved searches'));
    const panel = container.querySelector('.ov-chip-search-menu') as HTMLElement;
    expect(panel.style.transform).toBe('translateX(128px)');
    click(byText('menuitem', 'Status'));
    expect(byText('menuitemcheckbox', 'Draft')?.textContent).toContain('5');
    expect(byText('menuitemcheckbox', 'In review')?.textContent).toContain('0');
    rect.mockRestore();
  });

  it('saves the current search under a name', async () => {
    const save = vi.fn().mockResolvedValue({ id: 's9', name: 'Drafts', value: twoChips, shared: true, mine: true });
    const saved: SavedSearchesController = { items: [], loading: false, error: '', save, remove: vi.fn() };
    render(<Harness initial={twoChips} saved={saved} />);
    click(byLabel('Filters, group by and saved searches'));
    click(byText('menuitem', 'Save current search'));
    const name = byLabel('Name of the saved search') as HTMLInputElement;
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
    act(() => {
      setter.call(name, 'Drafts');
      name.dispatchEvent(new Event('input', { bubbles: true }));
    });
    click(Array.from(container.querySelectorAll('input[type="checkbox"]'))[0]);
    await act(async () => {
      (Array.from(container.querySelectorAll('button')).find((b) => b.textContent === 'Save') as HTMLElement).click();
    });
    expect(save).toHaveBeenCalledWith('Drafts', twoChips, true);
    expect(latest.chips).toEqual([{ kind: 'saved', id: 's9', name: 'Drafts', value: twoChips }]);
  });

  it('adds a custom filter on any field', () => {
    render(<Harness />);
    click(byLabel('Filters, group by and saved searches'));
    click(byText('menuitem', 'Custom filter'));
    const select = byLabel('Field') as HTMLSelectElement;
    act(() => {
      select.value = 'title';
      select.dispatchEvent(new Event('change', { bubbles: true }));
    });
    const valueInput = byLabel('Value') as HTMLInputElement;
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
    act(() => {
      setter.call(valueInput, 'spindle');
      valueInput.dispatchEvent(new Event('input', { bubbles: true }));
    });
    key(valueInput, 'Enter');
    expect(latest.chips).toEqual([{ kind: 'filter', field: 'title', values: ['spindle'], match: 'contains' }]);
  });
});
