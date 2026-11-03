import React, { useMemo, useState } from 'react';
import { Artifact } from '../api/client';
import { DropZone, planMove } from '../utils/artifactDrag';
import { compareArtifacts, documentOrder, normalizeParentId } from '../utils/artifactSequence';
import { Modal } from './ui/Modal';
import { buttonStyle } from './ui/buttonStyles';
import { useViewport } from '../hooks/useViewport';

/** What the artifact actions menu can ask the view to do. */
export type ArtifactContextAction =
  | 'create-before'
  | 'create-after'
  | 'create-child'
  | 'copy'
  | 'paste-before'
  | 'paste-after'
  | 'duplicate';

/** Where the menu opens, and for which artifact. */
export interface ArtifactMenuAnchor {
  x: number;
  y: number;
  artifactId: string;
}

/** The artifacts before and after this one among its siblings, for Move up / down. */
export const siblingNeighbors = (allArtifacts: Artifact[], artifact: Artifact): { up?: string; down?: string } => {
  const parentId = normalizeParentId(artifact.parent_id);
  const siblings = allArtifacts
    .filter((item) => normalizeParentId(item.parent_id) === parentId)
    .sort(compareArtifacts);
  const index = siblings.findIndex((item) => item.id === artifact.id);
  return {
    up: index > 0 ? siblings[index - 1].id : undefined,
    down: index >= 0 && index < siblings.length - 1 ? siblings[index + 1].id : undefined,
  };
};

interface ArtifactActionsMenuProps {
  menu: ArtifactMenuAnchor | null;
  onClose: () => void;
  allArtifacts: Artifact[];
  /** Whether something has been copied: the paste entries are greyed without it. */
  canPaste: boolean;
  onContextMenuAction?: (action: ArtifactContextAction, artifact: Artifact) => void;
  /** 'swap' is Move up / down; the zones are Move to… (and a drag, in the tree). */
  onReorder: (sourceId: string, targetId: string, mode: 'swap' | DropZone) => void;
  /** A move into a parent opens that parent, so the moved artifact stays in view. */
  onRevealParent?: (parentId: string) => void;
}

type MenuItem = {
  key: string;
  label: string;
  ariaLabel?: string;
  separated?: boolean;
  disabled?: boolean;
  hint?: string;
  run: (artifact: Artifact) => void;
};

