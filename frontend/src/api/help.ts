// In-app help: the user manual's topics, served by OpenV from the OpenD docs
// site for the workspace's docs line and plan (docs/plans/opend-help.md).
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import { client } from './http';
import type { HelpResults, HelpTopic } from './types/help';

export const helpAPI = {
  // The topic for a context key (a page's, or a help link's), or for an app
  // path when no key is given; a key falls back to its parents.
  context: (orgId: string, ctx: { key?: string; path?: string }) =>
    client.get<HelpTopic>(`/api/v1/orgs/${orgId}/help/context`, { params: ctx }),
  // One topic by its page id, such as "topics/vv/test-runs".
  topic: (orgId: string, topicId: string) =>
    client.get<HelpTopic>(`/api/v1/orgs/${orgId}/help/topics/${topicId}`),
  search: (orgId: string, q: string) =>
    client.get<HelpResults>(`/api/v1/orgs/${orgId}/help/search`, { params: { q } }),
};
