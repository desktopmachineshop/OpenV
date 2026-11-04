// The Design page's panels: the tree, the selected item's header and its
// attributes, what it traces to, and its properties.
import React from 'react';
import { Link } from 'react-router-dom';
import type { Artifact } from '../api/client';
import {
  Chip,
  GridHead,
  PanelHeader,
  buttonStyle,
  ellipsis,
  eyebrowStyle,
  gridRowStyle,
  mutedStyle,
  panelStyle,
  plainLink,
  refStyle,
  tapTarget,
} from '../components/ui';
import { attributeRows, type TreeNode, type Traced, type Verification } from './DesignItemsTree';

const STATUS_LABEL: Record<string, string> = { draft: 'draft', in_review: 'in review', approved: 'approved', superseded: 'superseded' };
export const statusWord = (a: Artifact) => STATUS_LABEL[a.status || 'draft'] || a.status || 'draft';

export const DesignTree: React.FC<{
  rows: TreeNode[];
  selected: string;
  collapsed: Set<string>;
  filter: string;
  onFilter: (v: string) => void;
  onSelect: (id: string) => void;
  onToggle: (id: string) => void;
}> = ({ rows, selected, collapsed, filter, onFilter, onSelect, onToggle }) => (
  <aside
    aria-label="Design tree"
    data-help-key="design.tree"
    style={{
      flex: '1 1 260px',
      maxWidth: 320,
      minWidth: 0,
      background: 'var(--surface)',
      borderRight: '1px solid var(--border)',
      display: 'flex',
      flexDirection: 'column',
    }}
  >
    <div style={{ padding: '10px 12px', borderBottom: '1px solid var(--border-soft)' }}>
      <input
        type="search"
        aria-label="Filter design items"
        placeholder="Filter"
        value={filter}
        onChange={(e) => onFilter(e.target.value)}
        style={{ width: '100%', boxSizing: 'border-box', height: 30, margin: 0 }}
      />
    </div>
    <ul role="tree" aria-label="Design items" style={{ listStyle: 'none', margin: 0, padding: '6px 0' }}>
      {rows.map(({ item, depth, descendants, hasChildren }) => {
        const sel = item.id === selected;
        const open = !collapsed.has(item.id);
        return (
          <li
            key={item.id}
            role="treeitem"
            aria-selected={sel}
            aria-expanded={hasChildren ? open : undefined}
            tabIndex={sel ? 0 : -1}
            onClick={() => onSelect(item.id)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                onSelect(item.id);
              }
            }}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 6,
              minHeight: 30,
              paddingLeft: 10 + depth * 14,
              paddingRight: 10,
              cursor: 'pointer',
              background: sel ? 'var(--tint-blue)' : 'transparent',
            }}
          >
            <button
              type="button"
              aria-label={open ? `Collapse ${item.title}` : `Expand ${item.title}`}
              disabled={!hasChildren}
              onClick={(e) => {
                e.stopPropagation();
                onToggle(item.id);
              }}
              style={{
                width: 24,
                height: 24,
                padding: 0,
                border: 'none',
                background: 'none',
                color: 'var(--text-muted)',
                fontSize: 10,
                cursor: hasChildren ? 'pointer' : 'default',
                visibility: hasChildren ? 'visible' : 'hidden',
              }}
            >
              {open ? '▾' : '▸'}
            </button>
            {item.ref && <span style={{ ...refStyle, fontSize: 12 }}>{item.ref}</span>}
            <span
              style={{
                ...ellipsis,
                flex: 1,
                fontWeight: depth === 0 || sel ? 600 : undefined,
                color: sel ? 'var(--accent-text)' : undefined,
              }}
            >
              {item.title}
            </span>
            {hasChildren && <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{descendants}</span>}
          </li>
        );
      })}
    </ul>
    {rows.length === 0 && <p style={{ ...mutedStyle, padding: '0 12px' }}>No design item matches.</p>}
  </aside>
);

export const ItemHeader: React.FC<{ item: Artifact; projectId: string }> = ({ item, projectId }) => (
  <>
    <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
      {item.ref && <span style={{ ...refStyle, fontWeight: 600 }}>{item.ref}</span>}
      <Chip>{statusWord(item)}</Chip>
      <Chip tone="neutral">Design item</Chip>
      <span style={{ flexGrow: 1 }} />
      <Link to={`/projects/${projectId}/requirements?artifact=${item.id}`} style={buttonStyle('secondary', { compact: true })}>
        Edit
      </Link>
    </div>
    <div style={{ display: 'flex', flexDirection: 'column', gap: 4, maxWidth: 1100 }}>
      <h1 style={{ margin: 0, fontSize: 20, fontWeight: 700 }}>{item.title}</h1>
      {item.body ? (
        <p style={{ margin: 0, fontSize: 14, lineHeight: 1.55, color: 'var(--text-body)', whiteSpace: 'pre-wrap' }}>
          {item.body}
        </p>
      ) : (
        <p style={{ ...mutedStyle, margin: 0 }}>No description yet.</p>
      )}
    </div>
  </>
);

