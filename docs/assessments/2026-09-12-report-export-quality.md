# OpenV — report export quality assessment (2026-09-12)

Scope: the document-style downloads — the **PDF specification**, the **Word
document**, and the **V&V status PDF** — with the Excel, CSV and ReqIF
downloads checked for the same defects where they apply. Code at `master`
`41e684a`. Prompted by the maintainer's experience: the files open without
error in their applications, but the formatting is poor, tables run across
page breaks, images are missing, and links are broken.

Method: every renderer was read end to end
(`internal/domain/reports/report.go`, `docx_report.go`, `vv_report.go`,
`internal/domain/downloads/download.go`, `internal/domain/exports/`); then
the documents were **rendered and measured** rather than judged from code. A
saved snapshot of the live OpenV Platform project
(`docs/exports/openv-platform-2026-09-07.json`, 241 artifacts, 251 links)
was loaded through the same `buildReportPDF` / `buildReportDOCX` /
`buildVVReportPDF` / `RenderExport` entry points the download handler calls,
with one added "torture" requirement that carries every markdown feature the
editor accepts (GFM table, code fence, numbered and nested lists, task list,
external link, `#REQ-6` and `#REQ-999-FIG-1` citations, Greek, CJK and
arrow characters) plus PNG, JPEG, WebP and SVG figures. The PDFs were
rasterised and inspected page by page and their text, links, drawings and
images measured with PyMuPDF; the DOCX package was opened with python-docx
and its XML counted. LibreOffice was not usable in the session, so Word
pagination is assessed from the OOXML (which is decisive for the points made
here: a table with no `cantSplit` or `tblHeader` splits and drops its header
in every word processor) rather than from a screenshot.

**Headline: the two documents are not the same specification, and neither
is one a reader could hand to an auditor.** The PDF silently drops every
description or heading whose prose is longer than a page (all four
assessments in the live project are absent from it), fails outright when a
project holds a WebP or SVG figure, and draws almost every artifact's table
border through the artifact that follows it. The Word document contains no
figures, no hyperlinks, no bullets and no page numbers, and its tables carry
no keep-together or repeat-header properties. Both flatten the markdown the
editor renders (tables, lists, code, links) to plain text, and the PDF loses
every character outside Windows-1252.

## 1. How a download is produced

There is **one shared preparation path and no intermediate document
format**. Every format is rendered straight from the same in-memory
snapshot; nothing is produced as HTML or Markdown and then converted.

```mermaid
flowchart LR
  W[Download wizard<br/>format + selection] --> H[GET /projects/{id}/download/{format}<br/>download_handlers.go]
  H --> D[downloads.Download<br/>download.go:161]
  D --> L[LoadReportExport<br/>live export JSON or baseline snapshot<br/>report.go:232]
  L --> S[exports.ProjectExport<br/>artifacts · links · attachments · profile]
  S --> A[exports.Apply<br/>sections · types · headings · attachment categories<br/>selection.go:175]
  A --> J[JSON<br/>export.go:257]
  A --> C[CSV<br/>export.go:323]
  A --> X[XLSX excelize<br/>excel.go:85]
  A --> R[ReqIF encoding/xml<br/>reqif.go:317]
  A --> M[buildReportModel<br/>tree · section numbers · qualified titles · link groups<br/>report.go:162]
  M --> P[PDF gofpdf<br/>buildReportPDF report.go:341]
  M --> O[DOCX hand-written OOXML<br/>buildReportDOCX docx_report.go:21]
  P --> Z{attachment<br/>category ticked?}
  O --> Z
  Z -- yes --> B[zip: document + attachments/<br/>download.go:250]
  Z -- no --> F[file]
  S -.-> V[V&V PDF gofpdf<br/>buildVVReportPDF vv_report.go:113<br/>separate route /vv/report]
```

What is shared, and what is not:

| Layer | Shared by | What it does |
|---|---|---|
| Snapshot | all six formats and the V&V PDF | `exports.ProjectExport`: the live export JSON, or a baseline's stored snapshot. The same struct the JSON download serialises. |
| Selection | all six formats | `exports.Apply` narrows artifacts, links and attachment metadata once, so "no headings" means the same thing in a PDF and a CSV (REQ-56). |
| Report model | PDF and DOCX only | `buildReportModel`: parent/child tree, `SectionNumbers`, `qualifiedTitle` ("1.2 Background" / "REQ-12 Title"), deduplicated link groups per artifact. |
| Text | PDF, DOCX and the V&V PDF | `stripMarkdown` (`report.go:37`): a chain of regular expressions that deletes markdown syntax and returns one plain string. This is the whole "conversion". |
| Layout | nothing | The PDF is imperative gofpdf drawing; the DOCX is string-concatenated WordprocessingML; the XLSX is excelize calls. No template, stylesheet, HTML, print CSS or document AST exists. |