// The one menu of a row's actions: making artifacts beside or under it,
// moving it (up, down, or anywhere through the Move to… dialog, which is what
// a touch screen has instead of a drag), and copying it. A right click on a
// row and the row's ⋯ button both open it, in the tree and in the table.
export const ArtifactActionsMenu: React.FC<ArtifactActionsMenuProps> = ({
  menu,
  onClose,
  allArtifacts,
  canPaste,
  onContextMenuAction,
  onReorder,
  onRevealParent,
}) => {
  const touch = useViewport().coarsePointer;
  const [moveDialogFor, setMoveDialogFor] = useState<Artifact | null>(null);
  const [moveTargetId, setMoveTargetId] = useState('');
  const [moveZone, setMoveZone] = useState<DropZone>('after');
  // Every artifact in document order with its depth, for the Move to… picker,
  // from allArtifacts: a move has to be able to target a heading the current
  // filter is hiding.
  const flattened = useMemo(() => documentOrder(allArtifacts), [allArtifacts]);

  const openMoveDialog = (artifact: Artifact) => {
    // Default to "after the next sibling", the commonest small move.
    const neighbors = siblingNeighbors(allArtifacts, artifact);
    setMoveTargetId(neighbors.down || neighbors.up || '');
    setMoveZone(neighbors.down ? 'after' : 'before');
    setMoveDialogFor(artifact);
  };

  const submitMove = () => {
    if (!moveDialogFor || !moveTargetId) return;
    if (!planMove(allArtifacts, moveDialogFor.id, moveTargetId, moveZone)) return;
    if (moveZone === 'child') onRevealParent?.(moveTargetId);
    onReorder(moveDialogFor.id, moveTargetId, moveZone);
    setMoveDialogFor(null);
  };

  const target = menu ? allArtifacts.find((a) => a.id === menu.artifactId) : undefined;
  const neighbors = target ? siblingNeighbors(allArtifacts, target) : {};
  const act = (action: ArtifactContextAction) => (artifact: Artifact) => onContextMenuAction?.(action, artifact);
  const items: MenuItem[] = target
    ? [
        { key: 'create-before', label: 'Create before', run: act('create-before') },
        { key: 'create-after', label: 'Create after', run: act('create-after') },
        { key: 'create-child', label: 'Create new child', run: act('create-child') },
        {
          key: 'move-up',
          label: 'Move up',
          ariaLabel: `Move ${target.title} up`,
          separated: true,
          disabled: !neighbors.up,
          run: (a) => neighbors.up && onReorder(a.id, neighbors.up, 'swap'),
        },
        {
          key: 'move-down',
          label: 'Move down',
          ariaLabel: `Move ${target.title} down`,
          disabled: !neighbors.down,
          run: (a) => neighbors.down && onReorder(a.id, neighbors.down, 'swap'),
        },
        { key: 'move-to', label: 'Move to…', run: openMoveDialog },
        { key: 'copy', label: 'Copy', separated: true, run: act('copy') },
        { key: 'duplicate', label: 'Duplicate', run: act('duplicate') },
        { key: 'paste-before', label: 'Paste before', disabled: !canPaste, hint: 'Copy an artifact first', run: act('paste-before') },
        { key: 'paste-after', label: 'Paste after', disabled: !canPaste, hint: 'Copy an artifact first', run: act('paste-after') },
      ]
    : [];

  return (
    <>
      {menu && target && (
        <>
          <div
            style={{ position: 'fixed', inset: 0, zIndex: 999 }}
            onClick={onClose}
            onContextMenu={(e) => e.preventDefault()}
          />
          <div
            role="menu"
            aria-label={`Actions for ${target.title}`}
            style={{
              position: 'fixed',
              left: `${Math.min(menu.x, Math.max(8, window.innerWidth - 208))}px`,
              top: `${Math.min(menu.y, Math.max(8, window.innerHeight - 380))}px`,
              backgroundColor: 'var(--surface)',
              border: '1px solid var(--border)',
              borderRadius: 6,
              boxShadow: '0 4px 14px rgba(0,0,0,0.15)',
              zIndex: 1000,
              minWidth: 190,
              padding: '4px 0',
            }}
            data-help-key="requirements.create"
          >
            {items.map((item) => (
              <button
                key={item.key}
                type="button"
                role="menuitem"
                disabled={item.disabled}
                aria-label={item.ariaLabel}
                title={item.disabled ? item.hint : undefined}
                onClick={() => {
                  item.run(target);
                  onClose();
                }}
                style={{
                  display: 'block',
                  width: '100%',
                  padding: touch ? '12px 14px' : '7px 12px',
                  border: 'none',
                  backgroundColor: 'transparent',
                  textAlign: 'left',
                  cursor: item.disabled ? 'not-allowed' : 'pointer',
                  fontSize: touch ? 14 : 13,
                  color: item.disabled ? 'var(--text-muted)' : 'var(--text)',
                  // A rule above Move and above Copy separates making new
                  // artifacts, moving this one, and copying it.
                  borderTop: item.separated ? '1px solid var(--border-soft)' : undefined,
                }}
                onMouseEnter={(e) => {
                  if (!item.disabled) e.currentTarget.style.backgroundColor = 'var(--surface-hover)';
                }}
                onMouseLeave={(e) => (e.currentTarget.style.backgroundColor = 'transparent')}
              >
                {item.label}
              </button>
            ))}
          </div>
        </>
      )}

      {moveDialogFor && (
        <Modal title={`Move “${moveDialogFor.title}”`} width={420} onClose={() => setMoveDialogFor(null)}>
          <label htmlFor="move-target" style={{ fontSize: 12 }}>
            Relative to
          </label>
          <select
            id="move-target"
            value={moveTargetId}
            onChange={(e) => setMoveTargetId(e.target.value)}
            style={{ marginBottom: 12 }}
            data-help-key="requirements.create"
          >
            <option value="">Choose an artifact…</option>
            {flattened
              .filter(({ artifact }) => artifact.id !== moveDialogFor.id)
              .map(({ artifact, depth }) => (
                <option
                  key={artifact.id}
                  value={artifact.id}
                  disabled={!planMove(allArtifacts, moveDialogFor.id, artifact.id, moveZone)}
                >
                  {'  '.repeat(depth)}
                  {artifact.ref ? `${artifact.ref} ` : ''}
                  {artifact.title}
                </option>
              ))}
          </select>
          <div role="radiogroup" aria-label="Placement" style={{ display: 'flex', gap: 8, marginBottom: 16, flexWrap: 'wrap' }} data-help-key="requirements.create">
            {([
              { zone: 'before' as const, label: 'Before it' },
              { zone: 'after' as const, label: 'After it' },
              { zone: 'child' as const, label: 'Inside it' },
            ]).map((opt) => (
              <label
                key={opt.zone}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 6,
                  minHeight: 44,
                  padding: '0 12px',
                  border: `1px solid ${moveZone === opt.zone ? 'var(--tint-blue-border)' : 'var(--border)'}`,
                  borderRadius: 6,
                  background: moveZone === opt.zone ? 'var(--tint-blue)' : 'var(--surface)',
                  cursor: 'pointer',
                  flex: '1 1 auto',
                  fontWeight: 500,
                }}
              >
                <input
                  type="radio"
                  name="move-zone"
                  value={opt.zone}
                  checked={moveZone === opt.zone}
                  onChange={() => setMoveZone(opt.zone)}
                  style={{ width: 'auto' }}
                />
                {opt.label}
              </label>
            ))}
          </div>
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 10 }} data-help-key="requirements.create">
            <button type="button" style={{ ...buttonStyle('secondary'), minHeight: 44 }} onClick={() => setMoveDialogFor(null)}>
              Cancel
            </button>
            <button
              type="button"
              onClick={submitMove}
              disabled={!moveTargetId || !planMove(allArtifacts, moveDialogFor.id, moveTargetId, moveZone)}
              style={{
                ...buttonStyle('primary', {
                  disabled: !moveTargetId || !planMove(allArtifacts, moveDialogFor.id, moveTargetId, moveZone),
                }),
                minHeight: 44,
              }}
            >
              Move
            </button>
          </div>
        </Modal>
      )}
    </>
  );
};
