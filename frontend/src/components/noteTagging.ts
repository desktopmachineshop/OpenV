/**
 * Tagging inside a note: "@" and "@@" for people, "#" and "##" for references.
 *
 * Gated as one thing (REQ-137) because it is one thing to learn: a workspace
 * either has the tagging menus in its notes or it does not, and shipping the
 * people half without the references half would be a stranger product than
 * either. Reading is never gated — a note already written with "@@dana" or
 * "##REQ-12" renders for everybody, because a gate that broke existing prose
 * would be worse than no gate.
 */
export const NOTE_TAGGING_FEATURE = 'note-tagging';
