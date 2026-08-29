import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { searchAPI, SearchHit } from '../api/client';

// GlobalSearch is the workspace-wide artifact search box (issue #128). It
// lives in the project sidebar header, queries GET /api/v1/search (debounced),
// and shows a dropdown of results grouped by project. Selecting a result deep
// links into that project's requirements view (?artifact= is the shareable
// selection param handled by ModuleView).
export const GlobalSearch: React.FC = () => {
  const navigate = useNavigate();
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<SearchHit[]>([]);
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  // Debounced fetch (300ms). The cancelled flag drops responses that land
  // after the query has moved on.
  useEffect(() => {
    const q = query.trim();
    if (!q) {
      setResults([]);
      setLoading(false);
      return;
    }
    let cancelled = false;
    setLoading(true);
    const timer = window.setTimeout(() => {
      searchAPI
        .global(q)
        .then((res) => {
          if (cancelled) return;
          setResults(Array.isArray(res.data) ? res.data : []);
          setActiveIndex(0);
          setLoading(false);
        })
        .catch(() => {
          if (cancelled) return;
          setResults([]);
          setLoading(false);
        });
    }, 300);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [query]);

  // Close when clicking anywhere outside the search box.
  useEffect(() => {
    if (!open) return;
    const onMouseDown = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener('mousedown', onMouseDown);
    return () => document.removeEventListener('mousedown', onMouseDown);
  }, [open]);

  // Group hits by project, preserving server rank order within and across
  // groups. The flattened order is what keyboard navigation walks.
  const groups = useMemo(() => {
    const byProject: { projectId: string; projectName: string; hits: SearchHit[] }[] = [];
    for (const hit of results) {
      const group = byProject.find((g) => g.projectId === hit.project_id);
      if (group) {
        group.hits.push(hit);
      } else {
        byProject.push({
          projectId: hit.project_id,
          projectName: hit.project_name || 'Project',
          hits: [hit],
        });
      }
    }
    return byProject;
  }, [results]);

  const flat = useMemo(() => groups.flatMap((g) => g.hits), [groups]);

  const select = (hit: SearchHit) => {
    setOpen(false);
    setQuery('');
    inputRef.current?.blur();
    navigate(`/projects/${hit.project_id}/requirements?artifact=${hit.artifact_id}`);
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Escape') {
      setOpen(false);
      inputRef.current?.blur();
      return;
    }
    if (!open) return;
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setActiveIndex((i) => Math.min(i + 1, flat.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setActiveIndex((i) => Math.max(i - 1, 0));
    } else if (e.key === 'Enter' && flat[activeIndex]) {
      e.preventDefault();
      select(flat[activeIndex]);
    }
  };

  const showDropdown = open && query.trim() !== '';
  let flatIndex = -1;

  return (
    <div ref={containerRef} style={{ position: 'relative' }}>
      <input
        ref={inputRef}
        type="search"
        value={query}
        placeholder="Search artifacts…"
        aria-label="Search artifacts across projects"
        onChange={(e) => {
          setQuery(e.target.value);
          setOpen(true);
        }}
        onFocus={() => setOpen(true)}
        onKeyDown={onKeyDown}
        style={{
          width: '100%',
          boxSizing: 'border-box',
          padding: '6px 8px',
          fontSize: 12,
          borderRadius: 6,
          border: '1px solid var(--sidebar-border)',
          background: 'var(--sidebar-menu-bg)',
          color: 'var(--sidebar-text)',
          outline: 'none',
        }}
      />
      {showDropdown && (
        <div
          style={{
            position: 'absolute',
            top: 'calc(100% + 4px)',
            left: 0,
            width: 340,
            maxHeight: 420,
            overflowY: 'auto',
            background: 'var(--surface)',
            color: 'var(--text)',
            border: '1px solid var(--border)',
            borderRadius: 8,
            boxShadow: '0 8px 24px rgba(0,0,0,0.25)',
            zIndex: 1200,
          }}
        >
          {loading && (
            <div style={{ padding: '10px 12px', fontSize: 12, color: 'var(--text-muted)' }}>
              Searching…
            </div>
          )}
          {!loading && flat.length === 0 && (
            <div style={{ padding: '10px 12px', fontSize: 12, color: 'var(--text-muted)' }}>
              No matching artifacts
            </div>
          )}
          {!loading &&
            groups.map((group) => (
              <div key={group.projectId}>
                <div
                  style={{
                    padding: '6px 12px 4px',
                    fontSize: 10,
                    fontWeight: 700,
                    letterSpacing: 1,
                    textTransform: 'uppercase',
                    color: 'var(--text-muted)',
                    background: 'var(--surface-alt)',
                    position: 'sticky',
                    top: 0,
                  }}
                >
                  {group.projectName}
                </div>
                {group.hits.map((hit) => {
                  flatIndex += 1;
                  const isActive = flatIndex === activeIndex;
                  const myIndex = flatIndex;
                  return (
                    <div
                      key={hit.artifact_id}
                      role="button"
                      tabIndex={-1}
                      onClick={() => select(hit)}
                      onMouseEnter={() => setActiveIndex(myIndex)}
                      style={{
                        padding: '8px 12px',
                        cursor: 'pointer',
                        background: isActive ? 'var(--surface-hover)' : 'transparent',
                        borderBottom: '1px solid var(--border-soft)',
                      }}
                    >
                      <div style={{ display: 'flex', alignItems: 'baseline', gap: 8 }}>
                        <span
                          style={{
                            fontSize: 13,
                            fontWeight: 600,
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                          }}
                        >
                          {hit.title}
                        </span>
                        <span style={{ fontSize: 10, color: 'var(--text-muted)', flexShrink: 0 }}>
                          {hit.type}
                        </span>
                      </div>
                      {hit.snippet && (
                        <div
                          style={{
                            fontSize: 11,
                            color: 'var(--text-secondary)',
                            marginTop: 2,
                            display: '-webkit-box',
                            WebkitLineClamp: 2,
                            WebkitBoxOrient: 'vertical',
                            overflow: 'hidden',
                          }}
                        >
                          {hit.snippet}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            ))}
        </div>
      )}
    </div>
  );
};
