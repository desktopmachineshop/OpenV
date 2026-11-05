import { useEffect } from 'react';

// ?notes=1 beside ?artifact= opens the notes panel on it: a comment the
// Home or the activity log links to opens with its thread showing (#718).
export const useNotesParam = (
  notes: boolean,
  artifactId: string | null,
  stacked: boolean,
  openStacked: () => void,
  reveal: () => void
) => {
  useEffect(() => {
    if (!notes || !artifactId) return;
    if (stacked) openStacked();
    else reveal();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [notes, artifactId]);
};