The consequence for quality is structural: because the body text is reduced
to a string before either renderer sees it, neither renderer can produce a
table, a list, a code block, emphasis, a hyperlink or an inline figure, and
every fix made in one renderer has to be made again in the other.

## 2. Findings

Severity: **High** = content is lost or the download fails; **Medium** =
the document is materially misleading or unusable for its purpose;
**Low** = polish or process.

| ID | Sev. | Format | Finding | Evidence |
|---|---|---|---|---|
| EXP-1 | **High** | PDF | **Long prose is dropped.** A description or heading whose body does not fit on one page is routed to the "split" path, which renders only the title for headings and nothing at all for descriptions. The body and any figures vanish with no marker. | `report.go:661-700` (`renderArtifactContentWithSplitting` handles only the details-table types). DSC-1, DSC-2, DSC-3 and DSC-4 (8.7–15.8 k characters each) are absent from the rendered PDF; a probe phrase from each is not in the extracted text. |
| EXP-2 | **High** | PDF | **A WebP, SVG, TIFF or BMP figure fails the whole download.** Uploads accept those types; gofpdf supports JPG, PNG and GIF only and latches an error on the document when asked to register anything else. The renderer checks `info == nil` and continues, but `pdf.Output` returns the latched error, so the handler answers 500 "failed to build download". | Allowlist `internal/api/handlers.go:2452-2463`; `report.go:1137-1140`, `:1285-1288`, `:386`. Reproduced: one WebP attachment → `unsupported image type: webp`, 0 bytes; one SVG → `unsupported image type: svg+xml`. A missing file is skipped correctly. |
| EXP-3 | **High** | DOCX | **The Word document contains no images.** The renderer never emits `w:drawing`; the package has no `word/media/` part and its relationships name only `styles.xml`. The wizard describes this format as "the same specification as a .docx". | `docx_report.go:21-115`, rels `:361`; rendered package parts: `[Content_Types].xml`, `_rels/.rels`, `word/_rels/document.xml.rels`, `word/styles.xml`, `word/document.xml` — 0 drawings, 0 media. `downloadSelection.ts:38-41`. |
| EXP-4 | **High** | PDF | **Table borders are drawn through the next artifact.** The outer rectangle of every details table is sized by `calculateArtifactDetailsTableHeight`, which still adds 4 mm per line of a pretty-printed attributes JSON for a row whose renderer was commented out. The cursor moves on by the real content height, so the phantom border (12 mm for a status-only artifact, more with priority and verification attributes) crosses the following heading and its first rows. The same over-estimate feeds the keep-together heuristic, which pushes artifacts to a new page more often than needed. | `report.go:1247-1259` (height) vs `:1064-1088` (renderer removed, comment "No idea what attributes are"). Measured on the 80-page render: **142 of 219 artifact headings sit inside the previous artifact's border**; 76 boxes end more than 8 mm below their last text; **29 of 80 pages are at least 30 % blank at the bottom** (mean 29 %). Page 41: HAZ-7's border passes through HAZ-8's description. |
| EXP-5 | **High** | PDF, DOCX, V&V PDF | **Markdown is flattened, not rendered.** The editor renders GFM (`react-markdown` + `remark-gfm`); the documents receive one plain string. GFM tables come out as raw `\| a \| b \|` pipe rows; numbered lists lose their numbers (`1.` → two spaces); nested bullets lose their indent; code blocks lose fences and indentation (all runs of spaces collapse to one); bold, italic, strikethrough and inline code are erased; `[text](url)` keeps the text and **discards the URL**; `![alt](url)` is deleted; blockquotes and headings become plain lines. | `report.go:37-115`; SPA `ArtifactBody.tsx:33-35`. Torture artifact on PDF page 3 and in `document.xml`: pipe table, ` first numbered`, `- done task`, `fmt.Println` with a single leading space. The live project already carries four bodies with GFM tables and six with numbered lists. |
| EXP-6 | Medium | PDF, DOCX | **Links are broken or absent.** DOCX: no `w:hyperlink`, no `w:bookmarkStart`, no TOC field — traceability targets are plain text. PDF: only traceability rows are clickable; `#REQ-12` and `#REQ-17-FIG-1` citations in bodies (which the SPA linkifies, REQ-52) are plain text; external URLs are dropped by EXP-5; and a link to a **description** artifact jumps to page 1, because `SetLink` is only called where a title is drawn and descriptions have no title. The underlined text in a link row is the group header, not the link. | `docx_report.go:145-190`; `report.go:596-606`, `:1308-1333`. Rendered PDF: 520 internal links, the one pointing at DSC-1 resolves to page 1; `document.xml`: 0 hyperlinks, 0 bookmarks. |
| EXP-7 | Medium | PDF, V&V PDF | **Characters outside Windows-1252 are lost.** gofpdf's core Arial font is single-byte; the translator maps what cp1252 has and renders the rest as `.`. The link-row renderer skips the translator altogether, so a non-Latin title in a traceability row is emitted as raw UTF-8 bytes. | `report.go:347`, `:1326-1329`. Torture title "Ω ≤ 5 µm" renders as ". . 5 µm"; "→ ✓ 日本語" as ". . ...". The live project's bodies already contain `→ ≈ ≤ ⋯`. |
| EXP-8 | Medium | PDF | **Figures are thumbnails with no caption or number.** Inside a details table an image is capped at 35 mm tall; under a heading or description at 90 mm; both are labelled "Image:" only. The figure reference (REQ-50) and original filename are never printed, so a body that cites `REQ-17-FIG-1` cannot be matched to a picture. | `report.go:1398` (`imageMaxHeight = 35.0`), `:644-650`, `:1152`, `:951`. Page 5: a 600×1400 JPEG rendered at 15 × 35 mm. |
| EXP-9 | Medium | DOCX | **Tables have no pagination properties and lists have no bullets.** No `cantSplit` on rows, no `tblHeader` on the traceability header row, no `keepNext` binding the "Traceability" label to its table, no `tblLayout`. Word therefore splits a table's rows across pages and does not repeat the header. `ListBullet` is indentation only: there is no `numbering.xml`, so profile metrics and constraints render as indented lines without bullets. Every body is one paragraph with `<w:br/>` line breaks rather than paragraphs, so blank lines become double breaks and paragraph spacing never applies. | `docx_report.go:280-304`, `:369-384`. Rendered: 422 tables, 0 `tblHeader`, 0 `cantSplit`, 0 `numPr`, 797 `<w:br/>` in 4,129 paragraphs. |
| EXP-10 | Medium | PDF, DOCX | **No page furniture or navigation.** Neither document has a cover page, table of contents, running header, footer or page number. The PDF has no outline/bookmarks and no title or author metadata; the DOCX has no `docProps`. Word's navigation pane works only because headings carry `outlineLvl`. | `report.go` never calls `Bookmark`, `SetTitle`, `SetFooterFunc` or `AliasNbPages`; rendered PDF: 0 outline entries, empty title. |
| EXP-11 | Medium | PDF, DOCX | **V&V status and attributes are absent, contrary to REQ-6.** The requirement says the PDF and Word document "shall include V&V status"; neither renders `verification_status`, `verification_method`, `priority`, `status`, `severity` or any typed attribute (REQ-70). The only per-artifact facts are Reference, Type and Version. | `report.go:1064-1088` (removed), `docx_report.go:119-141`. Rendered text contains no "Priority" and no verification status. |
| EXP-12 | Low | PDF, DOCX | **Output is not deterministic.** Link groups are Go maps and both renderers range over them, so the order of link types in a traceability row differs from one render to the next of the same snapshot. Two consecutive renders of the fixture differ in 54 of 1,367 table rows; a baseline export is therefore not byte-comparable. | `report.go:1344-1372`, `docx_report.go:163-186`. |
| EXP-13 | Low | PDF | **Two layouts for one table.** The "split" path centres Type and Version and draws no left/right borders on link and image rows; the normal path left-aligns and boxes them. Which one a reader sees depends on pagination. Headings are 13 pt at every depth; nesting is shown only by a 6 mm indent that also narrows every table. | `report.go:856,869` (`"CM"`) vs `:1040,1054` (`"L"`); page 5 vs page 21 of the render. |
| EXP-14 | Low | V&V PDF | **Coverage rows truncate instead of wrapping, and show titles without references.** A requirement title longer than the column is cut with "…"; the table has no ref column and the title index uses the bare title, so the row cannot be cited. | `vv_report.go:90`, `:140-143`, `:203`. |
| EXP-15 | Low | XLSX, CSV, ReqIF | The workbook has no hyperlinks between the Links sheet and the artifact sheets, no print titles or page setup; the CSV writes links as `type:<uuid>` while the workbook writes refs; ReqIF carries the raw markdown inside its XHTML `div` (documented, and acceptable for interchange). | `excel.go:232-269`, `export.go:277-316`, `reqif.go:802`. |
| EXP-16 | Low | process | **Nothing tests the rendered result.** `report.go` (1,570 lines) has no test file; the DOCX tests assert substrings of `document.xml`; nothing exercises images, the split path, non-Latin text, page geometry or an unsupported image type. `docs/api-spec.md` documents the legacy `/export` and `/report` routes but not the `/download/*` family the wizard uses. The Generated stamp is server local time with no zone. | `internal/domain/reports/` test files; `docs/api-spec.md:205-243`; `report.go:365`. |

