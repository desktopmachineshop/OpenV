import React, { useCallback, useEffect, useState } from 'react';
import { sharedProductsAPI } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { fromSharedProduct, RandomProduct } from '../utils/randomProduct';

// Voting on the community pool of demo products.
//
// The pool is the one cross-tenant surface in OpenV, and a vote is the
// lightest thing a person can say about someone else's joke: "keep this one".
// Two pieces live here — the arrow on the rolled card, and the leaderboards
// the roller can switch to — because both are about the same counts and both
// have the same rule behind them: one account, one vote, and only for a
// product that actually reached the pool.

/** Which list the roller is showing: chance, or one of the two leaderboards. */
export type RandomFilter = 'random' | 'top' | 'top_week';

/** The counts a vote settles on, in the client's camelCase. */
export interface VoteState {
  votes: number;
  votesWeek: number;
  voted: boolean;
}

/**
 * Why a product cannot be voted for, or '' when it can. Inventions are
 * published automatically, so a product with no shared id is one that never
 * made it (a rate limit, a name already taken, no network) and lives in this
 * browser alone — there is nothing for anyone else to vote on.
 */
export const voteDisabledReason = (product: RandomProduct): string =>
  product.sharedId
    ? ''
    : 'Kept in this browser only — a product has to reach the shared pool before anyone can vote for it.';

/**
 * The up-arrow and its count on the rolled product card.
 *
 * Pressing it toggles your own vote; the server is the authority on the
 * resulting counts, so the button renders what came back rather than guessing
 * one up or one down.
 */
export const ProductVoteButton: React.FC<{
  product: RandomProduct;
  onChange: (state: VoteState) => void;
  onError?: (message: string) => void;
}> = ({ product, onChange, onError }) => {
  const [busy, setBusy] = useState(false);
  const disabledReason = voteDisabledReason(product);
  const voted = !!product.voted;
  const votes = product.votes || 0;
  const votesWeek = product.votesWeek || 0;

  const toggle = async () => {
    if (!product.sharedId || busy) return;
    setBusy(true);
    try {
      const res = voted
        ? await sharedProductsAPI.unvote(product.sharedId)
        : await sharedProductsAPI.vote(product.sharedId);
      onChange({ votes: res.data.votes, votesWeek: res.data.votes_week, voted: res.data.voted });
    } catch (err: any) {
      onError?.(apiErrorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, flexShrink: 0 }}>
      <button
        type="button"
        className="vote-button"
        aria-label="Vote for this product"
        aria-pressed={voted}
        disabled={!!disabledReason || busy}
        onClick={toggle}
        title={
          disabledReason ||
          (voted ? 'You voted for this product — press again to take it back' : 'Vote for this product')
        }
        style={{
          background: voted ? 'var(--accent)' : 'var(--surface)',
          color: voted ? '#fff' : 'var(--text)',
          border: '1px solid var(--border)',
          borderRadius: 4,
          cursor: disabledReason ? 'not-allowed' : 'pointer',
          opacity: disabledReason ? 0.5 : 1,
          fontSize: 13,
          fontWeight: 600,
          padding: '4px 10px',
        }}
      >
        <span aria-hidden>▲</span> {votes}
      </button>
      <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>· {votesWeek} this week</span>
    </span>
  );
};

/**
 * The five most-voted products, all time or this week.
 *
 * The server does the ordering and the windowing (it owns the clock), so this
 * only asks for the sort it is showing and renders what comes back. An empty
 * answer means nobody has voted in that window — which is worth saying, since
 * an empty list otherwise reads as a failure.
 */
export const TopSharedProducts: React.FC<{
  sort: 'top' | 'top_week';
  /** Bumped by the caller after a vote, to re-read the standings. */
  refreshKey?: number;
  /** Applies one entry to the new-project form. */
  onUse: (product: RandomProduct) => void;
  /** The shared id currently applied to the form, if any. */
  selectedId?: string;
}> = ({ sort, refreshKey, onUse, selectedId }) => {
  const [products, setProducts] = useState<RandomProduct[] | null>(null);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    try {
      const res = await sharedProductsAPI.list({ sort, limit: 5 });
      setProducts((res.data || []).map(fromSharedProduct));
      setError('');
    } catch (err: any) {
      setProducts([]);
      setError(apiErrorMessage(err));
    }
  }, [sort]);

  useEffect(() => {
    load();
  }, [load, refreshKey]);

  if (products === null) {
    return (
      <div style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 12 }}>Loading the standings…</div>
    );
  }
  if (error) {
    return (
      <div style={{ fontSize: 13, color: 'var(--danger-strong)', marginBottom: 12 }}>
        Could not load the standings: {error}
      </div>
    );
  }
  if (products.length === 0) {
    return (
      <div style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 12 }}>
        {sort === 'top_week'
          ? 'No votes yet this week — vote for a product and it appears here.'
          : 'No votes yet — vote for a product and it appears here.'}
      </div>
    );
  }

  return (
    <ul
      aria-label={sort === 'top_week' ? 'Top 5 products this week' : 'Top 5 products all time'}
      style={{ listStyle: 'none', margin: '0 0 12px', padding: 0, display: 'grid', gap: 6 }}
    >
      {products.map((product) => (
        <li
          key={product.sharedId}
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            flexWrap: 'wrap',
            gap: 8,
            background: 'var(--surface-alt)',
            border: `1px solid ${product.sharedId === selectedId ? 'var(--accent)' : 'var(--border-soft)'}`,
            borderRadius: 4,
            padding: '6px 10px',
            fontSize: 13,
          }}
        >
          <span style={{ minWidth: 0 }}>
            <strong>{product.name}</strong>{' '}
            <span style={{ color: 'var(--text-muted)' }}>({product.category})</span>
          </span>
          <span style={{ display: 'flex', alignItems: 'center', gap: 8, flexShrink: 0 }}>
            <span style={{ color: 'var(--text-muted)' }}>
              <span aria-hidden>▲</span>{' '}
              {sort === 'top_week'
                ? `${product.votesWeek || 0} this week`
                : `${product.votes || 0} ${product.votes === 1 ? 'vote' : 'votes'}`}
            </span>
            <button
              type="button"
              className="button-secondary button"
              style={{ width: 'auto', padding: '6px 10px', fontSize: 12 }}
              onClick={() => onUse(product)}
            >
              Use this
            </button>
          </span>
        </li>
      ))}
    </ul>
  );
};
