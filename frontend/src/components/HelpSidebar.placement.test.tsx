import { readFileSync } from 'fs';
import { join } from 'path';

// Where the ? button lives, and — just as much — where it does not.
//
// It began as a button floating over the bottom-right corner of every page.
// That corner belongs to the page: on a phone it landed on the composer, and
// on a desktop it covered the V&V runs table's Complete and Abort buttons,
// which could not be clicked at all once the table reached the bottom of the
// window. Reserving the corner instead only moved the cost — a dead strip
// along the bottom of every page — so the button now sits in the bar beside
// the notification bell, which is where the other per-person controls are.
//
// Source assertions rather than rendered ones: what is being pinned is which
// element owns the button and that no layout reserves space for a floating
// one, neither of which jsdom lays out.
const read = (file: string) => readFileSync(join(__dirname, file), 'utf8');

describe('the help button sits with the notification bell', () => {
  const layout = read('ProjectLayout.tsx');
  const list = read('ProjectList.tsx');
  const navbar = read('Navbar.tsx');
  const css = read('HelpSidebar.css');

  // Every shell drives the panel itself, so the component never falls back
  // to drawing its own floating button.
  it('is controlled by every shell that mounts the panel', () => {
    expect(layout).toContain('<HelpSidebar open={helpOpen} onOpenChange={setHelpOpen} />');
    expect(list).toContain('<HelpSidebar open={helpOpen} onOpenChange={setHelpOpen} />');
    // The old arrangement handed the uncontrolled component to desktop.
    expect(layout).not.toContain('<HelpSidebar />');
    expect(list).not.toContain('<HelpSidebar />');
  });

  // Beside the bell in each of the three bars: the compact top bar, the
  // desktop sidebar's account row, and the navbar the projects page uses.
  it('is adjacent to the bell in the navbar', () => {
    const helpAt = navbar.indexOf('aria-label="Help"');
    const bellAt = navbar.indexOf('<NotificationBell');
    expect(helpAt).toBeGreaterThan(-1);
    expect(bellAt).toBeGreaterThan(helpAt);
  });

  it('is in both of the project shell bars', () => {
    // Two ? buttons: the compact top bar and the desktop sidebar.
    const matches = layout.match(/aria-label="Help"/g) || [];
    expect(matches).toHaveLength(2);
  });

  // The regression that prompted the move, and the one that the first fix
  // introduced. Neither may come back.
  it('reserves no page space for a floating button', () => {
    expect(css).not.toContain('help-toggle-clearance');
    expect(layout).not.toContain('help-toggle-clearance');
    expect(list).not.toContain('help-toggle-clearance');
  });
});
