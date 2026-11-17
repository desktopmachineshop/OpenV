import React from 'react';
import type { Artifact, ArtifactTypeDef } from '../api/client';
import { ARTIFACT_TYPE_CATALOG } from '../generated/contract';
import { typeLabel } from '../utils/artifactFacts';
import type { PickerOption } from './ui/Picker';

// The options the artifact editor's pickers offer, built from what the
// editor already loads: the project's artifacts, the type catalogue and the
// owners. Kept out of ArtifactEditor.tsx so the form stays a form.

const dot = (color: string) => (
  <span aria-hidden="true" style={{ width: 9, height: 9, borderRadius: '50%', flex: 'none', background: color }} />
);

const refTag = (text: string) => (
  <span style={{ flex: 'none', minWidth: 52, fontSize: 12, color: 'var(--text-muted)', fontVariantNumeric: 'tabular-nums' }}>{text}</span>
);

/**
 * Every type in the catalogue's order, with its description and colour once
 * the meta endpoint has answered (the generated catalogue carries neither).
 */
export const typeOptions = (meta: ArtifactTypeDef[]): PickerOption[] =>
  ARTIFACT_TYPE_CATALOG.map((t) => {
    const def = meta.find((m) => m.value === t.value);
    return {
      value: t.value,
      label: def?.label || t.label,
      hint: def?.description || undefined,
      lead: dot(def?.color || 'var(--border)'),
    };
  });

/**
 * The artifacts this one may sit under, in document order, indented by how
 * deep each sits, and found by ref as well as title. It and its descendants
 * are left out: a parent cannot be its own child.
 */
export const parentOptions = (artifacts: Artifact[], selfId?: string): PickerOption[] => {
  const byId = new Map(artifacts.map((a) => [a.id, a]));
  const depth = (a: Artifact): number => {
    let d = 0;
    const seen = new Set<string>();
    for (let p = a.parent_id; p && byId.has(p) && !seen.has(p); p = byId.get(p)!.parent_id) {
      seen.add(p);
      d++;
    }
    return d;
  };
  const under = (a: Artifact): boolean => {
    const seen = new Set<string>();
    for (let p: string | null | undefined = a.id; p && !seen.has(p); p = byId.get(p)?.parent_id) {
      if (p === selfId) return true;
      seen.add(p);
    }
    return false;
  };
  return artifacts
    .filter((a) => !selfId || !under(a))
    .map((a) => {
      const tag = [a.doc_number, a.ref].filter(Boolean).join(' · ');
      return {
        value: a.id,
        label: a.title || 'Untitled',
        keywords: `${a.ref || ''} ${a.doc_number || ''}`,
        meta: a.type === 'heading' ? undefined : typeLabel(a.type),
        depth: Math.min(depth(a), 6),
        lead: tag ? refTag(tag) : undefined,
      };
    });
};

const avatar = (name: string, square: boolean) => (
  <span
    aria-hidden="true"
    style={{
      width: 22, height: 22, flex: 'none', borderRadius: square ? 5 : '50%', display: 'grid', placeItems: 'center',
      fontSize: 10, fontWeight: 700, color: 'var(--accent-text)',
      background: 'color-mix(in srgb, var(--accent) 14%, transparent)',
    }}
  >
    {name.split(/\s+/).map((w) => w[0]).join('').slice(0, 2).toUpperCase()}
  </span>
);

/** Parties first, then members, each name once, under their own headings. */
export const ownerOptions = (parties: string[], members: string[]): PickerOption[] => {
  const seen = new Set<string>();
  const out: PickerOption[] = [];
  const add = (name: string, group: 'Parties' | 'Members') => {
    const key = name.trim().toLowerCase();
    if (!key || seen.has(key)) return;
    seen.add(key);
    out.push({ value: name, label: name, group, lead: avatar(name, group === 'Parties') });
  };
  parties.forEach((p) => add(p, 'Parties'));
  members.forEach((m) => add(m, 'Members'));
  return out;
};
