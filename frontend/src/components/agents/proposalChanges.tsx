// What an agent's proposal would change, read as a person reads a change:
// the kind of write as a chip, the artifact it lands on, and for an edit
// each field before and after. The raw payload stays one click away.
import React from 'react';
import type { Artifact, Link as TraceLink, Proposal } from '../../api/client';
import { Chip } from '../ui';
import type { ChipTone } from '../ui';

const OPS: Record<string, { label: string; tone: ChipTone }> = {
  create_artifact: { label: 'New artifact', tone: 'success' },
  create: { label: 'New artifact', tone: 'success' },
  update_artifact: { label: 'Edit', tone: 'info' },
  update: { label: 'Edit', tone: 'info' },
  delete_artifact: { label: 'Delete', tone: 'danger' },
  delete: { label: 'Delete', tone: 'danger' },
  create_link: { label: 'New link', tone: 'success' },
  delete_link: { label: 'Remove link', tone: 'danger' },
  record_test_result: { label: 'Test result', tone: 'agent' },
};

export const opMeta = (op: string): { label: string; tone: ChipTone } =>
  OPS[(op || '').toLowerCase()] || { label: op || 'change', tone: 'neutral' };

export interface ProposalContext {
  artifacts: Record<string, Artifact>;
  links: Record<string, TraceLink>;
}

const FIELD_LABEL: Record<string, string> = {
  title: 'Title',
  body: 'Text',
  status: 'Review status',
  type: 'Type',
  parent_id: 'Parent',
};

// Payload keys that address the write rather than change a field.
const ADDRESS_KEYS = new Set(['project_id', 'id', 'artifact_id', 'ref', 'run_id', 'version']);

const show = (v: unknown): string => {
  if (v === null || v === undefined || v === '') return '';
  if (typeof v === 'string') return v;
  return JSON.stringify(v);
};

const refStyle: React.CSSProperties = {
  fontFamily: 'source-code-pro, Menlo, Monaco, Consolas, monospace',
  fontSize: 12,
  color: 'var(--accent-text)',
};

/** An artifact named by its ref and title, or by the id a proposal holds. */
export const ArtifactName: React.FC<{ id?: string | null; ctx: ProposalContext; fallback?: string }> = ({ id, ctx, fallback }) => {
  const a = id ? ctx.artifacts[id] : undefined;
  if (!a) return <span style={refStyle}>{id || fallback || 'new'}</span>;
  return (
    <span>
      {a.ref && <span style={{ ...refStyle, marginRight: 6 }}>{a.ref}</span>}
      <strong style={{ fontWeight: 600 }}>{a.title}</strong>
    </span>
  );
};

const block = (bg: string, border: string, strike = false): React.CSSProperties => ({
  background: bg,
  border: `1px solid ${border}`,
  borderRadius: 4,
  padding: '4px 8px',
  fontSize: 13,
  whiteSpace: 'pre-wrap',
  wordBreak: 'break-word',
  maxHeight: 180,
  overflowY: 'auto',
  textDecoration: strike ? 'line-through' : undefined,
  color: strike ? 'var(--text-muted)' : 'var(--text)',
});

