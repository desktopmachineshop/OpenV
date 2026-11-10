package api

import (
	"strings"

	"github.com/openv/requirements-platform/internal/domain/events"
)

// The activity log stores actors and entities as raw IDs ("user:<uuid>", an
// artifact UUID), which is right for an audit trail but unreadable in the UI.
// eventView decorates a stored event with the names those IDs stand for; the
// raw actor/entity_id fields ride along unchanged so the log still shows the
// hashes next to the names.
type eventView struct {
	events.Event
	// ActorKind is "user", "agent", "worker" or "system"; ActorID is the ID
	// inside the actor string (the user ID, the agent run ID, ...).
	ActorKind string `json:"actor_kind,omitempty"`
	ActorID   string `json:"actor_id,omitempty"`
	ActorName string `json:"actor_name,omitempty"`
	// EntityKind names the kind of thing the event happened to ("requirement",
	// "work item", ...) and EntityName the thing itself. An archived artifact
	// (one deleted, which keeps every version) is named by its last version,
	// marked "(archived)". Both are empty when the
	// name cannot be resolved (an artifact no version has, a missing service).
	EntityKind string `json:"entity_kind,omitempty"`
	EntityName string `json:"entity_name,omitempty"`
	// ArchivedArtifacts lists the artifacts the event names (its entity, a
	// link's ends, a comment's or a result's artifact) that have since been
	// archived, so a client can tell them apart without parsing a name: it
	// opens such an artifact's history rather than its live page.
	ArchivedArtifacts []string `json:"archived_artifacts,omitempty"`
}

// eventNamer resolves the display names for one page of events. Lookups are
// memoized per page because the same user, agent or artifact recurs across
// many rows, and are only made when the event payload does not already carry
// the name (most write handlers stamp a title into the payload, so a typical
// page needs very few queries).
type eventNamer struct {
	h         *Handler
	users     map[string]string
	runs      map[string]string
	agents    map[string]string
	artifacts map[string]string
	// archived is which of the artifacts looked up are archived.
	archived  map[string]bool
	workItems map[string]string
	baselines map[string]string
}

// decorateEvents resolves names for a page of events. Call it only on events
// the caller is allowed to see: it dereferences the IDs they carry.
func (h *Handler) decorateEvents(list []events.Event) []eventView {
	n := &eventNamer{
		h:         h,
		users:     map[string]string{},
		runs:      map[string]string{},
		agents:    map[string]string{},
		artifacts: map[string]string{},
		archived:  map[string]bool{},
		workItems: map[string]string{},
		baselines: map[string]string{},
	}
	out := make([]eventView, 0, len(list))
	for _, e := range list {
		v := eventView{Event: e}
		v.ActorKind, v.ActorID, v.ActorName = n.actor(e.Actor)
		v.EntityKind, v.EntityName = n.entity(e)
		v.ArchivedArtifacts = n.archivedIn(e)
		out = append(out, v)
	}
	return out
}

// actor splits an actor string ("user:<id>", "agent:<run id>", "worker:<org>",
// "worker:<org>:user:<id>", "system") into its kind, the ID it carries and a
// display name.
func (n *eventNamer) actor(actor string) (kind, id, name string) {
	switch {
	case actor == "" || actor == events.ActorSystem:
		return "system", "", "System"
	case strings.HasPrefix(actor, "user:"):
		id = strings.TrimPrefix(actor, "user:")
		return "user", id, n.userName(id)
	case strings.HasPrefix(actor, "agent:"):
		// Agent actors carry the run ID; the name worth showing is the agent
		// that ran, so hop run -> agent.
		id = strings.TrimPrefix(actor, "agent:")
		return "agent", id, n.runAgentName(id)
	case strings.HasPrefix(actor, "worker:"):
		// A workspace runner key, optionally reserved to one user.
		rest := strings.TrimPrefix(actor, "worker:")
		if i := strings.Index(rest, ":user:"); i >= 0 {
			id = rest[i+len(":user:"):]
			return "worker", id, n.userName(id)
		}
		return "worker", rest, "Workspace runner"
	}
	return "", "", actor
}

// entity names the thing an event happened to, preferring the payload the
// publishing handler stamped in and falling back to a lookup by entity ID.
func (n *eventNamer) entity(e events.Event) (kind, name string) {
	p := e.Payload
	family := e.EventType
	if i := strings.Index(family, "."); i > 0 {
		family = family[:i]
	}
	switch family {
	case "artifact":
		kind = payloadString(p, "artifact_type")
		if kind == "" {
			kind = "artifact"
		}
		name = payloadString(p, "title")
		// artifact.deleted carries no payload, and an artifact archived since
		// is named by its last version, marked archived, rather than by the
		// title it had when the event happened.
		if label := n.artifactLabel(e.EntityID); name == "" || n.archived[e.EntityID] {
			name = label
		}
	case "link":
		kind = payloadString(p, "link_type")
		if kind == "" {
			kind = "link"
		}
		// Both ends, always: an end archived since is named by its last
		// version, marked archived, so the line says what the link joined.
		from := n.artifactLabel(payloadString(p, "from_id"))
		to := n.artifactLabel(payloadString(p, "to_id"))
		if from != "" || to != "" {
			if from == "" {
				from = "an unknown artifact"
			}
			if to == "" {
				to = "an unknown artifact"
			}
			name = from + " → " + to
		}
	case "baseline":
		kind = "baseline"
		name = payloadString(p, "name")
		if name == "" {
			name = n.baselineName(e.EntityID)
		}
	case "chatter":
		// The entity is the chatter entry; what identifies it for a reader is
		// the artifact it was posted on.
		kind = "comment"
		if k := payloadString(p, "kind"); k != "" {
			kind = k
		}
		name = n.artifactLabel(payloadString(p, "artifact_id"))
	case "testrun":
		kind = "test result"
		name = n.artifactLabel(payloadString(p, "test_case_id"))
	case "workitem":
		kind = "work item"
		name = payloadString(p, "title")
		if name == "" {
			name = n.workItemTitle(e.EntityID)
		}
	case "agentrun":
		kind = "agent run"
		name = n.agentName(payloadString(p, "agent_id"))
		if name == "" {
			name = n.runAgentName(e.EntityID)
		}
	case "proposal":
		kind = "proposal"
		name = payloadString(p, "op")
	default:
		kind = family
	}
	return kind, name
}

