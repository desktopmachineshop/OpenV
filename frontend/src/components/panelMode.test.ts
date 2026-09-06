import {
  PanelMode,
  loadPanelMode,
  nextPanelMode,
  panelIsOpen,
  panelTakesSpace,
  savePanelMode,
} from './panelMode';

describe('nextPanelMode', () => {
  it('cycles pinned → auto-hide → hidden → pinned', () => {
    expect(nextPanelMode('pinned')).toBe('autohide');
    expect(nextPanelMode('autohide')).toBe('hidden');
    expect(nextPanelMode('hidden')).toBe('pinned');
  });
});

describe('panelIsOpen', () => {
  it('is always open when pinned, hover or not', () => {
    expect(panelIsOpen('pinned', false)).toBe(true);
    expect(panelIsOpen('pinned', true)).toBe(true);
  });

  it('opens on hover only when auto-hiding', () => {
    expect(panelIsOpen('autohide', false)).toBe(false);
    expect(panelIsOpen('autohide', true)).toBe(true);
  });

  it('stays shut when hidden, even under the pointer', () => {
    expect(panelIsOpen('hidden', true)).toBe(false);
  });

  // The bug this guards: the edge strip is the only way back from hidden, and
  // hover deliberately does not open a hidden panel, so without an explicit
  // reveal clicking the strip did nothing at all.
  it('opens a hidden panel when the reader asks for it back', () => {
    expect(panelIsOpen('hidden', false, true)).toBe(true);
  });

  it('reveals an auto-hiding panel too, for a click where there is no hover', () => {
    expect(panelIsOpen('autohide', false, true)).toBe(true);
  });

  it('leaves a pinned panel open whatever the reveal says', () => {
    expect(panelIsOpen('pinned', false, false)).toBe(true);
  });
});

// An auto-hiding panel overlays the document. Reflowing the text every time
// the pointer drifts past the edge would be worse than leaving it shut.
describe('panelTakesSpace', () => {
  it('only a pinned panel takes width from the document', () => {
    expect(panelTakesSpace('pinned')).toBe(true);
    expect(panelTakesSpace('autohide')).toBe(false);
    expect(panelTakesSpace('hidden')).toBe(false);
  });
});

describe('loadPanelMode / savePanelMode', () => {
  beforeEach(() => localStorage.clear());

  it('round-trips a chosen mode per panel', () => {
    savePanelMode('nav', 'hidden');
    savePanelMode('notes', 'autohide');
    expect(loadPanelMode('nav')).toBe('hidden');
    expect(loadPanelMode('notes')).toBe('autohide');
  });

  it('defaults to pinned, so nothing moves for someone who never chose', () => {
    expect(loadPanelMode('nav')).toBe('pinned');
  });

  it('falls back rather than trusting a stored value it does not know', () => {
    localStorage.setItem('openv-panel-mode-nav', 'sideways');
    expect(loadPanelMode('nav')).toBe('pinned');
    expect(loadPanelMode('nav', 'autohide' as PanelMode)).toBe('autohide');
  });
});
