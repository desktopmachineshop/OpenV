import type { FeatureKey } from '../generated/contract';
import { useAppStore } from '../state/store';

// useFeature answers whether a gated feature is on for the current member in
// the active workspace (REQ-137). False until the gates have loaded, so a
// stable-channel workspace never sees a feature flash on and then off. The
// key is one of release.Registry's (the generated contract's FeatureKey), so
// a gate on a key Go does not register fails tsc.
export const useFeature = (key: FeatureKey): boolean => {
  const features = useAppStore((s) => s.features);
  return Boolean(features?.features[key]);
};
