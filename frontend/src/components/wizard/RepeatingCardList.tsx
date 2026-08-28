import React from 'react';

interface RepeatingCardListProps<T> {
  items: T[];
  onChange: (items: T[]) => void;
  makeNew: () => T;
  addLabel: string;
  emptyText?: string;
  /** Render the editable body of one card. Call update(patch) to modify the item. */
  renderItem: (item: T, index: number, update: (patch: Partial<T>) => void) => React.ReactNode;
  /** Optional guard — return false to hide the remove button for an item. */
  canRemove?: (item: T, index: number) => boolean;
}

// Generic add/remove card list used by wizard steps (personas, hazards, ...).
export function RepeatingCardList<T>({
  items,
  onChange,
  makeNew,
  addLabel,
  emptyText,
  renderItem,
  canRemove,
}: RepeatingCardListProps<T>): React.ReactElement {
  const update = (index: number, patch: Partial<T>) => {
    const next = items.slice();
    next[index] = { ...next[index], ...patch };
    onChange(next);
  };

  const remove = (index: number) => {
    onChange(items.filter((_, i) => i !== index));
  };

  return (
    <div>
      {items.length === 0 && emptyText && (
        <div style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 12 }}>{emptyText}</div>
      )}
      {items.map((item, index) => (
        <div
          key={index}
          style={{
            background: 'var(--surface)',
            border: '1px solid var(--border)',
            borderRadius: 4,
            padding: 16,
            marginBottom: 12,
            position: 'relative',
          }}
        >
          {(!canRemove || canRemove(item, index)) && (
            <button
              onClick={() => remove(index)}
              title="Remove"
              style={{
                position: 'absolute',
                top: 8,
                right: 8,
                background: 'none',
                border: 'none',
                color: 'var(--danger)',
                cursor: 'pointer',
                fontSize: 16,
                width: 'auto',
                padding: 4,
              }}
            >
              ✕
            </button>
          )}
          {renderItem(item, index, (patch) => update(index, patch))}
        </div>
      ))}
      <button
        className="button-secondary"
        onClick={() => onChange([...items, makeNew()])}
        style={{ background: 'var(--accent)' }}
      >
        + {addLabel}
      </button>
    </div>
  );
}
