# Button

The only way to trigger an action; one primary button per view, everything else secondary or ghost.

## Variants
| Class | Use |
| --- | --- |
| `ov-btn ov-btn--primary` | The single most important action on the view: New artifact, Approve, Save, Create free account. |
| `ov-btn` | Secondary actions: Capture baseline, Export, Edit. |
| `ov-btn ov-btn--ghost` | Toolbar actions that should recede: Compare, Collapse all. |
| `ov-btn ov-btn--danger` | Destructive actions, placed last or inside the overflow menu. Never filled. |
| `ov-btn--sm` / `ov-btn--lg` | Toolbars and composers / website hero and phone full-width actions. |
| `ov-btn--icon` | Icon-only; must carry `aria-label`. |

## What the consumer provides
A real `<button>` (or `<a>` for navigation), a verb-first label of one to three words, and `disabled` rather than a greyed class.

## Do and don't
- Do say exactly what happens: "Capture baseline", not "Submit".
- Don't colour buttons by meaning. Green "create" and blue "download" is the old look; action colour is always `primary`.
- Don't put Delete beside Edit at the same weight. Put it last, as `--danger`, or in the overflow menu.
