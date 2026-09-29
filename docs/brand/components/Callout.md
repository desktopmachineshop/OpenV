# Callout

A tinted block that tells the reader something about the surrounding content: a warning, an agent proposal, an error, a confirmation.

## What the consumer provides
A short title (`ov-callout__title`) naming the situation, one or two sentences on what it means, and at most one action button.

## Variants
`--info`, `--success`, `--warning`, `--danger`, `--agent`. Same meanings as StatusChip.

## Do and don't
- Don't add a coloured left border. The tint and the title carry it.
- Don't stack more than two callouts; the second one usually belongs in the review queue.
