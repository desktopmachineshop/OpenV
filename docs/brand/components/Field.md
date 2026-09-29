# Field

A labelled input: label above, control, then helper or error text below.

## What the consumer provides
A `<label for>` tied to the control's `id`, the control itself (`input`, `select` or `textarea` with class `ov-input`), and optional helper (`ov-help`) or error (`ov-error`) text. Set `aria-invalid="true"` on the control when it has an error.

## Rules
- Label always visible. Never use the placeholder as the label.
- Error text says what to enter, not just that it is wrong.
- Inputs are `control-md` (36px) on desktop and at least 16px text on phones.
