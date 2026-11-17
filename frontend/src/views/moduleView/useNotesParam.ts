import { useEffect } from 'react';

// ?notes=1 beside ?artifact= opens the notes panel on it: a comment the
// Home or the activity log links to opens with its thread showing (#718).
export const useNotesParam = (
  notes: boolean,
  artifactId: string | null,
  stacked: boolean,
  openStacked: (pane: 'document' | 'notes') => void,
  reveal: () => void
) => {
  useEffect(() => {
    if (!artifactId) return;
    // On a phone the panes stack and the tree shows first, so a link to an
    // artifact (from Home, a notification, a shared URL) opens its document,
    // or its notes when it asks for them; otherwise the phone landed on the
    // tree and the artifact looked as if the link had gone nowhere.
    if (stacked) openStacked(notes ? 'notes' : 'document');
    else if (notes) reveal();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [notes, artifactId]);
};