/** One field's change: what it says now, and what it would say. */
const FieldChange: React.FC<{ label: string; before?: string; after: string }> = ({ label, before, after }) => (
  <div style={{ display: 'flex', flexWrap: 'wrap', gap: '2px 8px', alignItems: 'flex-start' }}>
    <span style={{ flex: '0 0 112px', fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', paddingTop: 5 }}>{label}</span>
    <span style={{ flex: '1 1 240px', display: 'flex', flexDirection: 'column', gap: 4, minWidth: 0 }}>
      {before !== undefined && before !== '' && (
        <span aria-label={`${label} before`} style={block('var(--tint-red)', 'var(--tint-red-border)', true)}>
          {before}
        </span>
      )}
      <span aria-label={`${label} after`} style={block('var(--tint-green)', 'var(--tint-green-border)')}>
        {after || <em style={{ color: 'var(--text-muted)' }}>empty</em>}
      </span>
    </span>
  </div>
);

/** The fields a create or update sets, attributes flattened, each against what the artifact has now. */
const fieldChanges = (payload: Record<string, any>, current: Artifact | undefined, ctx: ProposalContext) => {
  const out: { key: string; label: string; before?: string; after: string }[] = [];
  Object.entries(payload || {}).forEach(([k, v]) => {
    if (ADDRESS_KEYS.has(k)) return;
    if (k === 'attributes' && v && typeof v === 'object' && !Array.isArray(v)) {
      Object.entries(v as Record<string, unknown>).forEach(([ak, av]) => {
        const before = current ? show(current.attributes?.[ak]) : undefined;
        if (current && before === show(av)) return;
        out.push({ key: `attributes.${ak}`, label: ak.replace(/_/g, ' '), before, after: show(av) });
      });
      return;
    }
    const before = current ? show((current as unknown as Record<string, unknown>)[k]) : undefined;
    if (current && before === show(v)) return;
    const after = k === 'parent_id' && typeof v === 'string' && ctx.artifacts[v] ? ctx.artifacts[v].title : show(v);
    const was = k === 'parent_id' && before && ctx.artifacts[before] ? ctx.artifacts[before].title : before;
    out.push({ key: k, label: FIELD_LABEL[k] || k.replace(/_/g, ' '), before: was, after });
  });
  return out;
};

/** The headline: what the proposal does, and to what. */
export const ProposalTarget: React.FC<{ proposal: Proposal; ctx: ProposalContext }> = ({ proposal: p, ctx }) => {
  const payload = p.payload || {};
  switch ((p.op || '').toLowerCase()) {
    case 'create_artifact':
    case 'create':
      return (
        <span>
          {payload.type && <Chip tone="neutral">{String(payload.type)}</Chip>}{' '}
          <strong style={{ fontWeight: 600 }}>{show(payload.title) || 'Untitled'}</strong>
        </span>
      );
    case 'create_link':
    case 'delete_link': {
      const link = p.op === 'delete_link' && p.target_id ? ctx.links[p.target_id] : undefined;
      const from = link?.from_id || payload.from_id;
      const to = link?.to_id || payload.to_id;
      const type = link?.type || payload.type || 'link';
      return (
        <span style={{ display: 'inline-flex', flexWrap: 'wrap', alignItems: 'center', gap: 6 }}>
          <ArtifactName id={from} ctx={ctx} />
          <Chip tone="neutral">{String(type)} →</Chip>
          <ArtifactName id={to} ctx={ctx} />
        </span>
      );
    }
    case 'record_test_result':
      return (
        <span style={{ display: 'inline-flex', flexWrap: 'wrap', alignItems: 'center', gap: 6 }}>
          <ArtifactName id={payload.test_case_id} ctx={ctx} />
          {payload.status && <Chip>{String(payload.status)}</Chip>}
        </span>
      );
    default:
      return <ArtifactName id={p.target_id} ctx={ctx} />;
  }
};

/** The body of a proposal card: the fields it changes, or what it removes. */
export const ProposalChanges: React.FC<{ proposal: Proposal; ctx: ProposalContext }> = ({ proposal: p, ctx }) => {
  const payload = p.payload || {};
  const op = (p.op || '').toLowerCase();
  let body: React.ReactNode = null;
  if (op === 'update_artifact' || op === 'update' || op === 'create_artifact' || op === 'create') {
    const current = op.startsWith('update') && p.target_id ? ctx.artifacts[p.target_id] : undefined;
    const changes = fieldChanges(payload, current, ctx).filter((c) => !(op.startsWith('create') && (c.key === 'title' || c.key === 'type')));
    body =
      changes.length === 0 ? (
        <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>No field differs from the artifact as it is now.</span>
      ) : (
        changes.map((c) => <FieldChange key={c.key} label={c.label} before={c.before} after={c.after} />)
      );
  } else if (op === 'delete_artifact' || op === 'delete') {
    const current = p.target_id ? ctx.artifacts[p.target_id] : undefined;
    body = (
      <span style={{ fontSize: 13, color: 'var(--danger-strong)' }}>
        Deletes this artifact{current?.body ? ':' : '.'}
        {current?.body && <span style={{ ...block('var(--tint-red)', 'var(--tint-red-border)', true), display: 'block', marginTop: 4 }}>{current.body}</span>}
      </span>
    );
  } else if (op === 'delete_link') {
    body = <span style={{ fontSize: 13, color: 'var(--danger-strong)' }}>Removes this traceability link.</span>;
  } else if (op === 'record_test_result' && payload.notes) {
    body = <FieldChange label="Notes" after={show(payload.notes)} />;
  }
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      {body}
      <details>
        <summary style={{ fontSize: 12, color: 'var(--text-muted)', cursor: 'pointer', minHeight: 32, display: 'flex', alignItems: 'center' }}>
          Raw payload
        </summary>
        <pre
          style={{
            background: 'var(--surface-alt)',
            border: '1px solid var(--neutral-soft)',
            borderRadius: 4,
            padding: 10,
            fontSize: 12,
            overflow: 'auto',
            maxHeight: 260,
            margin: '4px 0 0',
          }}
        >
          {JSON.stringify(p.payload, null, 2)}
        </pre>
      </details>
    </div>
  );
};
