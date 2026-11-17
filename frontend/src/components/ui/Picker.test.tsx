import React, { act, useState } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { useAppStore } from '../../state/store';
import { Picker, PickerOption } from './Picker';
import { buildRows, matchRange, typeAhead } from './pickerModel';

// The editor's dropdown: opening lists every option whatever the field holds
// (the browser's suggestion list hid all but the typed value), typing narrows
// it, free text can be used as typed, and without the gate the native control
// stays.

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const OWNERS: PickerOption[] = [
  { value: 'Rivelin Robotics', label: 'Rivelin Robotics', group: 'Parties' },
  { value: 'Positioner Supplier', label: 'Positioner Supplier', group: 'Parties' },
  { value: 'Robot Supplier', label: 'Robot Supplier', group: 'Parties' },
  { value: 'David Mason', label: 'David Mason', group: 'Members' },
];
const TYPES: PickerOption[] = ['Heading', 'Requirement', 'Test Case', 'Hazard'].map((l) => ({ value: l.toLowerCase(), label: l }));

describe('pickerModel', () => {
  it('lists every option with no query, the none row first', () => {
    const rows = buildRows(OWNERS, '', { noneLabel: 'Not set' });
    expect(rows.map((r) => r.value)).toEqual(['', ...OWNERS.map((o) => o.value)]);
  });
  it('matches anywhere in the label or keywords, and drops the none row', () => {
    expect(buildRows(OWNERS, 'sup', { noneLabel: 'Not set' }).map((r) => r.value)).toEqual(['Positioner Supplier', 'Robot Supplier']);
    expect(buildRows([{ value: 'a', label: 'Lift', keywords: 'REQ-11' }], 'req-11', {}).length).toBe(1);
  });
  it('offers what was typed when free text is on and nothing is called that', () => {
    expect(buildRows(OWNERS, 'Acme', { freeText: true }).map((r) => r.kind)).toEqual(['create']);
    expect(buildRows(OWNERS, 'robot supplier', { freeText: true }).map((r) => r.kind)).toEqual(['option']);
  });
  it('finds the match to underline, and jumps by first letter with wrap', () => {
    expect(matchRange('Robot Supplier', 'sup')).toEqual([6, 9]);
    expect(matchRange('Robot', 'x')).toBeNull();
    const rows = buildRows(TYPES, '', {});
    expect(typeAhead(rows, 0, 'h')).toBe(3);
    expect(typeAhead(rows, 3, 'h')).toBe(0);
    expect(typeAhead(rows, 0, 'z')).toBe(-1);
  });
});

describe('Picker', () => {
  let container: HTMLDivElement;
  let root: Root;
  const setGate = (on: boolean) =>
    useAppStore.setState({
      features: { channel: 'nightly', stable_release: '', preview: false, features: { 'editor-pickers': on } },
    } as any);

  beforeEach(() => {
    setGate(true);
    container = document.createElement('div');
    document.body.appendChild(container);
    act(() => { root = createRoot(container); });
  });
  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  const Harness: React.FC<{ initial: string; options: PickerOption[]; freeText?: boolean; noneLabel?: string; spy?: (v: string) => void }> = ({ initial, spy, ...rest }) => {
    const [value, setValue] = useState(initial);
    return <Picker id="f" value={value} onChange={(v) => { setValue(v); spy?.(v); }} {...rest} />;
  };
  const render = (el: React.ReactElement) => act(() => root.render(el));
  const field = () => container.querySelector('#f') as HTMLElement;
  const options = () => Array.from(container.querySelectorAll('[role="option"]'));
  const key = (el: Element, k: string) => act(() => { el.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true })); });
  const click = (el: Element) => act(() => { el.dispatchEvent(new MouseEvent('click', { bubbles: true })); });
  const type = (el: HTMLInputElement, text: string) => act(() => {
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
    setter.call(el, text);
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });

  it('opens on every owner when the field already holds one, with it ticked', () => {
    render(<Harness initial="Robot Supplier" options={OWNERS} freeText />);
    click(field());
    expect(options().map((o) => o.getAttribute('aria-label'))).toEqual(OWNERS.map((o) => o.label));
    expect(container.querySelector('[aria-selected="true"]')?.getAttribute('data-value')).toBe('Robot Supplier');
    expect(container.textContent).toContain('Parties');
    expect(container.textContent).toContain('Members');
  });

  it('narrows as you type, and keeps a name nobody is called', () => {
    const spy = vi.fn();
    render(<Harness initial="" options={OWNERS} freeText spy={spy} />);
    click(field());
    type(field() as HTMLInputElement, 'Acme Fixtures');
    expect(options().map((o) => o.getAttribute('aria-label'))).toEqual(['Use “Acme Fixtures”']);
    key(field(), 'Enter');
    expect(spy).toHaveBeenLastCalledWith('Acme Fixtures');
    expect(field().getAttribute('aria-expanded')).toBe('false');
  });

  it('chooses with the arrow keys and Enter, and closes with Escape', () => {
    const spy = vi.fn();
    render(<Harness initial="requirement" options={TYPES} spy={spy} />);
    const trigger = field();
    expect(trigger.tagName).toBe('BUTTON');
    key(trigger, 'ArrowDown');
    expect(trigger.getAttribute('aria-expanded')).toBe('true');
    expect(trigger.getAttribute('aria-activedescendant')).toBe('f-opt-1');
    key(trigger, 'ArrowDown');
    key(trigger, 'Enter');
    expect(spy).toHaveBeenLastCalledWith('test case');
    key(trigger, 'ArrowDown');
    key(trigger, 'Escape');
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
  });

  it('offers Not set on an optional field and clears it', () => {
    const spy = vi.fn();
    render(<Harness initial="hazard" options={TYPES} noneLabel="Not set" spy={spy} />);
    click(field());
    click(options()[0]);
    expect(spy).toHaveBeenLastCalledWith('');
    expect(field().textContent).toContain('Not set');
  });

  it('adds a search box past eight options', () => {
    const many = Array.from({ length: 12 }, (_, i) => ({ value: `v${i}`, label: `Option ${i}` }));
    render(<Harness initial="v3" options={many} />);
    click(field());
    const search = container.querySelector('input[aria-label="Search options"]') as HTMLInputElement;
    expect(search).not.toBeNull();
    type(search, 'Option 1');
    expect(options().map((o) => o.getAttribute('data-value'))).toEqual(['v1', 'v10', 'v11']);
  });

  it('says why an empty list is empty', () => {
    render(<Harness initial="" options={[]} freeText />);
    click(field());
    expect(container.textContent).toContain('Nothing to suggest yet. Type a name to use it.');
  });

  it('keeps the native select, with the same id, without the gate', () => {
    setGate(false);
    render(<Harness initial="requirement" options={TYPES} />);
    expect(field().tagName).toBe('SELECT');
    expect((field() as HTMLSelectElement).value).toBe('requirement');
  });
});
