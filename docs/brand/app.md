# In the app

How the brand applies inside the product, screen by screen.

## Shell
- Light sidebar on `surface` with a `border` edge, 240px (`sidebar-width`). Groups (Define, Verify, Plan, Agents) labelled in `caption` weight 500 on `text-muted`; the current item uses `surface-selected` and `text-selected`.
- Top bar 56px on `surface`: page name on the left in `heading`, actions on the right with the primary button last, then notifications and the account menu.
- Page ground is `bg`; panels sit on `surface` with `border` and `radius-md`.

## Action hierarchy
- One `ov-btn--primary` per view: New artifact, Approve, Complete run, Save.
- Secondary actions are `ov-btn`; toolbar actions that should recede are `ov-btn--ghost ov-btn--sm`.
- Delete, Abort and Revoke are `ov-btn--danger`, placed last or in the row's overflow menu. Destructive actions always confirm in a dialog that names the thing being removed.

## Requirements screen
- Tree rows 32px: ref badge, title, one status dot only if the chip is hidden for space. Reordering by drag or the row menu, never three buttons per row.
- The document pane shows the ref, type, one status chip and the quality chip once, then the title in `title` and the body in 15px `text-secondary` capped at `measure`.
- Fields sit in a four-column grid of `caption` label over `body` value.
- Suspect links and agent proposals are callouts in the document, not toasts.

## State vocabulary
| Where | State | Token |
| --- | --- | --- |
| Artifact | Draft / In review / Approved / Superseded | neutral / warning / success / neutral |
| Test result | Pass / Fail / Blocked / Not run | success / danger / warning / neutral |
| Test run | In progress / Completed / Aborted | info / success / danger |
| Agent run | Running / Finished / Failed / Cancelled | info / success / danger / neutral |
| Quality | Good / Fair / Poor | success / warning / danger |
| Link | Suspect | warning |
| Anything by an agent | Proposal, drafted, assigned to agent | agent |

## Phone layout
- Under 640px the sidebar becomes a bottom tab bar (Requirements, V&V, Board, Inbox, More) at 56px plus the safe area.
- Touch targets 44px (`touch-target`); inputs at least 16px text so iOS does not zoom.
- Two-pane screens become one pane with a Tree / Document / Notes segmented control. Row actions open a bottom sheet with `radius-lg` top corners.

## Tables and dense data
- `ov-table` inside an `ov-panel`. Header on `surface-sunken`, 40px body rows, a hairline between rows, tabular figures.
- AG Grid maps to the same tokens: background `surface`, header `surface-sunken`, row hover `surface-hover`, borders `border`, text `text`, secondary text `text-muted`.
