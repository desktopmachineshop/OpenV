import type { EvidenceCitation } from '../api/client';

/**
 * Recording a result for a case the run already has one for adds a result
 * with an id of its own, and the server moves the citations of the result it
 * supersedes onto the new one. The run grid keys citations by result id, so
 * it moves them the same way rather than showing the case's evidence as gone.
 */
export const moveCitations = (
  citations: Record<string, EvidenceCitation[]>,
  fromResultId: string,
  toResultId: string
): Record<string, EvidenceCitation[]> => {
  const moving = citations[fromResultId];
  if (fromResultId === toResultId || !moving || moving.length === 0) return citations;
  const next = { ...citations };
  delete next[fromResultId];
  next[toResultId] = [
    ...(next[toResultId] || []),
    ...moving.map((c) => ({ ...c, test_result_id: toResultId })),
  ];
  return next;
};
