import React, { useEffect, useMemo, useState } from 'react';
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { MANUAL_CHAPTERS, ManualChapter, getChapter, headingId } from '../manual';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Flatten react-markdown heading children into plain text for anchor ids. */
const nodeText = (node: React.ReactNode): string => {
  if (node == null || typeof node === 'boolean') return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(nodeText).join('');
  if (React.isValidElement(node)) return nodeText((node.props as any).children);
  return '';
};

/** The h2 headings of a chapter, for the sidebar's in-page TOC. */
const chapterHeadings = (chapter: ManualChapter): { id: string; text: string }[] =>
  chapter.content
    .split('\n')
    .filter((line) => /^## /.test(line))
    .map((line) => {
      const text = line.replace(/^## /, '').trim();
      return { id: headingId(text), text };
    });

interface SearchHit {
  chapter: ManualChapter;
  /** Heading of the matched section ('' = chapter intro). */
  section: string;
  sectionId: string;
  snippet: string;
}

/** Case-insensitive search across all chapters, section by section. */
const searchManual = (query: string): SearchHit[] => {
  const q = query.trim().toLowerCase();
  if (q.length < 2) return [];
  const hits: SearchHit[] = [];
  for (const chapter of MANUAL_CHAPTERS) {
    // Split the chapter into sections at any heading line.
    const lines = chapter.content.split('\n');
    let section = '';
    let body: string[] = [];
    const flush = () => {
      const text = body.join('\n');
      const haystack = (section + '\n' + text).toLowerCase();
      const at = haystack.indexOf(q);
      if (at >= 0) {
        // Build a plain-text snippet around the first match in the body.
        const plain = text
          .replace(/[#*>`|]/g, '')
          .replace(/\[(.*?)\]\(.*?\)/g, '$1')
          .replace(/\s+/g, ' ')
          .trim();
        const idx = plain.toLowerCase().indexOf(q);
        const start = Math.max(0, (idx >= 0 ? idx : 0) - 70);
        const end = Math.min(plain.length, (idx >= 0 ? idx + q.length : 0) + 110);
        const snippet =
          (start > 0 ? '…' : '') + plain.slice(start, end).trim() + (end < plain.length ? '…' : '');
        hits.push({
          chapter,
          section,
          sectionId: section ? headingId(section) : '',
          snippet,
        });
      }
      body = [];
    };
    for (const line of lines) {
      const m = /^(#{1,3}) (.*)$/.exec(line);
      if (m) {
        flush();
        section = m[1] === '#' ? '' : m[2].trim();
      } else {
        body.push(line);
      }
    }
    flush();
  }
  return hits;
};

/** Highlight query matches inside a snippet. */
const Highlighted: React.FC<{ text: string; query: string }> = ({ text, query }) => {
  const q = query.trim().toLowerCase();
  if (!q) return <>{text}</>;
  const parts: React.ReactNode[] = [];
  let rest = text;
  let key = 0;
  while (rest.length > 0) {
    const at = rest.toLowerCase().indexOf(q);
    if (at < 0) {
      parts.push(rest);
      break;
    }
    if (at > 0) parts.push(rest.slice(0, at));
    parts.push(
      <mark key={key++} style={{ background: '#fdebd0', padding: '0 1px' }}>
        {rest.slice(at, at + q.length)}
      </mark>
    );
    rest = rest.slice(at + q.length);
  }
  return <>{parts}</>;
};

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

// In-app user manual: sidebar TOC (chapters + in-page headings), client-side
// search across all chapters, and next/prev chapter navigation. Deep-linkable
// as /manual/:chapterSlug (and #heading anchors within a chapter).
export const ManualView: React.FC = () => {
  const { chapterSlug } = useParams<{ chapterSlug: string }>();
  const navigate = useNavigate();
  const location = useLocation();
  const [query, setQuery] = useState('');

  const chapter = (chapterSlug && getChapter(chapterSlug)) || MANUAL_CHAPTERS[0];
  const index = MANUAL_CHAPTERS.findIndex((c) => c.slug === chapter.slug);
  const prev = index > 0 ? MANUAL_CHAPTERS[index - 1] : null;
  const next = index < MANUAL_CHAPTERS.length - 1 ? MANUAL_CHAPTERS[index + 1] : null;

  // Unknown slug → canonicalize to the first chapter's URL.
  useEffect(() => {
    if (chapterSlug && !getChapter(chapterSlug)) {
      navigate('/manual', { replace: true });
    }
  }, [chapterSlug, navigate]);

  // Scroll to the top on chapter change, or to the #anchor if one is present.
  useEffect(() => {
    if (location.hash) {
      const el = document.getElementById(location.hash.slice(1));
      if (el) {
        el.scrollIntoView();
        return;
      }
    }
    document.getElementById('manual-scroll')?.scrollTo(0, 0);
  }, [chapter.slug, location.hash]);

  const results = useMemo(() => searchManual(query), [query]);
  const searching = query.trim().length >= 2;

  const headings = useMemo(() => chapterHeadings(chapter), [chapter]);

  const heading = (Tag: 'h1' | 'h2' | 'h3') => (props: { children?: React.ReactNode }) => {
    const text = nodeText(props.children);
    return <Tag id={headingId(text)}>{props.children}</Tag>;
  };

  const openHit = (hit: SearchHit) => {
    setQuery('');
    navigate(`/manual/${hit.chapter.slug}${hit.sectionId ? `#${hit.sectionId}` : ''}`);
  };

  return (
    <div style={{ display: 'flex', height: '100vh', overflow: 'hidden' }}>
      {/* Sidebar */}
      <aside
        style={{
          width: 260,
          minWidth: 260,
          background: '#2c3e50',
          color: '#ecf0f1',
          display: 'flex',
          flexDirection: 'column',
        }}
      >
        <div style={{ padding: '16px 14px', borderBottom: '1px solid #34495e' }}>
          <div style={{ fontWeight: 700, fontSize: 16 }}>OpenV Manual</div>
          <Link
            to="/projects"
            style={{ fontSize: 12, color: '#95a5a6', textDecoration: 'none' }}
          >
            ← Back to projects
          </Link>
        </div>
        <div style={{ padding: '10px 14px', borderBottom: '1px solid #34495e' }}>
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search the manual…"
            style={{
              width: '100%',
              padding: '7px 10px',
              borderRadius: 4,
              border: '1px solid #34495e',
              background: '#233544',
              color: '#ecf0f1',
              fontSize: 13,
            }}
          />
        </div>
        <nav style={{ flex: 1, overflowY: 'auto', padding: '8px 0' }}>
          {MANUAL_CHAPTERS.map((c, i) => {
            const active = c.slug === chapter.slug && !searching;
            return (
              <React.Fragment key={c.slug}>
                <Link
                  to={`/manual/${c.slug}`}
                  style={{
                    display: 'block',
                    padding: '8px 16px',
                    color: active ? '#fff' : '#bdc3c7',
                    background: active ? '#3498db' : 'transparent',
                    textDecoration: 'none',
                    fontSize: 13.5,
                  }}
                >
                  <span style={{ color: active ? '#d6eaf8' : '#7f8c8d', marginRight: 8 }}>
                    {i + 1}.
                  </span>
                  {c.title}
                </Link>
                {active && headings.length > 0 && (
                  <div style={{ padding: '2px 0 6px' }}>
                    {headings.map((h) => (
                      <Link
                        key={h.id}
                        to={`/manual/${c.slug}#${h.id}`}
                        style={{
                          display: 'block',
                          padding: '4px 16px 4px 38px',
                          color: '#95a5a6',
                          textDecoration: 'none',
                          fontSize: 12,
                        }}
                      >
                        {h.text}
                      </Link>
                    ))}
                  </div>
                )}
              </React.Fragment>
            );
          })}
        </nav>
      </aside>

      {/* Content */}
      <main
        id="manual-scroll"
        style={{ flex: 1, overflowY: 'auto', background: '#f5f6fa' }}
      >
        <div style={{ maxWidth: 860, margin: '0 auto', padding: 24 }}>
          {searching ? (
            <div className="card">
              <h3 style={{ marginBottom: 4 }}>
                {results.length === 0
                  ? 'No results'
                  : `${results.length} result${results.length === 1 ? '' : 's'}`}{' '}
                for "{query.trim()}"
              </h3>
              <p style={{ fontSize: 13, color: '#7f8c8d' }}>
                Click a result to open its chapter.
              </p>
              {results.map((hit, i) => (
                <div
                  key={i}
                  onClick={() => openHit(hit)}
                  style={{
                    border: '1px solid #eee',
                    borderRadius: 4,
                    padding: '10px 12px',
                    marginBottom: 8,
                    cursor: 'pointer',
                    background: '#fff',
                  }}
                >
                  <div style={{ fontSize: 13, fontWeight: 600, color: '#2c3e50' }}>
                    {hit.chapter.title}
                    {hit.section && (
                      <span style={{ color: '#7f8c8d', fontWeight: 400 }}> › {hit.section}</span>
                    )}
                  </div>
                  <div style={{ fontSize: 12.5, color: '#555', marginTop: 4, lineHeight: 1.5 }}>
                    <Highlighted text={hit.snippet} query={query} />
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <>
              <div className="card markdown-content" style={{ padding: '24px 32px' }}>
                <ReactMarkdown
                  remarkPlugins={[remarkGfm]}
                  components={{
                    h1: heading('h1'),
                    h2: heading('h2'),
                    h3: heading('h3'),
                  }}
                >
                  {chapter.content}
                </ReactMarkdown>
              </div>
              <div
                style={{
                  display: 'flex',
                  justifyContent: 'space-between',
                  gap: 12,
                  marginBottom: 32,
                }}
              >
                <div>
                  {prev && (
                    <Link
                      to={`/manual/${prev.slug}`}
                      className="button-secondary button"
                      style={{ textDecoration: 'none', display: 'inline-block' }}
                    >
                      ← {prev.title}
                    </Link>
                  )}
                </div>
                <div>
                  {next && (
                    <Link
                      to={`/manual/${next.slug}`}
                      className="button"
                      style={{
                        textDecoration: 'none',
                        display: 'inline-block',
                        background: '#3498db',
                      }}
                    >
                      {next.title} →
                    </Link>
                  )}
                </div>
              </div>
            </>
          )}
        </div>
      </main>
    </div>
  );
};

export default ManualView;