const ATTR_COLUMNS = 'minmax(120px, 0.6fr) minmax(0, 1.4fr)';

export const Attributes: React.FC<{ item: Artifact }> = ({ item }) => {
  const rows = attributeRows(item);
  return (
    <section aria-labelledby="design-attrs" data-help-key="design.attributes" style={{ ...panelStyle, flex: '2 1 420px' }}>
      <PanelHeader id="design-attrs" title="Attributes" count={rows.length} />
      {rows.length === 0 ? (
        <p style={{ ...mutedStyle, margin: 0, padding: 12 }}>This design item has no attributes. Add them in its editor.</p>
      ) : (
        <div role="table" aria-label="Attributes">
          <GridHead columns={ATTR_COLUMNS} names={['Attribute', 'Value']} />
          {rows.map(([k, v]) => (
            <div role="row" key={k} style={gridRowStyle(ATTR_COLUMNS)}>
              <span role="cell" style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
                {k}
              </span>
              <span role="cell" style={{ ...ellipsis, fontSize: 13 }} title={v}>
                {v}
              </span>
            </div>
          ))}
        </div>
      )}
    </section>
  );
};

const RefLine: React.FC<{ a: Artifact; projectId: string; extra?: React.ReactNode }> = ({ a, projectId, extra }) => (
  <Link
    to={`/projects/${projectId}/requirements?artifact=${a.id}`}
    style={{ ...plainLink, ...tapTarget, display: 'flex', gap: 8, padding: '2px 0', color: 'var(--text)' }}
  >
    <span style={{ ...refStyle, width: 64, flex: 'none' }}>{a.ref || '—'}</span>
    <span style={{ ...ellipsis, flex: 1 }}>{a.title}</span>
    {extra}
  </Link>
);

const smallPanel: React.CSSProperties = { ...panelStyle, padding: '10px 12px', display: 'flex', flexDirection: 'column', gap: 4 };

export const TracePanels: React.FC<{
  traces: Traced[];
  tracedFrom: Traced[];
  verifications: Verification[];
  projectId: string;
}> = ({ traces, tracedFrom, verifications, projectId }) => {
  const groups = new Map<string, Traced[]>();
  for (const t of traces) groups.set(t.type, [...(groups.get(t.type) || []), t]);
  if (!groups.has('satisfies')) groups.set('satisfies', []);
  return (
    <>
      {[...groups].map(([type, list]) => (
        <section key={type} style={smallPanel} data-help-key="design.trace">
          <h2 style={{ ...eyebrowStyle, marginBottom: 4 }}>
            {type} · {list.length}
          </h2>
          {list.length === 0 && <span style={mutedStyle}>No requirement linked yet.</span>}
          {list.map((t) => (
            <RefLine key={t.artifact.id} a={t.artifact} projectId={projectId} extra={t.suspect ? <Chip>suspect</Chip> : undefined} />
          ))}
        </section>
      ))}
      <section style={smallPanel} data-help-key="design.trace">
        <h2 style={{ ...eyebrowStyle, marginBottom: 4 }}>Verified through · {verifications.length}</h2>
        {verifications.length === 0 && <span style={mutedStyle}>No test case verifies the requirements it satisfies.</span>}
        {verifications.map((v) => (
          <RefLine key={v.testCase.id} a={v.testCase} projectId={projectId} extra={<Chip>{v.result}</Chip>} />
        ))}
      </section>
      {tracedFrom.length > 0 && (
        <section style={smallPanel}>
          <h2 style={{ ...eyebrowStyle, marginBottom: 4 }}>Linked from · {tracedFrom.length}</h2>
          {tracedFrom.map((t) => (
            <RefLine key={t.artifact.id} a={t.artifact} projectId={projectId} extra={<span style={mutedStyle}>{t.type}</span>} />
          ))}
        </section>
      )}
    </>
  );
};

export const Properties: React.FC<{ item: Artifact; parent?: Artifact; contains: number }> = ({ item, parent, contains: children }) => {
  const rows: [string, React.ReactNode][] = [
    ['Parent', parent ? `${parent.ref ? `${parent.ref} ` : ''}${parent.title}` : '—'],
    ['Contains', children ? `${children} design ${children === 1 ? 'item' : 'items'}` : '—'],
    ['Version', String(item.version)],
    ['Updated', new Date(item.updated_at).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })],
  ];
  return (
    <section style={{ ...panelStyle, padding: '10px 12px' }}>
      <h2 style={{ ...eyebrowStyle, marginBottom: 6 }}>Properties</h2>
      <dl style={{ margin: 0, display: 'grid', gridTemplateColumns: '96px minmax(0, 1fr)', gap: '5px 8px', fontSize: 12 }}>
        {rows.map(([k, v]) => (
          <React.Fragment key={k}>
            <dt style={{ color: 'var(--text-muted)' }}>{k}</dt>
            <dd style={{ margin: 0 }}>{v}</dd>
          </React.Fragment>
        ))}
      </dl>
    </section>
  );
};
