# OpenV brand

OpenV is where engineering teams keep what a product must do, why, and the proof that it does. The brand should feel like a good instrument: precise, calm, legible at a glance, and trustworthy enough to sign off against. One palette, one type family and one set of components serve both the app and the website, so a screenshot on the website always looks like the product people will open.

## Principles

1. **Proof over polish.** Show real refs, real results, real screens. Never draw a fake UI or invent a number.
2. **One accent.** `primary` blue is the only action colour. Everything else is neutral unless it reports a state.
3. **Colour reports state, words confirm it.** Status colours appear only in chips and callouts, and always beside the state's name.
4. **Agents are visible.** Anything an AI agent did or proposes wears `agent`, so a reviewer can always tell machine work from human work.
5. **Dense where people work, open where people decide.** The app is compact (13 to 14px, 32px rows); the website breathes (18px lead, 96px sections). Same tokens, different spacing.

## Content fundamentals

- **Voice:** plain engineering English. Say what a thing does: "Test runs record results against test cases." No hype words (seamless, revolutionise, unleash).
- **Casing:** sentence case for headings, buttons, tabs and menu items: "Capture baseline", "Review queue".
- **Person:** "you" and "your team". The product is "OpenV". Agents *propose*; people *approve*.
- **Requirements** follow ISO/IEC/IEEE 29148: "The system shall …". Priority is MoSCoW (must, should, could) and lives in a field, never in the sentence.
- **Refs** are written exactly as issued (`REQ-12`, `TC-4`) and set in `ref`.
- **Numbers** carry units and real precision: "24 000 rpm within 2 %", "128 artifacts". No rounded marketing figures.
- **No emoji** in the app or on the site. No em dashes in UI copy; use a full stop or a comma.
- **Errors** say what happened and what to do: "The runner lost its connection after 10 minutes. Re-run the prompt to continue."

## Colour

- **Ground and surfaces.** Pages sit on `bg`. Content sits on `surface` with a 1px `border`. Use `surface-sunken` for table headers, code and tracks, `surface-hover` for hover, `surface-selected` with `text-selected` for the current row or nav item.
- **Text.** `text` for primary copy and headings, `text-secondary` for long passages, `text-muted` for metadata and helper text. All three pass 5:1 or better on every surface in both themes.
- **Action.** `primary` fills the one primary button per view, the selected tab underline and nothing else. Labels on it use `text-on-primary` (white in light, dark ink in dark). Links use `link`.
- **Focus.** Every focusable element shows a 2px solid `focus-ring` outline, offset 2px.
- **State.** `success` (pass, approved), `warning` (blocked, in review, suspect), `danger` (fail, error, destructive), `agent` (AI work), neutral (draft, not run). Each has `-bg` and `-border` companions for chips and callouts. Success is teal and danger is vermilion so the two stay distinguishable without red-green vision, and every chip carries its word.
- **Brand.** `brand-blue` and `brand-slate` are the logo's own colours. Use them in marks, the cover and illustrations, not in UI.
- **Controls.** Input and secondary-button borders use `border-control` (3:1 or better); `border` is for hairlines only.

## Type

- **IBM Plex Sans** for everything, **IBM Plex Mono** for refs, code and IDs. Both are open-source (SIL OFL) and ship as files in this system; load them the same way in the app and on the site.
- **App scale:** `title` 24, `heading` 16, `body` 14, `body-dense` 13, `label` 13/500, `caption` 12.
- **Website scale:** `display` 52 (hero only, two lines max), `display-sm` 36 (section headings), `lead` 18.
- Headings use weight 600. Emphasis inside a heading uses weight, never a second family.
- Running text stops at `measure` (68 characters). Tables use tabular figures.

## Space, shape and depth

- **Spacing** is a 4px grid: `space-1` 4 through `space-24` 96. App pages pad `space-6`; website sections pad `space-24` on desktop and `space-16` on phones.
- **One radius rule:** `radius-md` (6px) for buttons, inputs, panels and cards; `radius-lg` (10px) for overlays and website screenshot frames; `radius-full` for chips and avatars; `radius-sm` for refs and checkboxes.
- **Depth is quiet.** Prefer a `border` to a shadow. `shadow-popover` for menus, `shadow-modal` for dialogs and the hero screenshot.
- **Sizes:** controls 36px (`control-md`), toolbar controls 28px (`control-sm`), touch targets 44px, table and tree rows 32px, sidebar 240px, top bar 56px.

## Iconography

- Use **Phosphor Icons, Regular weight**, at 16px in dense UI and 20px in navigation and on phones, coloured `text-muted` (or `currentColor` inside buttons).
- One icon family only. No emoji, no hand-drawn icons.
- An icon never stands alone as a label except in an icon button with an `aria-label`.
- The logo mark is the blue ring-and-check "O". Use the files in Logos; never redraw or recolour it.

## Imagery

- The website shows the real product: screenshots of the app in this look, at 2x, framed with `radius-lg`, `border` and `shadow-modal`.
- Recordings and screenshots use the Benchtop CNC Mill or OpenV Platform demo projects, never lorem ipsum.
- No stock photography, gradients or decorative blobs.

## Motion

- 120 to 160ms ease on colour and background changes; a 1px press on buttons.
- Panels and sheets slide 200ms. Nothing loops or moves on its own except a running agent's live log.
- Everything respects `prefers-reduced-motion`.

## Using the package

| File | What it is |
| --- | --- |
| [`tokens.json`](tokens.json) | The source of truth: every colour (light and dark), type style, spacing step, radius, shadow and size, each with a usage note. |
| [`components.css`](components.css) | The `ov-` component classes: button, chip, ref, field, callout, panel, table, tabs, segmented control, nav item. Every value is a token. |
| [`app.md`](app.md) | How the brand applies inside the product. |
| [`website.md`](website.md) | How the brand applies on the public site. |
| [`migration.md`](migration.md) | Old `theme.css` variables and classes, and what replaces each. |
| [`components/`](components/) | One page per component: when to use it, what the consumer provides, do and don't. |

- **Tokens in code.** `scripts/brand/build_tokens.py` turns `tokens.json` into `frontend/src/brand/tokens.css`: one custom property per token (`--primary`, `--text-muted`, `--space-4`), defined on `.ov-brand` (the public site root) until the app migrates, with the dark theme under `[data-theme="dark"]` and `prefers-color-scheme`, and aliases for the old `theme.css` names, and the IBM Plex `@font-face` rules. Never edit the generated file; edit `tokens.json` and run the script.
- **Fonts** ship in `frontend/public/fonts/` with their licence (SIL OFL 1.1).
- **Components in code.** `frontend/src/brand/components.css` is a copy of `components.css`; React components render its classes rather than inline colours.
- **Changing the brand:** change `tokens.json` here first, regenerate, and update the usage notes. Never add a hex value in a component.
- A browsable copy of this package, with live previews of every component, is kept as the *OpenV Brand* design system artifact.
