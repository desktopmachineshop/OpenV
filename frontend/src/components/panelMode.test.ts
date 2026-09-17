import {
  PanelMode,
  loadPanelMode,
  nextPanelMode,
  panelIsOpen,
  panelStripLabel,
  panelStripWidth,
  panelTakesSpace,
  revealAfterModeChange,
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

// Issue #362: the panel used to close the moment its mode changed, taking the
// button that changes the mode with it. Cycling past a mode you did not want
// then meant finding the edge strip before you could try the next one.
describe('revealAfterModeChange', () => {
  it('keeps a panel on screen when the new mode would close it', () => {
    expect(revealAfterModeChange('autohide')).toBe(true);
    expect(revealAfterModeChange('hidden')).toBe(true);
  });

  it('asks for no reveal on pinned, which is open without one', () => {
    expect(revealAfterModeChange('pinned')).toBe(false);
  });

  it('leaves every mode in the cycle reachable without leaving the panel', () => {
    let mode: PanelMode = 'pinned';
    const seen: PanelMode[] = [mode];
    for (let i = 0; i < 3; i += 1) {
      mode = nextPanelMode(mode);
      // The panel is still showing, so the button is still under the pointer.
      expect(panelIsOpen(mode, false, revealAfterModeChange(mode))).toBe(true);
      seen.push(mode);
    }
    expect(seen).toEqual(['pinned', 'autohide', 'hidden', 'pinned']);
  });
});

// Issue #363: a 10px strip with a 12px chevron in it was not findable.
describe('panelStripWidth', () => {
  it('is a target somebody can aim at with a mouse', () => {
    expect(panelStripWidth(false)).toBeGreaterThanOrEqual(24);
  });

  it('is larger again where the pointer is a finger', () => {
    expect(panelStripWidth(true)).toBeGreaterThan(panelStripWidth(false));
  });
});

describe('panelStripLabel', () => {
  it('names the panel, because the strip sits away from it', () => {
    expect(panelStripLabel('the notes panel', false)).toBe('Show the notes panel');
    expect(panelStripLabel('the notes panel', true)).toBe('Hide the notes panel');
  });
});
