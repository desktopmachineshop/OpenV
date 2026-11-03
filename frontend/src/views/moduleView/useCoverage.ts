import { useCallback, useEffect, useState } from 'react';
import { vvAPI, CoverageEntry } from '../../api/client';

/**
 * The project's V&V coverage report, for the verification column, the figures
 * above the list and the detail panel's test list: the live project's, or the
 * baseline's while one is shown. Best-effort, as the quality scores are: a
 * failure leaves the column blank rather than blocking the list.
 */
export function useCoverage(projectId: string, baselineId: string) {
  const [entries, setEntries] = useState<CoverageEntry[]>([]);

  const loadCoverage = useCallback(async () => {
    if (!projectId) return;
    try {
      const response = baselineId !== 'live' ? await vvAPI.coverage(projectId, baselineId) : await vvAPI.coverage(projectId);
      setEntries(response.data?.entries || []);
    } catch {
      setEntries([]);
    }
  }, [projectId, baselineId]);

  useEffect(() => {
    void loadCoverage();
  }, [loadCoverage]);

  return { coverage: entries, loadCoverage };
}
