# Tabs

Switch between views of the same thing: underline tabs for panels, a segmented control for view modes.

## What the consumer provides
`role="tablist"` with `role="tab"` buttons and `aria-selected`, or a segmented group of buttons with `aria-pressed`.

## Rules
- Underline tabs (`ov-tabs`) for sections of a panel: Notes, Changes, History.
- Segmented (`ov-segmented`) for two to four view modes: Tree / Document / Notes on phones, Matrix / Graph.
- The selected tab's underline is `primary`; nothing else about it changes colour.
