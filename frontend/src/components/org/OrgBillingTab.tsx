import React, { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { BillingState, Org, PublicPlan, billingAPI, orgsAPI } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { useFeature } from '../../hooks/useFeature';
import { ErrorBanner } from '../ui';

/**
 * The workspace's subscription: what it is on, what that costs, and the two
 * doors out of here — the provider's checkout to buy, its portal to manage.
 *
 * Nothing about money lives in this component beyond what the platform
 * confirmed with the provider: no card field, no provider script, no key.
 * A purchase is a redirect to a page of the provider's and a return here,
 * where the return handler binds the completed checkout before the tab
 * renders, so the entitlement is live before the buyer reads the page.
 */

/** The plans a workspace admin can buy here, in the order they are shown. */
export const PLAN_LABELS: Record<string, string> = {
  single: 'Single User',
  free: 'Single User',
  business_lite: 'Business Lite',
  business: 'Business',
  team: 'Business',
  enterprise: 'Enterprise',
  open_source: 'Open source',
  self_host: 'Self-hosted',
};

export const planLabel = (plan: string): string => PLAN_LABELS[plan] || (plan ? plan : 'Single User');

/** Statuses under which the workspace holds a subscription worth managing. */
export const LIVE_STATUSES = ['trialing', 'active', 'past_due'];

export const formatAmount = (minor: number, currency: string): string =>
  new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: currency.toUpperCase(),
    minimumFractionDigits: minor % 100 === 0 ? 0 : 2,
    maximumFractionDigits: 2,
  }).format(minor / 100);

/** The sentence under a plan choice: what it costs for this workspace at
 *  the chosen interval and currency, seats multiplied in for a per-seat
 *  plan, or null where the catalogue has no such price. */
export const pricePreview = (plan: PublicPlan, interval: string, currency: string, seats: number): string | null => {
  const price = plan.intervals?.[interval];
  const amount = price?.amounts?.[currency];
  if (amount === undefined) return null;
  const tax = price?.tax_behavior === 'exclusive' ? ', plus VAT where it applies' : '';
  const per = interval === 'year' ? 'year' : 'month';
  if (!plan.per_seat) return `${formatAmount(amount, currency)} per ${per}${tax}`;
  const n = Math.max(1, seats);
  const total = formatAmount(amount * n, currency);
  return `${n} ${n === 1 ? 'seat' : 'seats'} × ${formatAmount(amount, currency)} = ${total} per ${per}${tax}`;
};

/** The saving a yearly price makes against twelve monthly ones, as a whole
 *  percentage, or null when there is nothing to compare. */
export const yearlySaving = (plan: PublicPlan, currency: string): number | null => {
  const month = plan.intervals?.month?.amounts?.[currency];
  const year = plan.intervals?.year?.amounts?.[currency];
  if (!month || !year || year >= month * 12) return null;
  return Math.round((1 - year / (month * 12)) * 100);
};

const formatDate = (iso?: string): string => {
  if (!iso) return '';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString(undefined, { day: 'numeric', month: 'long', year: 'numeric' });
};

/** What a status means, in the words the tab uses. */
export const statusLine = (state: BillingState): { title: string; body: string; tone: 'ok' | 'warn' | 'danger' | 'muted' } => {
  const b = state.billing;
  const plan = planLabel(state.plan);
  switch (b.status) {
    case 'trialing':
      return {
        title: `${plan} — trial`,
        body: b.period_end
          ? `The trial ends on ${formatDate(b.period_end)}; the card on file is charged then unless the subscription is cancelled first.`
          : 'On trial.',
        tone: 'ok',
      };
    case 'active':
      return {
        title: plan,
        body: b.cancel_at_period_end
          ? `Cancelled: the plan stays until ${formatDate(b.period_end)}, then the workspace returns to Single User.`
          : b.period_end
            ? `Renews on ${formatDate(b.period_end)}.`
            : 'Active.',
        tone: b.cancel_at_period_end ? 'warn' : 'ok',
      };
    case 'past_due':
      return {
        title: `${plan} — payment failed`,
        body: 'A payment failed. Your workspace keeps working — update the card to avoid interruption.',
        tone: 'danger',
      };
    case 'canceled':
    case 'unpaid':
    case 'incomplete':
    case 'paused':
    case 'disputed':
      return {
        title: `Single User — the ${plan} subscription ended`,
        body: 'Everything you have made is still here and still exportable. Anything beyond the Single User limits is read-only until the workspace subscribes again or trims back under them.',
        tone: 'warn',
      };
    default:
      return { title: 'Single User', body: 'The free tier. Nothing is billed.', tone: 'muted' };
  }
};

