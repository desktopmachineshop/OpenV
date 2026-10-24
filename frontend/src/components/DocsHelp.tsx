import React, { useCallback, useEffect, useRef, useState } from 'react';
import { helpAPI } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import type { HelpHit, HelpTopic } from '../api/client';

// The help panel's content when the deployment serves help from the user
// manual on OpenD (capability help-docs, docs/plans/opend-help.md): the
// manual's topic for the page you are on, for the release your workspace is
// on. Links between topics open in the panel, with Back to return; search
// covers the topics your workspace may read; "Open in the docs" opens the
// same page on the public docs site.
//
// The server sanitizes every topic's HTML (internal/domain/help), so it is
// rendered as given.

interface DocsHelpProps {
  orgId: string;
  /** The app path the panel follows, such as /projects/p1/vv. */
  path: string;
  /** Called when the docs cannot serve help here at all, so the panel can
   *  fall back to the help the app ships. */
  onUnavailable: () => void;
}

type Status = 'loading' | 'ready' | 'missing';

export const DocsHelp: React.FC<DocsHelpProps> = ({ orgId, path, onUnavailable }) => {
  const [topic, setTopic] = useState<HelpTopic | null>(null);
  const [history, setHistory] = useState<HelpTopic[]>([]);
  const [status, setStatus] = useState<Status>('loading');
  const [query, setQuery] = useState('');
  const [hits, setHits] = useState<HelpHit[] | null>(null);
  const body = useRef<HTMLDivElement>(null);

  // The topic for the page, whenever the page changes.
  useEffect(() => {
    let live = true;
    setStatus('loading');
    setHistory([]);
    setHits(null);
    helpAPI
      .context(orgId, { path })
      .then(({ data }) => {
        if (!live) return;
        setTopic(data);
        setStatus('ready');
      })
      .catch((err) => {
        if (!live) return;
        // No topic for this page: say so and offer search. Anything else
        // (help not configured, the docs site down): the app's own help.
        if (apiErrorMessage(err, '') === 'no help for this') {
          setTopic(null);
          setStatus('missing');
        } else {
          onUnavailable();
        }
      });
    return () => {
      live = false;
    };
  }, [orgId, path, onUnavailable]);

  // Scroll to the section a context or a link names.
  useEffect(() => {
    const el = body.current;
    if (!el) return;
    if (topic?.fragment) {
      el.querySelector(`[id="${CSS.escape(topic.fragment)}"]`)?.scrollIntoView({ block: 'start' });
    } else {
      el.scrollTop = 0;
    }
  }, [topic]);

  const open = useCallback(
    (topicId: string, fragment?: string) => {
      helpAPI
        .topic(orgId, topicId)
        .then(({ data }) => {
          setHistory((h) => (topic ? [...h, topic] : h));
          setTopic({ ...data, fragment });
          setHits(null);
          setStatus('ready');
        })
        .catch(() => {
          // A topic that vanished between two releases: stay where we are.
        });
    },
    [orgId, topic],
  );

  const back = () => {
    setHistory((h) => {
      const prev = h[h.length - 1];
      if (prev) {
        setTopic(prev);
        setStatus('ready');
      }
      return h.slice(0, -1);
    });
  };

  // Links between topics open here; every other link opens in a new tab.
  const onBodyClick = (e: React.MouseEvent) => {
    const link = (e.target as HTMLElement).closest('a[data-help-topic]') as HTMLAnchorElement | null;
    if (!link || e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
    open(link.dataset.helpTopic!, link.dataset.helpFragment);
  };

  const search = (e: React.FormEvent) => {
    e.preventDefault();
    const q = query.trim();
    if (!q) {
      setHits(null);
      return;
    }
    helpAPI
      .search(orgId, q)
      .then(({ data }) => setHits(data.hits))
      .catch(() => setHits([]));
  };

  return (
    <div className="help-docs">
      <form className="help-docs-search" role="search" onSubmit={search}>
        <input
          type="search"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search the manual"
          aria-label="Search the manual"
        />
        <button type="submit">Search</button>
      </form>

      {hits && (
        <section className="help-section" aria-label="Search results">
          <h4>Results</h4>
          {hits.length === 0 ? (
            <p className="help-docs-empty">Nothing in the manual matches “{query.trim()}”.</p>
          ) : (
            <ul className="help-docs-list">
              {hits.map((hit) => (
                <li key={hit.topic_id}>
                  <button type="button" className="help-docs-link" onClick={() => open(hit.topic_id)}>
                    {hit.title}
                  </button>
                  <p>{hit.snippet}</p>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}

      {history.length > 0 && (
        <button type="button" className="help-docs-back" onClick={back}>
          ← Back
        </button>
      )}

      {status === 'loading' && <p className="help-docs-empty">Loading help…</p>}
      {status === 'missing' && (
        <p className="help-docs-empty">The manual has no topic for this page yet. Search it above.</p>
      )}

      {status === 'ready' && topic && (
        <>
          <article className="help-docs-topic" aria-labelledby="help-docs-title">
            <h3 id="help-docs-title">{topic.title}</h3>
            {topic.shortdesc && <p className="help-intro">{topic.shortdesc}</p>}
            {/* Sanitized by the server: see the note at the top. */}
            <div
              ref={body}
              className="help-docs-body"
              onClick={onBodyClick}
              dangerouslySetInnerHTML={{ __html: topic.html }}
            />
          </article>

          {topic.related.length > 0 && (
            <section className="help-section">
              <h4>In this section</h4>
              <ul className="help-docs-list">
                {topic.related.map((r) => (
                  <li key={r.topic_id}>
                    <button type="button" className="help-docs-link" onClick={() => open(r.topic_id)}>
                      {r.title}
                    </button>
                  </li>
                ))}
              </ul>
            </section>
          )}

          <footer className="help-docs-footer">
            <span>Manual for OpenV {topic.line}</span>
            {topic.full_url && (
              <a href={topic.full_url} target="_blank" rel="noopener noreferrer">
                Open in the docs ↗
              </a>
            )}
          </footer>
        </>
      )}
    </div>
  );
};

export default DocsHelp;
