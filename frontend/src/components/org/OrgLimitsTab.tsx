import React, { useEffect, useState } from 'react';
import { LimitUsage, Org, WorkspaceLimits, orgsAPI } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { ErrorBanner } from '../ui';

/**
 * What this workspace is allowed to do.
 *
 * Limits are shown before anybody hits one, on purpose. A ceiling nobody can
 * see is only discovered at the moment it refuses somebody — usually while
 * they are trying to do something — and that is the least useful moment to
 * learn it exists.
 */

/** Renders a limit's number in its own unit, so storage does not read as a
 *  count of things. */
export const formatLimit = (value: number, unit: LimitUsage['unit']): string => {
  switch (unit) {
    case 'mb':
      return value >= 1024 ? `${(value / 1024).toFixed(value % 1024 === 0 ? 0 : 1)} GB` : `${value} MB`;
    case 'minutes': {
      if (value < 60 || value % 60 !== 0) return `${value} minutes`;
      const hours = value / 60;
      return hours === 1 ? '1 hour' : `${hours} hours`;
    }
    case 'cpus':
      return value === 1 ? '1 CPU' : `${value} CPUs`;
    default:
      return `${value}`;
  }
};

/** The one-line reading of a limit: how much of it is gone, or that there is
 *  no ceiling at all. */
export const limitSummary = (limit: LimitUsage): string => {
  if (limit.unlimited) {
    return limit.used === undefined
      ? 'No limit'
      : `${formatLimit(limit.used, limit.unit)} used — no limit`;
  }
  if (limit.used === undefined) return formatLimit(limit.limit, limit.unit);
  return `${formatLimit(limit.used, limit.unit)} of ${formatLimit(limit.limit, limit.unit)}`;
};

/** Fraction of a capped limit that is used, or null when there is nothing to
 *  draw a bar for. */
export const usedFraction = (limit: LimitUsage): number | null => {
  if (limit.unlimited || limit.used === undefined || limit.limit <= 0) return null;
  return Math.min(1, limit.used / limit.limit);
};

/**
 * Whether being at or near this ceiling is worth colouring as a warning.
 *
 * A personal workspace's single seat is full the moment it exists and can
 * never be anything else, so painting it red would teach people to ignore the
 * colour on the limits that do mean something.
 */
export const isAlarming = (limit: LimitUsage): boolean => {
  if (limit.fixed) return false;
  const fraction = usedFraction(limit);
  return fraction !== null && fraction >= 0.8;
};

const barColour = (fraction: number, limit: LimitUsage): string => {
  if (limit.fixed) return 'var(--text-muted)';
  if (fraction >= 1) return 'var(--danger)';
  if (fraction >= 0.8) return 'var(--warning)';
  return 'var(--primary)';
};

interface OrgLimitsTabProps {
  org: Org;
}

export const OrgLimitsTab: React.FC<OrgLimitsTabProps> = ({ org }) => {
  const [data, setData] = useState<WorkspaceLimits | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    orgsAPI
      .limits(org.id)
      .then((res) => {
        if (cancelled) return;
        const body = res.data;
        if (!body || !Array.isArray(body.limits)) {
          // A response we cannot read is an error to show, not a crash: this
          // panel sits inside workspace settings and must not take the rest
          // of the page down with it.
          setError('The workspace limits could not be read.');
          return;
        }
        setData(body);
      })
      .catch((err: any) => {
        if (!cancelled) setError(`Failed to load the limits: ${apiErrorMessage(err)}`);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [org.id]);

  return (
    <div>
      <h3 style={{ marginBottom: 4 }}>Limits</h3>
      <p style={{ color: 'var(--text-muted)', fontSize: 14, marginTop: 0, maxWidth: 640 }}>
        What this workspace is allowed to do. Anything shown as “No limit” has
        no ceiling at all.
        {data && !data.self_hosted
          ? ' Upgrading the workspace’s plan raises these.'
          : data
            ? ' This deployment sets its own limits, so an administrator can change any of them.'
            : ''}
      </p>

      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 16 }} />

      {loading ? (
        <p style={{ color: 'var(--text-muted)' }}>Loading…</p>
      ) : !data ? null : (
        <div style={{ display: 'grid', gap: 14 }}>
          {data.limits.map((limit) => {
            const fraction = usedFraction(limit);
            const alarming = isAlarming(limit);
            return (
              <div key={limit.key} className="card" style={{ padding: 14 }}>
                <div
                  style={{
                    display: 'flex',
                    flexWrap: 'wrap',
                    gap: 8,
                    alignItems: 'baseline',
                    justifyContent: 'space-between',
                  }}
                >
                  <strong>{limit.label}</strong>
                  <span
                    style={{
                      fontSize: 13,
                      color: alarming ? barColour(fraction as number, limit) : 'var(--text-muted)',
                      fontWeight: alarming ? 600 : 400,
                    }}
                  >
                    {limitSummary(limit)}
                  </span>
                </div>
                {fraction !== null && (
                  <div
                    role="presentation"
                    style={{
                      marginTop: 8,
                      height: 6,
                      borderRadius: 3,
                      background: 'var(--border)',
                      overflow: 'hidden',
                    }}
                  >
                    <div
                      style={{
                        width: `${Math.round(fraction * 100)}%`,
                        height: '100%',
                        background: barColour(fraction, limit),
                      }}
                    />
                  </div>
                )}
                <p style={{ margin: '8px 0 0', fontSize: 13, color: 'var(--text-muted)' }}>
                  {limit.description}
                </p>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
};