const toneColour: Record<string, string> = {
  ok: 'var(--success-text)',
  warn: 'var(--warning)',
  danger: 'var(--danger)',
  muted: 'var(--text-muted)',
};

interface OrgBillingTabProps {
  org: Org;
  isAdmin: boolean;
}

export const OrgBillingTab: React.FC<OrgBillingTabProps> = ({ org, isAdmin }) => {
  const [searchParams, setSearchParams] = useSearchParams();
  const [state, setState] = useState<BillingState | null>(null);
  const [seats, setSeats] = useState<number>(1);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [plan, setPlan] = useState('business_lite');
  const [interval, setInterval] = useState('month');
  const [currency, setCurrency] = useState('gbp');
  const [changing, setChanging] = useState(false);
  const pickerOpen = useFeature('workspace-billing');

  // The return from checkout: bind the completed session before anything
  // renders, then take the parameters out of the URL so a refresh does not
  // bind it twice. A failed bind is not an error to the buyer — the money
  // moved — so the tab says the plan is on its way and the reconciler,
  // which runs every few minutes, lands it.
  useEffect(() => {
    if (!isAdmin) {
      setLoading(false);
      return;
    }
    let cancelled = false;
    const outcome = searchParams.get('checkout');
    const sessionId = searchParams.get('session_id') || '';
    const strip = () =>
      setSearchParams(
        (prev) => {
          prev.delete('checkout');
          prev.delete('session_id');
          return prev;
        },
        { replace: true }
      );
    const load = outcome === 'done' && sessionId
      ? billingAPI
          .refresh(org.id, sessionId)
          .then((res) => {
            if (!cancelled) setNotice('Payment received — your workspace is on its new plan.');
            return res;
          })
          .catch(() => {
            if (!cancelled) setNotice('Payment received — your plan will update within a few minutes.');
            return billingAPI.state(org.id);
          })
      : billingAPI.state(org.id);
    if (outcome === 'cancelled') setNotice('Checkout was cancelled. Nothing was charged.');
    if (outcome) strip();
    load
      .then((res) => {
        if (cancelled) return;
        const body = res.data;
        if (!body || !body.billing) {
          setError('The billing state could not be read.');
          return;
        }
        setState(body);
        if (body.plans?.currencies?.length && !body.plans.currencies.includes(currency)) {
          setCurrency(body.plans.currencies[0]);
        }
        if (LIVE_STATUSES.includes(body.billing.status)) {
          setPlan(body.plan);
          setInterval(body.billing.interval || 'month');
        }
      })
      .catch((err: any) => {
        if (!cancelled) setError(`Failed to load billing: ${apiErrorMessage(err)}`);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    orgsAPI
      .limits(org.id)
      .then((res) => {
        if (cancelled) return;
        const members = res.data?.limits?.find((l) => l.key === 'max_members');
        if (members?.used !== undefined) setSeats(Math.max(1, members.used));
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [org.id, isAdmin]);

  const catalogue = useMemo(() => state?.plans?.plans ?? [], [state]);
  const chosen = catalogue.find((p) => p.plan === plan) || catalogue[0];

  const redirect = (fn: () => Promise<{ data: { url: string } }>) => {
    setBusy(true);
    setError('');
    fn()
      .then((res) => {
        if (res.data?.url) window.location.assign(res.data.url);
        else setError('The billing provider did not answer with a page to open.');
      })
      .catch((err: any) => setError(apiErrorMessage(err)))
      .finally(() => setBusy(false));
  };

  const submitChange = () => {
    if (!state) return;
    setBusy(true);
    setError('');
    billingAPI
      .change(org.id, plan, interval)
      .then((res) => {
        setState(res.data);
        setChanging(false);
        setNotice(`The subscription is now ${planLabel(res.data.plan)}, billed ${res.data.billing.interval === 'year' ? 'yearly' : 'monthly'}. The difference is prorated on the next invoice.`);
      })
      .catch((err: any) => setError(apiErrorMessage(err)))
      .finally(() => setBusy(false));
  };

  if (!isAdmin) {
    return (
      <div>
        <h3 style={{ marginBottom: 4 }}>Billing</h3>
        <p style={{ color: 'var(--text-muted)', fontSize: 14 }}>Only a workspace admin can see or change the subscription.</p>
      </div>
    );
  }

  const live = state ? LIVE_STATUSES.includes(state.billing.status) : false;
  const status = state ? statusLine(state) : null;
  const canBuy = state ? !state.self_hosted && !state.granted && !live && state.plans?.billing_enabled : false;
  const picker = (() => {
    if (!state || !chosen) return null;
    const preview = pricePreview(chosen, interval, currency, seats);
    const saving = yearlySaving(chosen, currency);
    const currencies = state.plans?.currencies ?? [];
    return (
      <div className="card" style={{ padding: 16, display: 'grid', gap: 12, maxWidth: 560 }}>
        <div role="radiogroup" aria-label="Plan" style={{ display: 'grid', gap: 8 }}>
          {catalogue.map((p) => (
            <label key={p.plan} style={{ display: 'flex', gap: 10, alignItems: 'flex-start', cursor: 'pointer' }}>
              <input type="radio" name="billing-plan" value={p.plan} checked={plan === p.plan} onChange={() => setPlan(p.plan)} style={{ marginTop: 4, width: 'auto' }} />
              <span>
                <strong>{planLabel(p.plan)}</strong>
                <span style={{ display: 'block', fontSize: 13, color: 'var(--text-muted)' }}>
                  {p.plan === 'business'
                    ? 'Shared workspaces, teams, per-project access, workspace budget and usage. Billed per member and pending invitation; the count follows the Members tab.'
                    : 'Always-on hosted automation and a larger cloud runner, for one person. One flat price.'}
                </span>
              </span>
            </label>
          ))}
        </div>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 12, alignItems: 'center' }}>
          <label style={{ display: 'flex', gap: 6, alignItems: 'center', fontSize: 14 }}>
            <input type="radio" name="billing-interval" value="month" checked={interval === 'month'} onChange={() => setInterval('month')} style={{ width: 'auto' }} />
            Monthly
          </label>
          <label style={{ display: 'flex', gap: 6, alignItems: 'center', fontSize: 14 }}>
            <input type="radio" name="billing-interval" value="year" checked={interval === 'year'} onChange={() => setInterval('year')} style={{ width: 'auto' }} />
            Yearly{saving ? ` (save ${saving}%)` : ''}
          </label>
          {!live && currencies.length > 1 && (
            <label style={{ display: 'flex', gap: 6, alignItems: 'center', fontSize: 14 }}>
              Currency
              <select value={currency} onChange={(e) => setCurrency(e.target.value)} aria-label="Currency" style={{ width: 'auto' }}>
                {currencies.map((c) => (
                  <option key={c} value={c}>
                    {c.toUpperCase()}
                  </option>
                ))}
              </select>
            </label>
          )}
        </div>
        <p data-testid="billing-preview" style={{ margin: 0, fontSize: 14 }}>
          {preview || 'This plan is not offered at that interval in that currency.'}
        </p>
        {live ? (
          <div style={{ display: 'flex', gap: 8 }}>
            <button type="button" className="btn btn-primary" disabled={busy || !preview} onClick={submitChange} style={{ width: 'auto' }}>
              {busy ? 'Changing…' : 'Change plan'}
            </button>
            <button type="button" className="btn" disabled={busy} onClick={() => setChanging(false)} style={{ width: 'auto' }}>
              Keep the current plan
            </button>
          </div>
        ) : (
          <div>
            <button
              type="button"
              className="btn btn-primary"
              disabled={busy || !preview}
              onClick={() => redirect(() => billingAPI.checkout(org.id, plan, interval, currency))}
              style={{ width: 'auto' }}
            >
              {busy ? 'Opening Stripe…' : 'Continue to Stripe'}
            </button>
            <p style={{ margin: '8px 0 0', fontSize: 13, color: 'var(--text-muted)' }}>
              Payment is taken on a Stripe page; no card detail reaches OpenV. A first subscription starts with a 14-day trial, and the currency is fixed
              by the first purchase. Promotion codes are entered at checkout. VAT is worked out there and a business VAT number is accepted.
            </p>
          </div>
        )}
      </div>
    );
  })();

  return (
    <div>
      <h3 style={{ marginBottom: 4 }}>Billing</h3>
      <p style={{ color: 'var(--text-muted)', fontSize: 14, marginTop: 0, maxWidth: 640 }}>
        The workspace’s plan and subscription. Your data is never behind the paywall: export stays available on every plan, in every state.
      </p>

      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 16 }} />
      {notice && (
        <div role="status" className="card" style={{ padding: 12, marginBottom: 16, borderLeft: '4px solid var(--success-text)', fontSize: 14 }}>
          {notice}
        </div>
      )}

      {loading ? (
        <p style={{ color: 'var(--text-muted)' }}>Loading…</p>
      ) : !state || !status ? null : (
        <div style={{ display: 'grid', gap: 16 }}>
          <div className="card" style={{ padding: 16, borderLeft: `4px solid ${toneColour[status.tone]}` }}>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'baseline', justifyContent: 'space-between' }}>
              <strong data-testid="billing-status">{status.title}</strong>
              {live && (
                <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>
                  {state.billing.interval === 'year' ? 'Billed yearly' : 'Billed monthly'}
                  {state.plan === 'business' || state.plan === 'team'
                    ? state.billing.seats
                      ? ` · billing ${state.billing.seats} ${state.billing.seats === 1 ? 'seat' : 'seats'} · ${seats} ${seats === 1 ? 'member' : 'members and invitations'}`
                      : ' · seats syncing…'
                    : ''}
                </span>
              )}
            </div>
            <p style={{ margin: '8px 0 0', fontSize: 14 }}>{status.body}</p>
            {state.billing.grandfathered && (
              <p style={{ margin: '8px 0 0', fontSize: 13, color: 'var(--text-muted)' }}>
                This workspace was created during the alpha and keeps every tier’s features free, whatever it subscribes to.
              </p>
            )}
            {state.self_hosted && (
              <p style={{ margin: '8px 0 0', fontSize: 13, color: 'var(--text-muted)' }}>
                This deployment is self-hosted: every feature is included and there is nothing to buy.
              </p>
            )}
            {state.granted && !state.self_hosted && (
              <p style={{ margin: '8px 0 0', fontSize: 13, color: 'var(--text-muted)' }}>
                The {planLabel(state.plan)} plan was arranged with us directly and is invoiced by hand. Email us to change it.
              </p>
            )}
            {live && (
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, marginTop: 12 }}>
                <button
                  type="button"
                  className={state.billing.status === 'past_due' ? 'btn btn-primary' : 'btn'}
                  disabled={busy}
                  onClick={() => redirect(() => billingAPI.portal(org.id))}
                  style={{ width: 'auto' }}
                >
                  {state.billing.status === 'past_due' ? 'Update the card' : 'Manage billing'}
                </button>
                {!changing && (
                  <button type="button" className="btn" disabled={busy} onClick={() => setChanging(true)} style={{ width: 'auto' }}>
                    Change plan
                  </button>
                )}
              </div>
            )}
            {live && (
              <p style={{ margin: '10px 0 0', fontSize: 13, color: 'var(--text-muted)' }}>
                Invoices, the payment card, the billing address and VAT number, the receipt email and cancellation are all in Manage billing.
                Receipts go to the email given at checkout; change it there if the admin who subscribed leaves.
              </p>
            )}
          </div>

          {live && changing && picker}

          {canBuy && (state.billing.status !== 'none' || pickerOpen) && picker}
          {canBuy && state.billing.status === 'none' && !pickerOpen && (
            <p style={{ fontSize: 14, color: 'var(--text-muted)' }}>
              Subscribing reaches stable-channel workspaces at their next stable release. Switch the workspace to nightly in General settings to subscribe now.
            </p>
          )}
          {!state.self_hosted && !state.granted && !live && !state.plans?.billing_enabled && (
            <p style={{ fontSize: 14, color: 'var(--text-muted)' }}>
              Paid plans are not on sale on this instance yet. See <a href="/pricing" target="_blank" rel="noreferrer">the tiers</a> for what they will include.
            </p>
          )}
        </div>
      )}
    </div>
  );
};