## 3. Per-format picture

**PDF specification** (`report.go`, gofpdf 1.4.2, A4, 15 mm margins,
core Arial). Structurally sound as a file, and the internal traceability
links work for artifacts that have titles. Everything else in the table
above applies. The 80-page render of the live project is roughly 29 % blank
by area, has 142 headings drawn inside a border, no outline, and lacks the
four longest documents in the project.

**Word document** (`docx_report.go`, hand-written OOXML). Opens cleanly and
the heading styles give Word a navigation pane. It is a text-only rendering:
no figures, no links, no bullets, no page numbers, and tables with no
pagination control. Because it is the format people edit and review in, the
missing figures and links are the most visible loss.

**V&V status PDF** (`vv_report.go`). The best of the three: a real grid with
a repeated header on every page and colour-coded rollups. Titles truncate
and carry no reference; the same cp1252 limit applies.

**Excel workbook**. Correct and tested (TC-55): one sheet per type, frozen
bold header, autofilter, refs and section numbers as text. Bodies are raw
markdown, which is right for a spreadsheet. No hyperlinks.

**CSV and ReqIF**. Faithful to their purpose; the CSV's UUID-typed links are
the one inconsistency with the workbook.

## 4. Against the requirements

| Requirement | State |
|---|---|
| REQ-6 *Project download in every format* — "The PDF and the Word document shall include V&V status" | **Not met** (EXP-11). The verification mark rests on TC-55 (Excel) and TC-27 (selection), neither of which looks at document content. |
| REQ-36 *Visible artifact references* — titling each artifact in the PDF and DOCX | Met (TC-13). References are in the headings and the Reference row. |
| REQ-50 *Numbered figures* — a figure is referenced as `REQ-17-FIG-1` and its file is named for it | Met in storage and in the attachment archive; **not carried into the documents** (EXP-3, EXP-8): the PDF labels every figure "Image:", the DOCX has none. |
| REQ-52 *Referencing figures and linked artifacts* — a reference in a rendered description is a link | Met in the SPA only; in the documents it is plain text (EXP-6). |
| REQ-56 *Choosing what a download contains* | Met (TC-27). The selection layer is the strongest part of the pipeline. |
| Wizard copy — PDF: "sections, artifacts, figures and traceability"; DOCX: "the same specification as a .docx" | The DOCX claim is untrue while EXP-3 stands. |

