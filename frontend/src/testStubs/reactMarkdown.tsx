import React from 'react';

/**
 * react-markdown, stood in for under Jest.
 *
 * react-markdown and its remark plugins ship as ESM only, and the CRA test
 * runner cannot import them — the same constraint that keeps the soft-break
 * plugin in a file of its own. Without a stand-in, every test of a component
 * that renders markdown fails at import before a single assertion runs.
 *
 * It renders the markdown source verbatim, which is exactly what these tests
 * need: they assert on the text a bubble contains, not on how it was styled.
 * Nothing here proves markdown renders correctly — that is the real
 * component's job, and it is checked by looking at the built app rather than
 * by a test that would only be asserting against this stub.
 */
const ReactMarkdown: React.FC<{ children?: React.ReactNode }> = ({ children }) => (
  <>{children}</>
);

export default ReactMarkdown;
