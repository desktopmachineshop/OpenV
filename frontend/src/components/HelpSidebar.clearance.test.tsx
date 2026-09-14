import { readFileSync } from 'fs';
import { join } from 'path';

// The floating ? button is fixed to the bottom-right corner of the viewport,
// so it sits on top of whatever the page puts there — on the V&V dashboard,
// the runs table's Complete and Abort buttons, which could not be clicked at
// all once the table reached the bottom of the window.
//
// These are source assertions rather than rendered ones: the overlap is a
// product of `position: fixed` against a scroll container's real height,
// which jsdom does not lay out. What can be pinned here is the contract —
// the clearance exists, it is at least as tall as the button plus its inset,
// and every place that mounts the button reserves it.
const read = (file: string) => readFileSync(join(__dirname, file), 'utf8');

describe('the floating help button reserves the corner it covers', () => {
  const css = read('HelpSidebar.css');

  // The numbers have to stay related: the clearance is only correct while it
  // covers the button's height plus the gap it is inset by.
  it('reserves at least the button height plus its inset', () => {
    const size = /\.help-toggle\s*\{[^}]*height:\s*(\d+)px/.exec(css);
    const inset = /\.help-toggle\s*\{[^}]*bottom:\s*(\d+)px/.exec(css);
    const clearance = /\.help-toggle-clearance\s*\{[^}]*padding-bottom:\s*(\d+)px/.exec(css);

    expect(size).not.toBeNull();
    expect(inset).not.toBeNull();
    expect(clearance).not.toBeNull();

    const needed = Number(size![1]) + Number(inset![1]);
    expect(Number(clearance![1])).toBeGreaterThanOrEqual(needed);
  });

  // Padding inside the scroll box, not margin outside it: the last row has to
  // be able to scroll clear of the button rather than stop underneath it.
  it('reserves the corner as padding', () => {
    const rule = /\.help-toggle-clearance\s*\{([^}]*)\}/.exec(css);
    expect(rule).not.toBeNull();
    expect(rule![1]).toContain('padding-bottom');
    expect(rule![1]).not.toContain('margin-bottom');
  });

  // Both mount sites, so a new page that carries the button does not quietly
  // reintroduce the overlap.
  it('is applied by the project shell, and only where the button floats', () => {
    const layout = read('ProjectLayout.tsx');
    expect(layout).toContain("compact ? undefined : 'help-toggle-clearance'");
  });

  it('is applied by the projects list, which mounts the same button', () => {
    const list = read('ProjectList.tsx');
    expect(list).toContain('help-toggle-clearance');
  });
});
