import { COMPACT_LAYOUT_FEATURE, UI_REFRESH_FEATURE } from '../features';
import { useFeature } from './useFeature';

/**
 * Whether the refreshed interface is on for the workspace: the ui-refresh
 * gate, which is drawn on top of the compact layout and so waits for that
 * gate too. The shell and the pages it reshapes read it from here, so they
 * switch together.
 */
export const useUiRefresh = (): boolean => {
  const refresh = useFeature(UI_REFRESH_FEATURE);
  const compact = useFeature(COMPACT_LAYOUT_FEATURE);
  return refresh && compact;
};
