import { useEffect, useState } from 'react';
import { orgsAPI } from '../api/client';
import { useAppStore } from '../state/store';

/**
 * The biggest single figure this workspace may upload.
 *
 * The number is the server's (orgs.LimitMaxUploadMB), not a constant compiled
 * into the app: it follows the workspace's plan, and a build that hard-coded
 * one tier's ceiling would refuse a file another tier is entitled to upload —
 * which is how a 25 MB constant came to turn away the CAD the format
 * catalogue advertises (issue #364).
 *
 * The client check exists to fail fast and say something useful, not to
 * enforce anything: the API is what refuses an oversized upload, and it
 * answers with the workspace's real number. So while the limit is unknown,
 * this reports the fallback rather than blocking the upload.
 */

/** What the app assumes before the workspace's own limit has arrived. */
export const FALLBACK_UPLOAD_BYTES = 128 * 1024 * 1024;

/** Cached per workspace: the limit changes with the plan, not with the page. */
const cache = new Map<string, number>();

export const useUploadLimit = (): number => {
  const activeOrgId = useAppStore((s) => s.activeOrgId);
  const [limit, setLimit] = useState<number>(() =>
    activeOrgId ? cache.get(activeOrgId) ?? FALLBACK_UPLOAD_BYTES : FALLBACK_UPLOAD_BYTES
  );

  useEffect(() => {
    if (!activeOrgId) {
      setLimit(FALLBACK_UPLOAD_BYTES);
      return;
    }
    const cached = cache.get(activeOrgId);
    if (cached !== undefined) {
      setLimit(cached);
      return;
    }
    let cancelled = false;
    orgsAPI
      .limits(activeOrgId)
      .then((res) => {
        const entry = (res.data.limits || []).find((l) => l.key === 'max_upload_mb');
        // Unlimited, or a limit this server does not report: the server is
        // still the authority, so assume the upload may proceed.
        const bytes =
          !entry || entry.unlimited || entry.limit <= 0
            ? Number.POSITIVE_INFINITY
            : entry.limit * 1024 * 1024;
        cache.set(activeOrgId, bytes);
        if (!cancelled) setLimit(bytes);
      })
      .catch(() => {
        /* the API refuses what it must; a failed lookup is not a refusal */
      });
    return () => {
      cancelled = true;
    };
  }, [activeOrgId]);

  return limit;
};
