import { DOMAIN_EVENT_TYPES, DomainEventType } from '../generated/contract';

// The event types a triggered automation can listen to: every domain event
// type the backend declares (internal/domain/events/events.go), in its
// order, read from the generated contract (refactor plan X4b), so an event
// type Go adds is offered here once the contract is regenerated.
// arch/vocabParity.test.ts holds this list to contracts/vocab.json, which Go
// writes.
export const EVENT_TYPES: DomainEventType[] = [...DOMAIN_EVENT_TYPES];
