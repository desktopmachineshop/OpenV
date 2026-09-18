// J and K, the reading keys.
//
// Bare letters rather than a chord: stepping through a document is something a
// reviewer does a hundred times in a sitting, and a modifier turns that into
// work. The cost of a bare letter is that it must never fire while someone is
// typing, so the guard below is the whole feature — J is only "next" when J
// could not possibly have been meant as the letter J.
//
// Pure, so every way of being mid-typing is tested without a keyboard.

export type ReadingStep = 'next' | 'previous';

/** Fields where a keystroke is text, not a command. */
const TEXT_TAGS = new Set(['INPUT', 'TEXTAREA', 'SELECT', 'OPTION']);

/**
 * Whether the keystroke is being typed into something, in which case no
 * shortcut may claim it. A contenteditable region counts, and so does anything
 * inside one.
 */
export const isTypingTarget = (target: EventTarget | null): boolean => {
  const element = target instanceof Element ? target : null;
  if (!element) return false;
  if (TEXT_TAGS.has(element.tagName)) return true;
  return Boolean(element.closest('[contenteditable]:not([contenteditable="false"])'));
};

/**
 * Whether something is floating over the document: a dialog, a sheet, a
 * confirmation, the figure lightbox.
 *
 * Both the reading keys and the swipe stand down for these. An overlay has the
 * reader's whole attention and usually its own keys, and stepping the document
 * underneath it would change what they come back to. The role selectors catch
 * every shell in components/ui plus the attachment viewer; the class is the
 * lightbox, which is the one overlay that renders inside the document pane.
 */
export const overlayIsOpen = (doc: Document = document): boolean =>
  Boolean(
    doc.querySelector(
      '[role="dialog"], [role="alertdialog"], [aria-modal="true"], .lightbox-backdrop'
    )
  );

export interface KeyEventFacts {
  key: string;
  target: EventTarget | null;
  altKey: boolean;
  ctrlKey: boolean;
  metaKey: boolean;
  shiftKey: boolean;
  /** True while the editor, a dialog or a sheet owns the screen. */
  busy?: boolean;
}

/**
 * The step a keystroke asks for, or null.
 *
 * Refused: any modifier (⌘K is the browser's or an operating system's, and
 * Shift+J is a capital J someone meant to type), a keystroke in a field, and
 * anything at all while the artifact is being edited or a dialog is open —
 * navigating away from a half-written requirement would lose it.
 */
export const readingStepFor = ({
  key,
  target,
  altKey,
  ctrlKey,
  metaKey,
  shiftKey,
  busy,
}: KeyEventFacts): ReadingStep | null => {
  if (busy) return null;
  if (altKey || ctrlKey || metaKey || shiftKey) return null;
  if (isTypingTarget(target)) return null;
  if (key === 'j' || key === 'J') return 'next';
  if (key === 'k' || key === 'K') return 'previous';
  return null;
};
