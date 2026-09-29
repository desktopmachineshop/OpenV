# StatusChip

A pill that names a state in words, tinted by what that state means.

## The mapping (fixed across app and website)
| State | Class |
| --- | --- |
| Pass, Completed, Approved, Quality Good | `ov-chip--success` |
| Fail, Aborted, Error, Quality Poor | `ov-chip--danger` |
| Blocked, In review, Suspect link, Quality Fair | `ov-chip--warning` |
| In progress, Running | `ov-chip--info` |
| Anything an agent did or proposes | `ov-chip--agent` |
| Draft, Not run, Superseded, Unknown | `ov-chip` (neutral) |

## What the consumer provides
The state's word, in sentence case. The word is required: colour is never the only signal.

## Do and don't
- Do keep one chip per state per row.
- Don't use white text on a saturated fill. Chips are always tinted with dark text.
- Don't use `--agent` for anything a person did.
