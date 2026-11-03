// A word-by-word diff of two texts, for showing what changed in an artifact
// between two baselines: the longest common subsequence of their words, with
// the whitespace kept on the word before it so the text reads as written.

export type DiffPart = { kind: 'same' | 'del' | 'ins'; text: string };

const words = (s: string): string[] => (s || '').match(/\s*\S+|\s+$/g) || [];

/** Above this many word pairs the texts are shown as a whole replaced. */
const MAX_CELLS = 4_000_000;

export const wordDiff = (before: string, after: string): DiffPart[] => {
  const a = words(before);
  const b = words(after);
  const parts: DiffPart[] = [];
  const push = (kind: DiffPart['kind'], text: string) => {
    if (!text) return;
    // The space before a removed or added word is not part of the change:
    // keep it plain, so the strike-through and underline cover words only.
    // Within a run of removed or added words the spaces between them stay in it.
    const continues = parts[parts.length - 1]?.kind === kind;
    const lead = kind === 'same' || continues ? '' : (text.match(/^\s+/) || [''])[0];
    if (lead && lead !== text) {
      push('same', lead);
      text = text.slice(lead.length);
    }
    const last = parts[parts.length - 1];
    if (last && last.kind === kind) last.text += text;
    else parts.push({ kind, text });
  };
  if (a.length * b.length > MAX_CELLS) {
    push('del', before);
    push('ins', after);
    return parts;
  }
  // lcs[i][j]: the common words of a[i..] and b[j..].
  const lcs: Uint32Array[] = Array.from({ length: a.length + 1 }, () => new Uint32Array(b.length + 1));
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      lcs[i][j] = a[i].trim() === b[j].trim() ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
    }
  }
  let i = 0;
  let j = 0;
  while (i < a.length && j < b.length) {
    if (a[i].trim() === b[j].trim()) {
      push('same', b[j]);
      i++;
      j++;
    } else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
      push('del', a[i++]);
    } else {
      push('ins', b[j++]);
    }
  }
  while (i < a.length) push('del', a[i++]);
  while (j < b.length) push('ins', b[j++]);
  return parts;
};
