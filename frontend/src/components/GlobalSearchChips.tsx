import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { projectAPI, searchAPI, SearchHit, SearchMode } from '../api/client';
import { useAppStore } from '../state/store';
import { ARTIFACT_TYPE_CATALOG } from '../generated/contract';
import { STATUS_META, typeLabel } from '../utils/artifactFacts';
import { Chip } from './ui/Chip';
import { ChipSearch } from './search/ChipSearch';
import { ChipSuggestion, EMPTY_SEARCH, SearchSchema, SearchValue, addChip, chipValues, effectiveValue, suggestChips } from './search/searchValue';
import { useSavedSearches } from './search/useSavedSearches';

// The top-bar search on the chip search bar (issue #722): chips for project,
// type, status and owner narrow GET /api/v1/search, which the server applies;
// typing a ref such as REQ-12 still finds it first, and Enter opens the top
// hit. The results drop down below the bar, grouped by kind, with the chips
// typing offers after them.

const kindLabel = (type: string) => {
  const label = typeLabel(type);
  return label.endsWith('s') ? label : `${label}s`;
};

export const GlobalSearchChips: React.FC<{ refSearchOn: boolean }> = ({ refSearchOn }) => {
  const navigate = useNavigate();
  const projects = useAppStore((s) => s.projects);
  const currentUser = useAppStore((s) => s.currentUser);
  const [value, setValue] = useState<SearchValue>(EMPTY_SEARCH);
  const [results, setResults] = useState<SearchHit[]>([]);
  const [mode, setMode] = useState<SearchMode>('keyword');
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const boxRef = useRef<HTMLDivElement>(null);

  // A click anywhere outside the bar and its results closes them.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (boxRef.current && !boxRef.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, [open]);

  const schema: SearchSchema = useMemo(
    () => ({
      fields: [
        projects.length
          ? { key: 'project', label: 'Project', type: 'enum', options: projects.map((p) => ({ value: p.id, label: p.name })) }
          : {
              key: 'project',
              label: 'Project',
              type: 'enum',
              loadOptions: () => projectAPI.list().then((r) => (r.data || []).map((p) => ({ value: p.id, label: p.name }))),
            },
        { key: 'type', label: 'Type', type: 'enum', options: ARTIFACT_TYPE_CATALOG.map((t) => ({ value: t.value as string, label: t.label as string })) },
        { key: 'status', label: 'Status', type: 'enum', options: Object.entries(STATUS_META).map(([v, m]) => ({ value: v, label: m.label })) },
        { key: 'owner', label: 'Owner', type: 'person', options: currentUser?.name ? [{ value: currentUser.name, label: currentUser.name }] : [] },
      ],
      savedScope: 'global-search',
    }),
    [projects, currentUser?.name]
  );
  const saved = useSavedSearches(schema.savedScope, true, currentUser?.id);

  const { text } = effectiveValue(value);
  const q = text.trim();
  const facets = useMemo(
    () => ({
      projects: chipValues(value, 'project'),
      types: chipValues(value, 'type'),
      statuses: chipValues(value, 'status'),
      owners: chipValues(value, 'owner'),
    }),
    [value]
  );
  const filtered = facets.projects.length + facets.types.length + facets.statuses.length + facets.owners.length > 0;
  const facetKey = JSON.stringify(facets);

  // Debounced fetch (300ms), dropping a response the query has moved past.
  useEffect(() => {
    if (!q && !filtered) {
      setResults([]);
      setLoading(false);
      return;
    }
    let cancelled = false;
    setLoading(true);
    const timer = window.setTimeout(() => {
      searchAPI
        .global(q, { mode, ...JSON.parse(facetKey), facets: true })
        .then((res) => {
          if (cancelled) return;
          setResults(Array.isArray(res.data?.hits) ? res.data.hits : []);
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
  }, [q, facetKey, filtered, mode]);

  // Grouped by kind, in the server's rank order of each kind's first hit.
  const groups = useMemo(() => {
    const out: { type: string; hits: SearchHit[] }[] = [];
    for (const hit of results) {
      const g = out.find((x) => x.type === hit.type);
      if (g) g.hits.push(hit);
      else out.push({ type: hit.type, hits: [hit] });
    }
    return out;
  }, [results]);
  const hits = useMemo(() => groups.flatMap((g) => g.hits), [groups]);
  const offers: ChipSuggestion[] = useMemo(() => suggestChips(schema, value.text).slice(0, 5), [schema, value.text]);
  const total = hits.length + offers.length;

  const select = (hit: SearchHit) => {
    setOpen(false);
    setValue(EMPTY_SEARCH);
    inputRef.current?.blur();
    navigate(`/projects/${hit.project_id}/requirements?artifact=${hit.artifact_id}`);
  };
  const offer = (s: ChipSuggestion) => setValue({ ...addChip(value, s.chip), text: '' });

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Escape') {
      setOpen(false);
      inputRef.current?.blur();
      return;
    }
    if (!open || total === 0) return;
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setActiveIndex((i) => Math.min(i + 1, total - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setActiveIndex((i) => Math.max(i - 1, 0));
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (activeIndex < hits.length) select(hits[activeIndex]);
      else offer(offers[activeIndex - hits.length]);
    }
  };

  const showDropdown = open && (q !== '' || filtered);
  const row = (active: boolean): React.CSSProperties => ({
    padding: '7px 12px',
    cursor: 'pointer',
    background: active ? 'var(--surface-hover)' : 'transparent',
    borderBottom: '1px solid var(--border-soft)',
  });
  const heading: React.CSSProperties = {
    padding: '6px 12px 4px',
    fontSize: 10,
    fontWeight: 700,
    letterSpacing: 1,
    textTransform: 'uppercase',
    color: 'var(--text-muted)',
    background: 'var(--surface-alt)',
  };
  let index = -1;

  return (
    <div ref={boxRef} style={{ position: 'relative', display: 'flex' }}>
      <ChipSearch
        schema={schema}
        value={value}
        onChange={(next) => {
          setValue(next);
          setOpen(true);
        }}
        ariaLabel={refSearchOn ? 'Search artifacts across projects by text or ref' : 'Search artifacts across projects'}
        placeholder={refSearchOn ? 'Search artifacts or REQ-12…' : 'Search artifacts…'}
        variant="sidebar"
        align="left"
        suggestions={false}
        saved={saved}
        inputRef={inputRef}
        onInputFocus={() => setOpen(true)}
        onInputKeyDown={onKeyDown}
        helpKey="requirements.search"
      >
        {showDropdown && (
          <div
            role="region"
            aria-label="Search results"
            style={{
              position: 'absolute',
              top: 'calc(100% + 4px)',
              left: 0,
              width: 'min(380px, calc(100vw - 32px))',
              maxHeight: 440,
              overflowY: 'auto',
              background: 'var(--surface)',
              color: 'var(--text)',
              border: '1px solid var(--border)',
              borderRadius: 8,
              boxShadow: '0 8px 24px rgba(0,0,0,0.25)',
              zIndex: 1200,
            }}
          >
            <div style={{ display: 'flex', gap: 6, padding: '8px 10px', borderBottom: '1px solid var(--border-soft)' }}>
              {(['keyword', 'semantic'] as SearchMode[]).map((m) => (
                <button
                  key={m}
                  type="button"
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => setMode(m)}
                  aria-pressed={mode === m}
                  style={{
                    flex: 1,
                    padding: '4px 8px',
                    fontSize: 11,
                    fontWeight: 600,
                    textTransform: 'capitalize',
                    cursor: 'pointer',
                    borderRadius: 6,
                    border: '1px solid var(--border)',
                    background: mode === m ? 'var(--surface-hover)' : 'transparent',
                    color: mode === m ? 'var(--text)' : 'var(--text-muted)',
                  }}
                >
                  {m}
                </button>
              ))}
            </div>
            {loading && <div style={{ padding: '10px 12px', fontSize: 12, color: 'var(--text-muted)' }}>Searching…</div>}
            {!loading && hits.length === 0 && <div style={{ padding: '10px 12px', fontSize: 12, color: 'var(--text-muted)' }}>No matching artifacts</div>}
            {!loading &&
              groups.map((g) => (
                <div key={g.type} role="group" aria-label={kindLabel(g.type)}>
                  <div style={heading}>{kindLabel(g.type)}</div>
                  {g.hits.map((hit) => {
                    index += 1;
                    const mine = index;
                    return (
                      <div key={hit.artifact_id} role="button" tabIndex={-1} onClick={() => select(hit)} onMouseEnter={() => setActiveIndex(mine)} style={row(mine === activeIndex)}>
                        <div style={{ display: 'flex', alignItems: 'baseline', gap: 8 }}>
                          {hit.ref && <span style={{ fontSize: 10, fontWeight: 700, fontFamily: 'var(--font-mono, monospace)', color: 'var(--text-muted)', flexShrink: 0 }}>{hit.ref}</span>}
                          <span style={{ flex: 1, minWidth: 0, fontSize: 13, fontWeight: 600, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{hit.title}</span>
                          {hit.status && <Chip>{STATUS_META[hit.status as keyof typeof STATUS_META]?.label ?? hit.status}</Chip>}
                        </div>
                        <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>
                          {hit.project_name || 'Project'}
                          {hit.owner ? ` · ${hit.owner}` : ''}
                        </div>
                        {hit.snippet && <div style={{ fontSize: 11, color: 'var(--text-secondary)', marginTop: 2, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{hit.snippet}</div>}
                      </div>
                    );
                  })}
                </div>
              ))}
            {offers.length > 0 && (
              <div role="group" aria-label="Filter by">
                <div style={heading}>Filter by</div>
                {offers.map((s, i) => {
                  const mine = hits.length + i;
                  return (
                    <div key={s.id} role="button" tabIndex={-1} onMouseDown={(e) => e.preventDefault()} onClick={() => offer(s)} onMouseEnter={() => setActiveIndex(mine)} style={{ ...row(mine === activeIndex), fontSize: 12 }}>
                      {s.label}
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        )}
      </ChipSearch>
    </div>
  );
};
