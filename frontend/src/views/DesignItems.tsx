// The Design page: the project's design items as a tree on the left, and the
// selected item on the right: its description, its attributes as a table, the
// requirements it satisfies (and anything else it links to), the test cases
// that verify those requirements, and its place in the tree. A phone walks the
// tree instead: a path, the item, what it contains and its siblings. Gated by
// redesign-pages; it reads artifacts, links and V&V coverage.
import React, { useEffect, useMemo, useState } from 'react';
import { Link, Navigate, useParams } from 'react-router-dom';
import { writeSearchParams } from '../components/search/searchValue';
import { useUiRefresh } from '../hooks/useUiRefresh';
import { artifactAPI, linkAPI, vvAPI, type Artifact, type CoverageReport, type Link as TraceLink } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { Chip, ErrorBanner, FeatureGate, PanelHeader, ellipsis, mutedStyle, pageType, panelStyle, refStyle } from '../components/ui';
import { REDESIGN_PAGES_FEATURE } from '../features';
import { useViewport } from '../hooks/useViewport';
import { Attributes, DesignTree, ItemHeader, Properties, TracePanels, statusWord } from './DesignItemsParts';
import { DESIGN_TYPE, childrenOf, pathTo, traceOf, visibleRows } from './DesignItemsTree';

interface Loaded {
  artifacts: Artifact[];
  links: TraceLink[];
  coverage: CoverageReport | null;
}

const ItemList: React.FC<{ title: string; items: Artifact[]; kids: Map<string, Artifact[]>; onSelect: (id: string) => void }> = ({
  title,
  items,
  kids,
  onSelect,
}) =>
  items.length === 0 ? null : (
    <section aria-label={title} style={panelStyle}>
      <PanelHeader title={title} count={items.length} />
      {items.map((i) => (
        <button
          key={i.id}
          type="button"
          onClick={() => onSelect(i.id)}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 8,
            width: '100%',
            minHeight: 48,
            padding: '6px 12px',
            border: 'none',
            borderBottom: '1px solid var(--border-soft)',
            background: 'var(--surface)',
            color: 'var(--text)',
            textAlign: 'left',
            font: 'inherit',
          }}
        >
          {i.ref && <span style={refStyle}>{i.ref}</span>}
          <span style={{ ...ellipsis, flex: 1 }}>{i.title}</span>
          {(kids.get(i.id) || []).length > 0 && <span style={mutedStyle}>{(kids.get(i.id) || []).length} ›</span>}
        </button>
      ))}
    </section>
  );

