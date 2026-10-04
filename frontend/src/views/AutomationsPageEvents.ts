// The event types a triggered automation can listen to: every domain event
// type the backend declares (internal/domain/events/events.go), in its
// order. arch/vocabParity.test.ts holds this list to contracts/vocab.json,
// which Go writes, so an event type added there fails that test until it is
// offered here too.
export const EVENT_TYPES = [
  'artifact.created',
  'artifact.updated',
  'artifact.deleted',
  'artifact.status_changed',
  'artifact.restored',
  'link.created',
  'link.updated',
  'link.deleted',
  'baseline.captured',
  'baseline.deleted',
  'project.review_round_started',
  'chatter.created',
  'testrun.recorded',
  'workitem.created',
  'workitem.moved',
  'workitem.updated',
  'agentrun.finished',
  'agentrun.successors_skipped',
  'proposal.created',
  'org.member_added',
  'org.member_role_changed',
  'org.member_removed',
  'org.invitation_sent',
  'org.invitation_accepted',
  'project.member_added',
  'project.member_role_changed',
  'project.member_removed',
];