No requirement states what a rendered document must contain beyond
references and V&V status: nothing specifies markdown fidelity, figure
placement and captions, hyperlinks, character coverage, pagination rules or
deterministic output. That gap is filled by the draft requirements recorded
below, so the fixes have something to be verified against.

## 5. Prioritised actions

1. **Stop losing content (EXP-1, EXP-2).** In the split path, render
   heading and description bodies with `MultiCell` under auto page break
   instead of skipping them. Decode WebP with `golang.org/x/image/webp`
   and BMP/TIFF with `x/image` to PNG in memory and register via
   `RegisterImageOptionsReader`; rasterise SVG (`oksvg` + `rasterx`) or,
   failing that, print a placeholder line naming the figure. Never let a
   figure fail the download; clear or check `pdf.Err()` after every image
   call.
2. **Fix the border geometry (EXP-4).** Delete the attributes block from
   `calculateArtifactDetailsTableHeight` (or render the row, see 6), and
   draw the outer rectangle after the content from the real end position.
   Re-measure: the blank-page and heading-inside-border counts above are
   the acceptance numbers.
3. **Put a document model between the text and the renderers (EXP-5,
   EXP-6).** Parse bodies once with `goldmark` (GFM, in the Go ecosystem)
   into blocks — paragraph, heading, list (ordered/unordered, nesting),
   table, code, quote, with inline runs for emphasis, code, links and
   `#REF` citations — and have both renderers walk that tree. This replaces
   `stripMarkdown`, gives the DOCX real tables/lists/hyperlinks and the PDF
   real lists/tables/link annotations, and means the two documents finally
   agree with each other and with the SPA. Resolve `#REQ-12` to an internal
   link and `[text](url)` to an external one in both formats.
