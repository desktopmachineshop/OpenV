// Type-only compatibility of the hand-written response types with the JSON
// the Go types write (refactor plan X5). ../generated/wire.ts states what
// encoding/json writes for eleven Go types; for each, the hand-written
// interface in ./types may name only keys the server writes, and must accept
// the value the server writes for each: Wire[K] is assignable to Hand[K],
// since the frontend reads what the server sends. COMPAT_EXCEPTIONS names,
// with its reason, every key where the two differ today, and an entry no
// longer needed fails as well, so the list only shrinks.
//
// There is no runtime code here and no module imports this file, so the
// production build never sees it; `npx tsc --noEmit -p .` checks it. A
// failure names the type, the key and what is wrong, e.g.
// "Artifact.title: not a key the server writes" after a json tag rename.
import type {
  WireAgent,
  WireArtifact,
  WireAttachment,
  WireBaseline,
  WireEvent,
  WireLink,
  WireNotification,
  WireOrg,
  WireProject,
  WireRun,
  WireUser,
} from '../generated/wire';
import type { User } from './types/account';
import type { AgentDef, AgentRun } from './types/agents';
import type { Artifact, Link } from './types/artifacts';
import type { Attachment } from './types/attachments';
import type { AppNotification, DomainEvent } from './types/notifications';
import type { Org } from './types/orgs';
import type { Baseline, Project } from './types/projects';

/** Each hand-written response type, by its name, with its generated twin. */
interface Twins {
  Artifact: [Artifact, WireArtifact];
  Link: [Link, WireLink];
  Project: [Project, WireProject];
  Org: [Org, WireOrg];
  User: [User, WireUser];
  AgentRun: [AgentRun, WireRun];
  AgentDef: [AgentDef, WireAgent];
  Attachment: [Attachment, WireAttachment];
  Baseline: [Baseline, WireBaseline];
  AppNotification: [AppNotification, WireNotification];
  DomainEvent: [DomainEvent, WireEvent];
}

/**
 * How a hand-written key may differ from its twin, each entry
 * `[excuse, reason]`:
 * - `narrowed`: the hand-written type accepts only part of what the Go type
 *   lets the server write (a union of the values a Go string holds, an
 *   object where a nil map would write null, a key always present where
 *   omitempty could drop it). It must still be assignable to the wire type,
 *   so a Go change to the key's name or type still fails.
 * - `client-side`: the Go type does not write the key; the reason says what
 *   does. It must stay absent from the wire type.
 */
type Excuse = 'narrowed' | 'client-side';

export type COMPAT_EXCEPTIONS = {
  Artifact: {
    status: ['narrowed', 'ArtifactStatus is the four values of the const block of artifacts.StatusDraft, which Go writes as a string'];
    attributes: ['narrowed', 'Go writes null for a nil map; NewArtifact makes it {} and the column defaults to {}'];
  };
  Link: {
    attributes: ['narrowed', 'Go writes null for a nil map, which NewLink keeps when a create request sends no attributes'];
  };
  Project: {
    agent_auth: ['narrowed', 'the two values of projects.AgentAuthUserAccount and AgentAuthAPIKey, which Go writes as a string'];
  };
  Org: {
    type: ['narrowed', 'the two values of orgs.TypePersonal and TypeCompany, which Go writes as a string'];
    role: ['narrowed', 'only the org list (ListOrgsForUser) writes role, on every row; other answers omit it (omitempty)'];
    release_channel: ['narrowed', 'the two values of orgs.ChannelNightly and ChannelStable, which Go writes as a string'];
  };
  AgentDef: {
    allowed_tools: ['narrowed', 'Go writes null for a nil slice; scanAgent reads a missing list as []'];
    write_mode: ['narrowed', 'the two values of agents.WriteModeProposal and WriteModeDirect, which Go writes as a string'];
  };
  Attachment: {
    kind: ['narrowed', 'AttachmentKind is the four values of attachments.Kind, which Attachment.MarshalJSON writes as a string'];
  };
  AppNotification: {
    entity_ref: ['narrowed', 'Go writes null for a nil map; notifications.New makes it {}'];
  };
  DomainEvent: {
    payload: ['narrowed', 'Go writes null for a nil map; events.New makes it {}'];
    actor_kind: ['client-side', 'eventView (internal/api) adds it beside the events.Event it embeds, for the activity log'];
    actor_id: ['client-side', 'eventView (internal/api) adds it beside the events.Event it embeds, for the activity log'];
    actor_name: ['client-side', 'eventView (internal/api) adds it beside the events.Event it embeds, for the activity log'];
    entity_kind: ['client-side', 'eventView (internal/api) adds it beside the events.Event it embeds, for the activity log'];
    entity_name: ['client-side', 'eventView (internal/api) adds it beside the events.Event it embeds, for the activity log'];
  };
};

