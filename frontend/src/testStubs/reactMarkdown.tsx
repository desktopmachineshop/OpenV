import React from 'react';

/**
 * react-markdown, stood in for under Jest.
 *
 * react-markdown and its remark plugins ship as ESM only, and the CRA test
 * runner cannot import them — the same constraint that keeps the soft-break
 * plugin in a file of its own. Without a stand-in, every test of a component
 * that renders markdown fails at import before a single assertion runs.
 *
 * It renders text verbatim, which is what most of these tests need: they
 * assert on the text a bubble contains, not on how it was styled.
 *
 * It also renders LINKS and HEADINGS, because those are not styling. A
 * reference citation is a markdown link whose href carries a scheme the real
 * renderer would otherwise blank, handed to a `components.a` override that
 * decides whether following it stays inside the app — and a stub that emitted
 * "[#REQ-17](openv-ref:REQ-17)" as text would let that whole path regress
 * silently, which is exactly how it did regress. Headings come along because
 * the rule that keeps "## Interfaces" a heading and "##REQ-99-FIG-2" a
 * citation is only worth stating if something checks it.
 *
 * What it is not: a markdown implementation. Emphasis, lists, tables and code
 * are still passed through as text. Nothing here proves markdown renders
 * correctly — that is the real component's job, checked by looking at the
 * built app.
 */

type UrlTransform = (url: string, key: string, node: unknown) => string;

interface StubProps {
  children?: React.ReactNode;
  urlTransform?: UrlTransform;
  components?: { a?: React.ComponentType<any>; [key: string]: unknown };
  // Accepted and ignored: the plugins are ESM and are stubbed separately.
  remarkPlugins?: unknown;
}

/**
 * react-markdown's own URL sanitiser, reproduced.
 *
 * A component passes this as the fallback inside its own urlTransform, so the
 * stub has to offer it under the same name or the import lands undefined and
 * the test passes for the wrong reason.
 */
const safeProtocol = /^(https?|ircs?|mailto|xmpp)$/i;

export function defaultUrlTransform(value: string): string {
  const colon = value.indexOf(':');
  const questionMark = value.indexOf('?');
  const numberSign = value.indexOf('#');
  const slash = value.indexOf('/');
  if (
    colon === -1 ||
    (slash !== -1 && colon > slash) ||
    (questionMark !== -1 && colon > questionMark) ||
    (numberSign !== -1 && colon > numberSign) ||
    safeProtocol.test(value.slice(0, colon))
  ) {
    return value;
  }
  return '';
}

const LINK = /\[([^\]]*)\]\(([^)\s]*)\)/g;

/** Split one line into text and the links inside it. */
const renderInline = (
  line: string,
  keyPrefix: string,
  urlTransform: UrlTransform,
  Anchor: React.ComponentType<any>
): React.ReactNode[] => {
  const out: React.ReactNode[] = [];
  let last = 0;
  let m: RegExpExecArray | null;
  LINK.lastIndex = 0;
  while ((m = LINK.exec(line)) !== null) {
    if (m.index > last) out.push(line.slice(last, m.index));
    const href = urlTransform(m[2], 'href', null);
    out.push(
      <Anchor key={`${keyPrefix}-a${m.index}`} href={href}>
        {m[1]}
      </Anchor>
    );
    last = m.index + m[0].length;
  }
  if (last < line.length) out.push(line.slice(last));
  return out;
};

const ReactMarkdown: React.FC<StubProps> = ({ children, urlTransform, components }) => {
  const source = typeof children === 'string' ? children : '';
  if (!source) return <>{children}</>;

  const transform: UrlTransform = urlTransform || ((url) => defaultUrlTransform(url));
  const Anchor: React.ComponentType<any> =
    (components?.a as React.ComponentType<any>) ||
    (({ href, children: kids, ...rest }: any) => (
      <a href={href} {...rest}>
        {kids}
      </a>
    ));

  return (
    <>
      {source.split('\n').map((line, i) => {
        const heading = /^(#{1,6})\s+(.*)$/.exec(line);
        if (heading) {
          const Tag = `h${heading[1].length}` as keyof JSX.IntrinsicElements;
          return <Tag key={i}>{renderInline(heading[2], String(i), transform, Anchor)}</Tag>;
        }
        return (
          <React.Fragment key={i}>
            {renderInline(line, String(i), transform, Anchor)}
            {'\n'}
          </React.Fragment>
        );
      })}
    </>
  );
};

export default ReactMarkdown;
