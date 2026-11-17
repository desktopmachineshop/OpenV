// What the plan page does when a milestone is linked (milestone-links):
// changing the tasks it waits for or how its date is set from its panel,
// and making it wait for a task when a link is dropped on its diamond.
import { planAPI, type PlanMilestone, type PlanMilestoneInput, type ProjectPlan } from '../../api/client';
import { MILESTONE_LINKS_FEATURE } from '../../features';
import { useFeature } from '../../hooks/useFeature';

type Change = Pick<PlanMilestoneInput, 'waits_for' | 'date_mode'>;
type Run = (fn: () => Promise<unknown>, fallback: string) => Promise<boolean>;

/**
 * The panel's onLink and the chart's drop handler, both undefined while the
 * feature is off. A release's milestone takes only the tasks: its date is
 * the release's.
 */
export const useMilestoneLinks = (projectId: string, plan: ProjectPlan | null, run: Run) => {
  const on = useFeature(MILESTONE_LINKS_FEATURE);
  if (!on) return { onLink: undefined, onDrop: undefined };
  const onLink = (m: PlanMilestone, change: Change) =>
    run(
      () => (m.kind === 'release' ? planAPI.setReleaseWaitsFor(projectId, m.release_id!, change.waits_for || m.waits_for) : planAPI.updateMilestone(projectId, m.id, change)),
      'Failed to change what the milestone waits for',
    );
  const onDrop = (taskId: string, key: string) => {
    const m = plan?.milestones.find((x) => x.key === key);
    if (m && !m.waits_for.includes(taskId)) void onLink(m, { waits_for: [...m.waits_for, taskId] });
  };
  return { onLink, onDrop };
};