// payloadString reads a string field from an event payload, tolerating the
// absent key and any non-string value JSON round-tripping may have produced.
func payloadString(payload map[string]interface{}, key string) string {
	if payload == nil {
		return ""
	}
	s, _ := payload[key].(string)
	return s
}

func (n *eventNamer) userName(id string) string {
	if id == "" || n.h.UserService == nil {
		return ""
	}
	if name, ok := n.users[id]; ok {
		return name
	}
	name := ""
	if u, err := n.h.UserService.GetByID(id); err == nil && u != nil {
		name = strings.TrimSpace(u.Name)
		if name == "" {
			name = u.Email
		}
	}
	n.users[id] = name
	return name
}

// artifactLabel renders an artifact as "REQ-12 Positioner shall …", falling
// back to whichever half is known. An artifact archived since (the soft
// delete keeps every version, REQ-4) is rendered from its last version with
// " (archived)" after it, and recorded in n.archived.
func (n *eventNamer) artifactLabel(id string) string {
	if id == "" || n.h.ArtifactService == nil {
		return ""
	}
	if label, ok := n.artifacts[id]; ok {
		return label
	}
	a, err := n.h.ArtifactService.GetArtifact(id)
	if err != nil || a == nil {
		a = nil
		if versions, verr := n.h.ArtifactService.GetArtifactVersions(id); verr == nil {
			for _, v := range versions {
				if v != nil {
					a = v
					n.archived[id] = true
					break
				}
			}
		}
	}
	label := ""
	if a != nil {
		label = strings.TrimSpace(a.Title)
		switch {
		case a.Ref == "":
		case label == "":
			label = a.Ref
		default:
			label = a.Ref + " " + label
		}
		if n.archived[id] && label != "" {
			label += " (archived)"
		}
	}
	n.artifacts[id] = label
	return label
}

// archivedIn lists the artifacts an event names that have been archived, in
// the order the event names them. entity has already looked each one up.
func (n *eventNamer) archivedIn(e events.Event) []string {
	var ids []string
	add := func(id string) {
		if id != "" && n.archived[id] {
			for _, seen := range ids {
				if seen == id {
					return
				}
			}
			ids = append(ids, id)
		}
	}
	p := e.Payload
	switch {
	case strings.HasPrefix(e.EventType, "artifact."):
		add(e.EntityID)
	case strings.HasPrefix(e.EventType, "link."):
		add(payloadString(p, "from_id"))
		add(payloadString(p, "to_id"))
	case strings.HasPrefix(e.EventType, "chatter."):
		add(payloadString(p, "artifact_id"))
	case strings.HasPrefix(e.EventType, "testrun."):
		add(payloadString(p, "test_case_id"))
	}
	return ids
}

func (n *eventNamer) baselineName(id string) string {
	if id == "" || n.h.BaselineService == nil {
		return ""
	}
	if name, ok := n.baselines[id]; ok {
		return name
	}
	name := ""
	if b, err := n.h.BaselineService.GetBaseline(id); err == nil && b != nil {
		name = b.Name
	}
	n.baselines[id] = name
	return name
}

func (n *eventNamer) workItemTitle(id string) string {
	if id == "" || n.h.WorkItemService == nil {
		return ""
	}
	if title, ok := n.workItems[id]; ok {
		return title
	}
	title := ""
	if item, err := n.h.WorkItemService.Get(id); err == nil && item != nil {
		title = item.Title
	}
	n.workItems[id] = title
	return title
}

// runAgentName names the agent behind a run ID.
func (n *eventNamer) runAgentName(runID string) string {
	if runID == "" || n.h.RunService == nil {
		return ""
	}
	if name, ok := n.runs[runID]; ok {
		return name
	}
	name := ""
	if run, err := n.h.RunService.Get(runID); err == nil && run != nil {
		name = n.agentName(run.AgentID)
	}
	n.runs[runID] = name
	return name
}

func (n *eventNamer) agentName(agentID string) string {
	if agentID == "" || n.h.AgentService == nil {
		return ""
	}
	if name, ok := n.agents[agentID]; ok {
		return name
	}
	name := ""
	if a, err := n.h.AgentService.Get(agentID); err == nil && a != nil {
		name = a.Name
		if name == "" {
			name = a.Slug
		}
	}
	n.agents[agentID] = name
	return name
}
