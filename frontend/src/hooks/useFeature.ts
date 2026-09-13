import { useAppStore } from '../state/store';

// useFeature answers whether a gated feature is on for the current member in
// the active workspace (REQ-137). False until the gates have loaded, so a
// stable-channel workspace never sees a feature flash on and then off.
export const useFeature = (key: string): boolean => {
  const features = useAppStore((s) => s.features);
  return Boolean(features?.features[key]);
};
