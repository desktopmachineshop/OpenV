// Honouring the line breaks a writer typed.
//
// Markdown folds a single newline into a space, so a description written as
// separate lines rendered as one run-on paragraph and the writer's structure
// was silently lost. Requirement text is written in lines that matter.
//
// This lives apart from the component that uses it because react-markdown is
// ESM-only and cannot be imported by the test runner; the logic that decides
// what happens to the text is the part worth testing.

/**
 * Split a text node's single newlines into hard breaks.
 *
 * Markdown folds a single newline into a space, so a description typed as
 * separate lines rendered as one paragraph — the writer's line breaks were
 * silently thrown away. Requirement text is written in lines that matter, so
 * they are honoured here. Exported for its test; a paragraph break (a blank
 * line) still starts a new paragraph as markdown intends.
 */
export const splitSoftBreaks = (value: string): any[] => {
  const parts = value.split('\n');
  if (parts.length === 1) return [{ type: 'text', value }];
  const out: any[] = [];
  parts.forEach((part, i) => {
    if (i > 0) out.push({ type: 'break' });
    if (part !== '') out.push({ type: 'text', value: part });
  });
  return out;
};

/**
 * remark plugin turning single newlines into breaks. It walks the tree rather
 * than pulling in a visitor dependency, and only rewrites `text` nodes — code
 * blocks and inline code carry their own value and keep their formatting.
 */
export const remarkSoftBreaks = () => (tree: any) => {
  const walk = (node: any) => {
    if (!node || !Array.isArray(node.children)) return;
    const next: any[] = [];
    for (const child of node.children) {
      if (child?.type === 'text' && typeof child.value === 'string' && child.value.includes('\n')) {
        next.push(...splitSoftBreaks(child.value));
      } else {
        walk(child);
        next.push(child);
      }
    }
    node.children = next;
  };
  walk(tree);
};
