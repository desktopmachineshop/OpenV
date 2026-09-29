# On the website

The public site (openv.app: home, How it works, Demos, Pricing, FAQ, Open source) uses the same tokens, type and components as the app, at a more open spacing.

## Page frame
- Nav 56px on `bg` with a bottom `border`: logo lockup left, four to six links in `body` on `text-secondary`, Sign in as a ghost button and one primary signup button on the right. The nav stays on one line on desktop and collapses to a menu under 900px.
- Content column `content-max` (1200px) with `space-12` gutters on desktop and `space-4` on phones.
- Sections pad `space-24` top and bottom on desktop, `space-16` on phones. The whole site stays in the viewer's theme; no section flips to an inverted colour scheme.

## Hero
- Left-aligned split: text in five columns, product screenshot in six.
- Headline in `display`, two lines at most. One `lead` sentence under 20 words. Two buttons: primary "Create free account", secondary "Watch the demos". Nothing else in the hero.
- The screenshot is the real app in this look, framed with `radius-lg`, `border` and `shadow-modal`.

## Sections
- Section headings in `display-sm`, one `lead` sentence below at most. No small uppercase labels above headings.
- Vary the layout from section to section: a feature bento (one large tile with the traceability chain, smaller tiles around it), a demo strip led by one large video, a pricing table, a single-column self-host block. Do not repeat the same grid of equal white cards.
- Pricing uses `ov-panel` columns with the recommended plan's border in `primary`, not a coloured fill.

## Calls to action
- One label per intent across the whole site: "Create free account" for signup, "Watch the demos" for video, "Read the manual" for docs, "View on GitHub" for source.
- The primary button appears once per screen height at most.

## Copy
- Headlines state what OpenV does in the reader's terms. Example: "Requirements, with the proof attached."
- Sub-copy in `lead` under 25 words. Details belong on the page they describe (How it works, FAQ), not the home page.
- Facts about plans and limits come from `frontend/src/landing/content.ts`, the same source the app and manual read.
