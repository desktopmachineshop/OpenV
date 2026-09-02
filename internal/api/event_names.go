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
	// "work item", ...) and EntityName the thing itself. Both are empty when
	// the name cannot be resolved (a deleted artifact, a missing service).
	EntityKind string `json:"entity_kind,omitempty"`
	EntityName string `json:"entity_name,omitempty"`
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
		workItems: map[string]string{},
		baselines: map[string]string{},
	}
	out := make([]eventView, 0, len(list))
	for _, e := range list {
		v := eventView{Event: e}
		v.ActorKind, v.ActorID, v.ActorName = n.actor(e.Actor)
		v.EntityKind, v.EntityName = n.entity(e)
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
		if name == "" {
			// artifact.deleted carries no payload; the row may still be
			// readable as a superseded version.
			name = n.artifactLabel(e.EntityID)
		}
	case "link":
		kind = payloadString(p, "link_type")
		if kind == "" {
			kind = "link"
		}
		from := n.artifactLabel(payloadString(p, "from_id"))
		to := n.artifactLabel(payloadString(p, "to_id"))
		if from != "" && to != "" {
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
	if id == "" || n.h.userService == nil {
		return ""
	}
	if name, ok := n.users[id]; ok {
		return name
	}
	name := ""
	if u, err := n.h.userService.GetByID(id); err == nil && u != nil {
		name = strings.TrimSpace(u.Name)
		if name == "" {
			name = u.Email
		}
	}
	n.users[id] = name
	return name
}

// artifactLabel renders an artifact as "REQ-12 Positioner shall …", falling
// back to whichever half is known.
func (n *eventNamer) artifactLabel(id string) string {
	if id == "" || n.h.artifactService == nil {
		return ""
	}
	if label, ok := n.artifacts[id]; ok {
		return label
	}
	label := ""
	if a, err := n.h.artifactService.GetArtifact(id); err == nil && a != nil {
		label = strings.TrimSpace(a.Title)
		switch {
		case a.Ref == "":
		case label == "":
			label = a.Ref
		default:
			label = a.Ref + " " + label
		}
	}
	n.artifacts[id] = label
	return label
}

func (n *eventNamer) baselineName(id string) string {
	if id == "" || n.h.baselineService == nil {
		return ""
	}
	if name, ok := n.baselines[id]; ok {
		return name
	}
	name := ""
	if b, err := n.h.baselineService.GetBaseline(id); err == nil && b != nil {
		name = b.Name
	}
	n.baselines[id] = name
	return name
}

func (n *eventNamer) workItemTitle(id string) string {
	if id == "" || n.h.workItemService == nil {
		return ""
	}
	if title, ok := n.workItems[id]; ok {
		return title
	}
	title := ""
	if item, err := n.h.workItemService.Get(id); err == nil && item != nil {
		title = item.Title
	}
	n.workItems[id] = title
	return title
}

// runAgentName names the agent behind a run ID.
func (n *eventNamer) runAgentName(runID string) string {
	if runID == "" || n.h.runService == nil {
		return ""
	}
	if name, ok := n.runs[runID]; ok {
		return name
	}
	name := ""
	if run, err := n.h.runService.Get(runID); err == nil && run != nil {
		name = n.agentName(run.AgentID)
	}
	n.runs[runID] = name
	return name
}

func (n *eventNamer) agentName(agentID string) string {
	if agentID == "" || n.h.agentService == nil {
		return ""
	}
	if name, ok := n.agents[agentID]; ok {
		return name
	}
	name := ""
	if a, err := n.h.agentService.Get(agentID); err == nil && a != nil {
		name = a.Name
		if name == "" {
			name = a.Slug
		}
	}
	n.agents[agentID] = name
	return name
}
