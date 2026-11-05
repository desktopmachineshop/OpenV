import { dayLabel, describeEvent, eventTarget } from './activityEvents';
import type { DomainEvent } from '../api/client';

// The audit trail's words and targets, shared by the Homes and the Activity
// log (#718, #734).
const ev = (over: Partial<DomainEvent>): DomainEvent => ({
  id: 'e1',
  event_type: 'artifact.created',
  project_id: 'p1',
  entity_id: 'a1',
  actor: 'user:u1',
  actor_kind: 'user',
  actor_name: 'David',
  payload: {},
  created_at: '2026-11-04T23:10:00Z',
  ...over,
});

describe('eventTarget', () => {
  it('opens each kind of event on the thing it describes', () => {
    expect(eventTarget(ev({ event_type: 'artifact.status_changed' }))).toBe('/projects/p1/requirements?artifact=a1');
    expect(eventTarget(ev({ event_type: 'artifact.updated' }))).toBe('/projects/p1/requirements?artifact=a1');
    expect(eventTarget(ev({ event_type: 'link.created', entity_id: 'l1', payload: { from_id: 'tc1', to_id: 'r1', link_type: 'verifies' } }))).toBe('/projects/p1/requirements?artifact=tc1');
    expect(eventTarget(ev({ event_type: 'workitem.moved', entity_id: 'w1' }))).toBe('/projects/p1/board');
    expect(eventTarget(ev({ event_type: 'agentrun.finished', entity_id: 'run1' }))).toBe('/projects/p1/agent-runs?run=run1');
    expect(eventTarget(ev({ event_type: 'proposal.created', entity_id: 'pr1', payload: { run_id: 'run2' } }))).toBe('/projects/p1/agent-runs?run=run2');
    expect(eventTarget(ev({ event_type: 'testrun.recorded', entity_id: 'res1', payload: { run_id: 'tr1' } }))).toBe('/projects/p1/vv/runs/tr1');
    expect(eventTarget(ev({ event_type: 'baseline.captured', entity_id: 'b1' }))).toBe('/projects/p1/baselines/b1/compare');
    expect(eventTarget(ev({ event_type: 'chatter.created', entity_id: 'c1', payload: { artifact_id: 'a9' } }))).toBe('/projects/p1/requirements?artifact=a9&notes=1');
  });

  it('leads nowhere for a deleted artifact or an event without a project', () => {
    expect(eventTarget(ev({ event_type: 'artifact.deleted' }))).toBe('');
    expect(eventTarget(ev({ project_id: undefined }))).toBe('');
  });
});

describe('describeEvent', () => {
  it('says who approved what, in green, and where it leads', () => {
    const got = describeEvent(ev({ event_type: 'artifact.status_changed', entity_name: 'Phone suite', payload: { from: 'in_review', to: 'approved' } }));
    expect(got).toMatchObject({ who: 'David', what: 'approved', ref: 'Phone suite', tone: 'ok', to: '/projects/p1/requirements?artifact=a1' });
  });

  it('names a runner as the workspace runner', () => {
    expect(describeEvent(ev({ actor: 'worker:o1', actor_kind: 'worker', actor_name: undefined })).who).toBe('Workspace runner');
  });
});

describe('dayLabel', () => {
  const now = new Date(2026, 10, 5, 12, 0);
  it('heads the day as Today, Yesterday, or its date', () => {
    expect(dayLabel(new Date(2026, 10, 5, 8, 0).toISOString(), now)).toBe('Today');
    expect(dayLabel(new Date(2026, 10, 4, 23, 0).toISOString(), now)).toBe('Yesterday');
    expect(dayLabel(new Date(2026, 10, 3, 9, 0).toISOString(), now)).toMatch(/3/);
    expect(dayLabel(new Date(2025, 10, 3, 9, 0).toISOString(), now)).toMatch(/2025/);
  });
});