type Name = keyof Twins;
type Hand<N extends Name> = Twins[N][0];
type Wire<N extends Name> = Twins[N][1];
type Entries<N extends Name> = N extends keyof COMPAT_EXCEPTIONS ? COMPAT_EXCEPTIONS[N] : Record<never, never>;
type Excused<N extends Name, E extends Excuse> = {
  [K in keyof Entries<N>]: Entries<N>[K] extends [E, string] ? K : never;
}[keyof Entries<N>];
type Shared<N extends Name> = keyof Hand<N> & keyof Wire<N>;
type Say<N extends Name, K, What extends string> = `${N}.${K & string}: ${What}`;

/** Hand-written keys the server does not write. */
type NotWritten<N extends Name> = Say<
  N,
  Exclude<keyof Hand<N>, keyof Wire<N> | Excused<N, 'client-side'>>,
  'not a key the server writes'
>;

/** Keys of both whose value from the server the hand-written type does not accept. */
type NotAccepted<N extends Name> = Say<
  N,
  Exclude<
    { [K in Shared<N>]: [Wire<N>[K]] extends [Hand<N>[K]] ? never : K }[Shared<N>],
    Excused<N, 'narrowed'>
  >,
  'the hand-written type does not accept what the server writes'
>;

/**
 * `narrowed` entries that do not narrow, or that nothing needs any more. A
 * hand-written key may be optional where Go always writes it (a reader that
 * copes with its absence reads it safely), so the narrowing is judged
 * without that undefined.
 */
type BadNarrowed<N extends Name> = {
  [K in Excused<N, 'narrowed'>]: K extends Shared<N>
    ? [Exclude<Hand<N>[K], undefined>] extends [Wire<N>[K]]
      ? [Wire<N>[K]] extends [Hand<N>[K]]
        ? Say<N, K, 'the types agree now: remove its COMPAT_EXCEPTIONS entry'>
        : never
      : Say<N, K, 'narrowed, but the hand-written type is not assignable to the wire type'>
    : Say<N, K, 'narrowed, but not a key of both types'>;
}[Excused<N, 'narrowed'>];

/** `client-side` entries for a key the server writes, or one the hand-written type dropped. */
type BadClientSide<N extends Name> = {
  [K in Excused<N, 'client-side'>]: K extends keyof Wire<N>
    ? Say<N, K, 'the server writes it now: remove its COMPAT_EXCEPTIONS entry'>
    : K extends keyof Hand<N>
      ? never
      : Say<N, K, 'client-side, but not a key of the hand-written type'>;
}[Excused<N, 'client-side'>];

/** Entries that are not `[excuse, reason]` with a reason. */
type Malformed<N extends Name> = {
  [K in keyof Entries<N>]: Entries<N>[K] extends [Excuse, infer Reason extends string]
    ? Reason extends ''
      ? Say<N, K, 'its COMPAT_EXCEPTIONS entry gives no reason'>
      : never
    : Say<N, K, 'its COMPAT_EXCEPTIONS entry is not [excuse, reason]'>;
}[keyof Entries<N>];

type Problems<N extends Name> = NotWritten<N> | NotAccepted<N> | BadNarrowed<N> | BadClientSide<N> | Malformed<N>;

/** Compiles only when T is never: a failure prints every problem as a string. */
type Assert<T extends never> = T;

export type WireCompatArtifact = Assert<Problems<'Artifact'>>;
export type WireCompatLink = Assert<Problems<'Link'>>;
export type WireCompatProject = Assert<Problems<'Project'>>;
export type WireCompatOrg = Assert<Problems<'Org'>>;
export type WireCompatUser = Assert<Problems<'User'>>;
export type WireCompatAgentRun = Assert<Problems<'AgentRun'>>;
export type WireCompatAgentDef = Assert<Problems<'AgentDef'>>;
export type WireCompatAttachment = Assert<Problems<'Attachment'>>;
export type WireCompatBaseline = Assert<Problems<'Baseline'>>;
export type WireCompatAppNotification = Assert<Problems<'AppNotification'>>;
export type WireCompatDomainEvent = Assert<Problems<'DomainEvent'>>;
/** Every COMPAT_EXCEPTIONS entry names a type of Twins. */
export type WireCompatEntries = Assert<Exclude<keyof COMPAT_EXCEPTIONS, Name>>;