4. **Make the Word document whole (EXP-3, EXP-9, EXP-10).** Add
   `word/media/*` parts with `w:drawing` inline pictures scaled to the text
   width; `w:hyperlink` + `w:bookmarkStart` per artifact; `numbering.xml`
   with bullet and decimal definitions; `cantSplit` on rows, `tblHeader` on
   header rows, `keepNext` on labels; a footer with `PAGE` / `NUMPAGES`
   fields and a `TOC \o "1-3"` field; `docProps/core.xml`. Consider
   adopting a maintained OOXML library rather than growing the
   string-builder.
5. **Give the PDF a real typeface and furniture (EXP-7, EXP-8, EXP-10).**
   Embed a UTF-8 TrueType font (DejaVu Sans or Noto) via `AddUTF8Font`,
   drop the cp1252 translator, and apply the same font in link rows. Add
   `Bookmark` per heading, `SetTitle`/`SetAuthor`, a footer with page
   numbers via `AliasNbPages`, and a caption line "Figure REQ-17-FIG-1 —
   original.png" under each image sized to the column width (35 mm is a
   thumbnail, not a figure).
6. **Show V&V status and attributes (EXP-11).** Render the attribute rows
   the details table already has room for: priority, status,
   verification method and status, and typed attributes by their
   definitions. This closes REQ-6's open clause.
7. **Determinism and consistency (EXP-12, EXP-13).** Sort link types and
   targets before rendering; use one table renderer for both the fitted and
   split cases.
8. **Tests and documentation (EXP-16).** Turn the harness used here into a
   suite: render the docs export fixture and assert page count bounds,
   headings-inside-borders = 0, every artifact's body probe present, every
   figure present in both formats, no `.`-substitution for a non-Latin
   probe, byte-identical repeat renders. Document the `/download/*` routes
   in `docs/api-spec.md`.

Actions 1, 2 and 7 are small, local changes to `report.go`. Actions 3 to 5
are one piece of design work and will replace most of the two renderers;
doing 3 first is what makes 4 and 5 tractable.

## 6. Recorded in the OpenV Platform project

- **DSC-6** *Report export quality assessment 2026-09-12* under **HDG-16
  Assessments** holds this report.
- Draft requirements under **HDG-3 Requirements Management Core**, derived
  from **NEED-7** (audit-ready traceability): **REQ-124** *Rendered
  document fidelity*, **REQ-125** *Figures in rendered documents*,
  **REQ-126** *Links and navigation in rendered documents*, **REQ-127**
  *Document pagination and completeness*, **REQ-128** *Character coverage
  and deterministic rendering*.
- **TC-66** *Rendered document fidelity suite* under **HDG-14 Functional**,
  verifying the five, with a **fail** recorded in test run *2026-09-12
  Export quality assessment*; TC-55 and TC-13 recorded as **pass** in the
  same run from `go test ./internal/domain/{exports,downloads,reports}/`.
- Comments on REQ-6 (V&V status clause not met; its verified mark rests on
  tests that do not read document content), REQ-50 and REQ-52 (figure and
  reference behaviour stops at the SPA).
- Baseline *2026-09-12 Report export quality assessment*.

## 7. Resolution (same day)

Actions 1 to 8 were implemented in the change that carries this document:
a document model (`internal/domain/reports/doc`, goldmark) replaces
`stripMarkdown`; both renderers were rewritten over it (embedded UTF-8
fonts, per-row tables, flowing bodies, captioned figures with WebP/BMP/TIFF
decoding and placeholders for the rest, links and bookmarks, contents,
footers, metadata, deterministic ordering, fields and V&V status); the
downloads gained template presets, field toggles and evidence sections; the
cover states the snapshot; workspaces can carry a logo. The fidelity checks
of TC-66 run as `internal/domain/reports/fidelity_test.go`. See
`docs/reports.md`.
