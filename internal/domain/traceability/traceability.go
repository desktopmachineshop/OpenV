// Package traceability is the link write service: what the paths that write
// traceability links share (the checks a link must pass, the writes, the
// links_snapshot refresh of the artifacts a link touches with its
// auto-version note, the link events, and the version note of an edit),
// each path asking for it under a Policy of its own.
//
// Four paths write links today, and they do not agree (refactor plan X11,
// quirks Q3 and Q4; internal/api/traceability_paths_test.go pins each). The
// API gives each its Policy (internal/api/link_traceability.go):
//
//	                        POST, DELETE /links     managed edits  appliers  guided drafts
//	OnInvalid               Refuse (400)            Skip (200)     Refuse    Skip (made)
//	RequireFlowDownFeature  POST only               no             no        no
//	TargetRole              EditorOrFlowDownViewer  Editor         NoRole    Editor
//	EmitEvents              yes                     no (Q3)        yes       no
//	Actor                   the caller              the caller     system    none
//
// The service does not converge them: each Policy says what its path does,
// and converging two is a product change. What a path does that no Policy
// field names is in the calls it makes: the managed edits write the edited
// artifact's snapshot into its update (SetLinksSnapshot, Q4) and refresh
// only the other ends, and the guided drafts write their links in
// internal/domain/guided, versioning nothing.
//
// The service reads and writes through the narrow ports below, which the
// API fills from its services at each call. A role or feature question goes
// to Access, so it is answered for the caller of the request; a path with
// no caller (the appliers) leaves it nil, and a Policy that asks it nothing
// needs none.
package traceability

import (
	"errors"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// Artifacts are the artifact reads and writes the service makes.
// artifacts.Service satisfies it.
type Artifacts interface {
	GetArtifact(id string) (*artifacts.Artifact, error)
	UpdateArtifact(id string, req artifacts.UpdateArtifactRequest) (*artifacts.Artifact, error)
}

// Links are the link reads and writes the service makes. links.Service
// satisfies it.
type Links interface {
	GetLink(id string) (*links.Link, error)
	CreateLink(link *links.Link) error
	DeleteLink(id string) error
	GetLinksTo(artifactID string) ([]*links.Link, error)
	GetLinksFrom(artifactID string) ([]*links.Link, error)
}

// Notes writes an artifact's feed notes. chatter.Service satisfies it.
type Notes interface {
	CreateEntry(entry *chatter.ChatterEntry) error
}

// Access answers, for the caller of a write, the questions a Policy asks.
type Access interface {
	// HasProjectRole reports whether the caller holds role, or better, on
	// the project.
	HasProjectRole(projectID, role string) bool
	// FlowDownEnabled reports whether the flow-down feature (REQ-145) is
	// on for the caller in the project's workspace.
	FlowDownEnabled(projectID string) bool
}

// Publisher publishes a link event as actor, stamped with its workspace as
// the caller's other events are.
type Publisher func(eventType, projectID, entityID, actor string, payload map[string]interface{})

// Service is the link write service over its ports. Artifacts, Links and
// Notes are required. Access may be nil when no Policy the caller passes
// asks a question of it; Publish may be nil, and then no event is
// published.
type Service struct {
	Artifacts Artifacts
	Links     Links
	Notes     Notes
	Access    Access
	Publish   Publisher

	// flowDown keeps Access.FlowDownEnabled's answers for the service's
	// one caller, so a Policy that asks the gate and then the role asks the
	// workspace once.
	flowDown map[string]bool
}

// OnInvalid is what a write that makes several links does with one it
// cannot make: a link the link rules refuse, an end no artifact has, a role
// the caller lacks, or a store that refuses it.
type OnInvalid int

const (
	// Refuse stops the write with the reason: POST /api/v1/links answers
	// 400, and an applier fails its proposal (apply_failed). A write of one
	// link always refuses.
	Refuse OnInvalid = iota
	// Skip leaves the link out, with a warning, and goes on: the managed
	// link edits still update the artifact (200), and the guided drafts are
	// still made.
	Skip
)

// TargetRole is the role a write asks of the caller on the project of a
// link's end other than the write's own (base) project.
type TargetRole int

const (
	// AsksNoRole asks nothing: the proposal appliers, whose run reached only
	// its own project when it proposed.
	AsksNoRole TargetRole = iota
	// AsksEditor asks editor rights on each end's project, whatever the
	// link's type: the managed edits and the guided drafts.
	AsksEditor
	// AsksEditorOrFlowDownViewer asks editor rights, except that a refines
	// link takes viewer rights on its target's project while the flow-down
	// feature is on, so a supplier refines what it can only read (REQ-145):
	// POST and DELETE /api/v1/links.
	AsksEditorOrFlowDownViewer
)

// Policy is what one link-write path asks of the service.
type Policy struct {
	// OnInvalid is what a write of several links does with one it cannot
	// make.
	OnInvalid OnInvalid
	// RequireFlowDownFeature refuses a refines link while the flow-down
	// feature is closed to the caller (CheckFlowDown).
	RequireFlowDownFeature bool
	// TargetRole is the role asked on the project of an end outside the
	// base project.
	TargetRole TargetRole
	// EmitEvents publishes link.created and link.deleted (PublishLinkEvent).
	EmitEvents bool
	// Actor is who the events name: the caller, or the system.
	Actor string
}

// End is one end of a link as the checks see it: its artifact's type and
// project. The zero End is an end the caller has not resolved, a pending
// proposal's ref (issue #235), which no check asks about.
type End struct {
	Type      string
	ProjectID string
}

// EndOf is the End of an artifact, or the zero End for nil.
func EndOf(a *artifacts.Artifact) End {
	if a == nil {
		return End{}
	}
	return End{Type: a.Type, ProjectID: a.ProjectID}
}

// Known reports whether the end was resolved.
func (e End) Known() bool { return e != End{} }

// ErrFlowDownClosed refuses a refines link while the flow-down feature is
// closed to the caller.
var ErrFlowDownClosed = errors.New("the flow-down feature is closed to this workspace")

// AccessError refuses a link for want of the role a Policy asks on the
// project of one of its ends.
type AccessError struct {
	End  string // "source" or "target"
	Role string
}

func (e *AccessError) Error() string {
	return "no " + e.Role + " access to the " + e.End + "'s project"
}

// ManagedLinkChanges is what managed link edits did: the links they made
// and removed, which the edited artifact's version note lists, and the
// other artifacts those links touch, which are auto-versioned. An add or a
// removal that was skipped is in none of them, so the note names no
// artifact the caller was refused (#379 bug 196).
type ManagedLinkChanges struct {
	Added, Removed []*links.Link
	Affected       []string
}