const DesignContent: React.FC = () => {
  const { projectId = '' } = useParams<{ projectId: string }>();
  const { isPhone } = useViewport();
  const [data, setData] = useState<Loaded | null>(null);
  const [error, setError] = useState('');
  const [selected, setSelected] = useState('');
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const [filter, setFilter] = useState('');

  useEffect(() => {
    let live = true;
    Promise.all([artifactAPI.list(projectId), linkAPI.list(projectId), vvAPI.coverage(projectId).catch(() => null)])
      .then(([a, l, c]) => live && setData({ artifacts: a.data, links: l.data || [], coverage: c?.data || null }))
      .catch((err) => live && setError(apiErrorMessage(err, 'Failed to load the design items')));
    return () => {
      live = false;
    };
  }, [projectId]);

  const items = useMemo(() => (data?.artifacts || []).filter((a) => a.type === DESIGN_TYPE), [data]);
  const kids = useMemo(() => childrenOf(items), [items]);
  const rows = useMemo(() => visibleRows(items, collapsed, filter), [items, collapsed, filter]);
  const current = items.find((i) => i.id === selected) || items[0];
  const trace = useMemo(
    () => (current && data ? traceOf(current.id, data.artifacts, data.links, data.coverage) : null),
    [current, data]
  );

  if (error) return <ErrorBanner message={error} />;
  if (!data) return <div style={{ padding: 16, color: 'var(--text-muted)' }}>Loading…</div>;
  if (!current || !trace) {
    return (
      <div style={{ padding: 16 }} data-help-pane="design">
        <h1 className="ov-page-title" style={{ margin: '0 0 8px', fontSize: 20 }}>Design</h1>
        <p style={mutedStyle}>
          This project has no design items yet. Add a <b>Design Item</b> on the{' '}
          <Link to={`/projects/${projectId}/requirements`}>Requirements</Link> page, and link it to the requirements it
          satisfies.
        </p>
      </div>
    );
  }
  const byId = new Map(items.map((i) => [i.id, i]));
  const parent = current.parent_id ? byId.get(current.parent_id) : undefined;
  const children = kids.get(current.id) || [];
  const select = (id: string) => setSelected(id);
  const toggle = (id: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  if (isPhone) {
    const path = pathTo(current.id, items).map((id) => byId.get(id)!);
    const siblings = (kids.get(parent?.id || '') || []).filter((s) => s.id !== current.id);
    return (
      <div data-help-pane="design" style={{ ...pageType(true), padding: 12, display: 'flex', flexDirection: 'column', gap: 12 }}>
        <nav aria-label="Path" style={{ display: 'flex', flexWrap: 'wrap', gap: 6, alignItems: 'center', fontSize: 13 }}>
          {path.map((p, i) =>
            i === path.length - 1 ? (
              <span key={p.id} aria-current="page" style={{ fontWeight: 600 }}>
                {p.title}
              </span>
            ) : (
              <React.Fragment key={p.id}>
                <button type="button" onClick={() => select(p.id)} style={{ border: 'none', background: 'none', padding: 0, color: 'var(--accent-text)', font: 'inherit' }}>
                  {p.title}
                </button>
                <span aria-hidden="true" style={{ color: 'var(--text-muted)' }}>
                  ›
                </span>
              </React.Fragment>
            )
          )}
        </nav>
        <section style={{ ...panelStyle, padding: 12, display: 'flex', flexDirection: 'column', gap: 6 }}>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            {current.ref && <span style={{ ...refStyle, fontWeight: 600 }}>{current.ref}</span>}
            <Chip>{statusWord(current)}</Chip>
          </div>
          <h1 style={{ margin: 0, fontSize: 18 }}>{current.title}</h1>
          {current.body && <p style={{ margin: 0, color: 'var(--text-body)', whiteSpace: 'pre-wrap' }}>{current.body}</p>}
          <div style={{ display: 'flex', gap: 12, ...mutedStyle }}>
            <span>{children.length} children</span>
            <span>Satisfies {trace.traces.filter((t) => t.type === 'satisfies').length}</span>
            <span>Verified through {trace.verifications.length}</span>
          </div>
        </section>
        <ItemList title="Contains" items={children} kids={kids} onSelect={select} />
        <TracePanels {...trace} projectId={projectId} />
        <ItemList title={parent ? `Also in ${parent.title}` : 'Also at the top'} items={siblings} kids={kids} onSelect={select} />
      </div>
    );
  }

  return (
    <div data-help-pane="design" style={{ ...pageType(false), display: 'flex', flexWrap: 'wrap', alignItems: 'stretch', minHeight: '100%' }}>
      <DesignTree
        rows={rows}
        selected={current.id}
        collapsed={collapsed}
        filter={filter}
        onFilter={setFilter}
        onSelect={select}
        onToggle={toggle}
      />
      <section
        aria-label={`Design item ${current.ref || current.title}`}
        data-help-key="design.detail"
        style={{ flex: '999 1 560px', minWidth: 0, padding: 16, display: 'flex', flexDirection: 'column', gap: 12 }}
      >
        <ItemHeader item={current} projectId={projectId} />
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 12, alignItems: 'flex-start' }}>
          <Attributes item={current} />
          <div style={{ flex: '1 1 300px', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 12 }}>
            <TracePanels {...trace} projectId={projectId} />
            <Properties item={current} parent={parent} contains={children.length} />
          </div>
        </div>
      </section>
    </div>
  );
};

/** Where the Design page's address leads in the refreshed interface: Requirements, filtered to design items. */
export const DESIGN_IN_REQUIREMENTS = `../requirements?${writeSearchParams(new URLSearchParams(), {
  chips: [{ kind: 'filter', field: 'type', values: [DESIGN_TYPE], match: 'is' }],
  text: '',
}).toString()}`;

export const DesignItems: React.FC = () => {
  // The refreshed interface (ui-refresh) has no Design page: a design item is
  // read in Requirements, whose pane shows what this page showed of it. Old
  // links and bookmarks land there.
  const refresh = useUiRefresh();
  if (refresh) return <Navigate to={DESIGN_IN_REQUIREMENTS} replace />;
  return (
    <FeatureGate feature={REDESIGN_PAGES_FEATURE} name="Design">
      <DesignContent />
    </FeatureGate>
  );
};
