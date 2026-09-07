# OpenV frontend — mobile readiness assessment (2026-09-07)

Scope: `frontend/` at `81030f9` (master after PR #298). 35,511 lines of
TS/TSX across 7 CSS files; styling is overwhelmingly inline `style={{}}`
objects, so almost nothing is reachable by a media query. Companion to
`docs/plans/mobile-support.md`.

**Headline: the app is a desktop-only application.** There is no responsive
layer, no touch layer, and on the production Railway topology no working
sign-in on iOS Safari. It is installable as a PWA, but the installed app is
the same desktop layout.

## 1. Responsive design

Exactly ten `@media` occurrences, six of them responsive, all in leaf
components: `ImageLightbox.css:88`, `ImageGallery.css:145`,
`ProjectList.css:331`, `ImageUploadInput.css:57` (all `max-width: 768px`)
and `HelpSidebar.css:179,189` (1024 and 768 px). No layout container has
one. No component reads the screen width: a grep for `innerWidth`,
`matchMedia`, `ResizeObserver`, `visualViewport` finds only the theme's
`prefers-color-scheme` check.

Fixed-pixel layouts that decide the minimum usable width:

| View | Fixed widths | Minimum before content is legible |
|---|---|---|
| `ProjectLayout.tsx:171-212` | shell `100vh`/`overflow:hidden`; nav `width/minWidth 200`; edge strip 10 px | 200 px before any view |
| `ModuleView.tsx:1084-1750` | tree column 400 px default (`min 200`, `max 800`), notes 320 px (`min 250`), two 10 px resize handles, 40 px padding, `overflow: hidden` | about 460 px with the document pane at zero |
| `GuidedWizard.tsx:1654-1677` + `StepShell.tsx:43` + `GuidedChatPanel.tsx:510` | rail 220 px, chat panel 340 px, no `flexWrap` | 852 px with the nav |
| `KanbanBoard.tsx:325-345` | columns `flex: 0 0 260px`, row scrolls horizontally | one column visible on a phone |
| `TraceabilityMatrix.tsx:226-303` | pinned 240 px column, further `minWidth` 200 to 280 | about 1160 px |
| `CrewBuilder.tsx:669`, `AgentsPage.tsx:138`, `AgentRunsPage.tsx:330`, `ImpactView.tsx:112`, `ManualView.tsx:164-169` | side panels of 320, 320, 460, 320 and 260 px | two-column with no fallback |
| `Login.tsx:72`, `CreateOrgModal.tsx:65`, `WorkItemDrawer.tsx:150` | 380, 400 and 420 px with no `maxWidth` | overflow on an iPhone SE |

The shared `Modal`, `ConfirmDialog` and `PromptDialog` are correctly clamped
to the viewport. Twelve of fifteen tables have fixed-width headers and no
horizontal scroll wrapper. `100vh` is used in about ten places where
`100dvh` is needed.

The `overflow: hidden` shell is REQ-53's fit-the-window contract working as
designed; on a narrow screen it converts every overflow into clipped,
unreachable content rather than a scroll.

## 2. Touch and pointer

- Every drag is HTML5 `draggable` plus `dataTransfer`
  (`ArtifactList.tsx:213-272`, `KanbanBoard.tsx:331-377`); a grep for
  touch or pointer handlers in `src/` finds none. Touch browsers do not
  synthesise these events. Reparenting an artifact is drag-only
  (`utils/artifactDrag.ts:10-13`) and launching an agent from the board is
  drag-only (`KanbanBoard.tsx:320`).
- The Cytoscape crew canvas (`CrewCanvas.tsx:165-340`) is the one exception
  and already handles tap and drag by touch, with 30 px nodes.
- The artifact context menu is right-click only (`ArtifactList.tsx:207-212`)
  and carries seven actions with no other entry point; iOS shows its native
  callout on long-press instead of firing `contextmenu`.
- Panel auto-hide opens on hover (`panelMode.ts:47`), is persisted per
  person, and is recovered through a 10 px strip; on touch it behaves as
  hidden. Figure actions sit in a hover overlay (`ImageGallery.css:43-45`).
  Column resizing binds mouse events only (`ModuleView.tsx:151-197`).
- 151 `title` tooltips never render on touch; several carry the only
  explanation of a control.
- Tap targets: edge strips and resize handles 10 px; tree chevron 14 px;
  download-wizard checkboxes 16 px; nav section headers about 26 px tall;
  most chips `padding: 1px 6px`. Only the help button (60 px) and the
  interview composer (44 px) meet the 44 px guideline. 117 `fontSize`
  values of 11 px or less.

## 3. Viewport and PWA

`index.html` has a correct viewport meta without `user-scalable=no`, a
theme colour, one apple-touch-icon and the manifest link; it lacks
`viewport-fit=cover` and the Apple web-app meta tags. `manifest.json` is
complete: `display: standalone`, `id`, `start_url`, `scope`, maskable and
plain icons at 192 and 512 px; no screenshots or shortcuts, and
`orientation: any` lets an installed phone app launch into a portrait layout
it cannot render. `sw.js` deliberately caches nothing (documented
rationale: live collaborative app), so there is no offline mode, no push,
and no stale-shell risk; registration is inline in `index.tsx:30-37`.

## 4. Navigation

`ProjectLayout` is a permanent 200 px left rail with no drawer, hamburger
or tab bar. `panelMode` is a preference, not a response to width; the
default is pinned. Nothing adapts to the screen.

## 5. Mobile-specific views

`InterviewChat.tsx` is the only phone-first screen: a fluid column capped at
640 px, the standard header, scrolling list and fixed composer, 80 percent
bubbles with word wrapping, a 44 px composer, no cookie (`withCredentials:
false`), so it works on iOS. Its faults are `100vh`, a 14 px textarea
(focus zoom) and a Ctrl-Enter-only shortcut beside the send button.

## 6. Inputs

`index.css:116-125` sets 14 px on every input, textarea and select, so iOS
zooms on focus and stays zoomed; several components go to 12 or 13 px. The
body editor's `#` reference autocomplete is driven by arrow, Enter and Tab
keys (`ArtifactEditor.tsx:196-211`), none of which a soft keyboard offers,
though entries can be tapped. No `capture` attribute on file inputs, which
is correct (the OS offers camera and library). No date inputs exist. No
`inputMode`, `autoCapitalize` or `enterKeyHint`; `autoComplete` only appears
as `off`, so the login form gets no password-manager autofill.

## 7. Authentication on mobile

`api/client.ts:34-41` is cookie-only (`withCredentials: true`) with no
bearer fallback. Production sets `CROSS_SITE_COOKIES=true`, so
`handlers.go:187-192` issues the session as `SameSite=None; Secure`. The two
`up.railway.app` hosts are different registrable sites (the suffix is on the
Public Suffix List), so the cookie is third-party. Safari blocks third-party
cookies unconditionally and every iOS browser is WebKit: sign-in appears to
succeed, the next request answers 401, and the response interceptor
(`client.ts:70-79`) sends the user back to the login page. The cookie is not
`Partitioned` (Go's `http.Cookie` field is unused), Safari also caps
`SameSite=None` lifetimes, and the three `EventSource` streams inherit the
same problem. The public interview route is the one flow that works.
Self-hosted deployments on one domain get `SameSite=Lax` and are unaffected.

## 8. Performance

No client-side PDF or DOCX libraries (exports are server-side). Routes are
code-split well (`App.tsx:28-59`): the 1.1 MB ag-grid chunk and the 434 KB
Cytoscape chunk stay off the first load. The main bundle is 616 KB
uncompressed and first render waits on three sequential API calls. The
navbar logo is a 180 KB PNG drawn at 56 px; gallery thumbnails load
full-resolution attachments; no `loading="lazy"` or `srcSet` anywhere; gzip
on, brotli off; source maps are shipped.

## 9. Accessibility that affects mobile

Pinch-zoom is preserved; the token system supports dark mode and
`prefers-color-scheme`; body text contrast is strong. The muted text token
(`#7f8c8d` on white) is about 3.6:1, below AA, and it is used for much of
the small secondary text; `--neutral` on white is about 2.6:1. No
`prefers-reduced-motion` handling; the board's live-run indicator pulses
indefinitely.

## 10. Tests

`e2e/playwright.config.ts` has one project, Desktop Chrome. No WebKit, no
device profile, no viewport override. Nothing in CI exercises the engine
where the cookie blocker lives.

## Ranked obstacles

Phone: (1) sign-in impossible on iOS; (2) no responsive layer; (3) the
requirements workspace clips at any width under about 460 px; (4) all drag
dead on touch with no alternative for reparenting or handing a card to an
agent; (5) right-click-only artifact menu; (6) focus zoom on every field;
(7) the login card overflows small phones; (8) 10 to 24 px tap targets on
the controls that recover panels and expand branches; (9) auto-hide panel
mode is a trap on touch; (10) the guided wizard needs 852 px.

Tablet: (1) the iOS cookie blocker; (2) drag still dead, on a layout that
otherwise fits; (3) right-click and hover-only actions; (4) portrait breaks
the three-column views and the manifest allows portrait; (5) column
resizing and panel recovery are mouse-only.
