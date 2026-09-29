# RefBadge

The stable reference of an artifact (REQ-12, TC-4), set in IBM Plex Mono so refs read as identifiers everywhere.

## What the consumer provides
The ref exactly as the platform issues it, uppercase prefix, hyphen, number. Use `<a class="ov-ref">` when it links to the artifact.

## Do and don't
- Do show one ref per artifact. Never both `REQ-3` and `req-3`.
- Don't put a ref in body type. Mono is what makes refs scannable in trees, tables and notes.
