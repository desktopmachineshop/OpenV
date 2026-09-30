package events

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The domain event vocabulary (refactor plan step S6, invariant I10), as a
// literal list. The strings are stored data: every row of the events table
// carries one, GET /api/v1/events filters on them, automations match them,
// and the notifier, the orchestration hooks, the budget monitor and the
// trigger matcher switch on them. A renamed or dropped constant stops this
// file compiling; a changed string, or a constant added without an entry
// here, fails TestEventTypesArePinned. Such a change is never a refactor:
// edit these lists in a PR that says what it changes.
//
// A pinned entry names a constant, holds the constant itself (so a renamed
// or removed one does not compile) and the literal its value must equal.
type pinnedConst struct{ name, got, want string }

// pinnedEventTypes lists the 27 event types in declaration order.
var pinnedEventTypes = []pinnedConst{
	{"ArtifactCreated", ArtifactCreated, "artifact.created"},
	{"ArtifactUpdated", ArtifactUpdated, "artifact.updated"},
	{"ArtifactDeleted", ArtifactDeleted, "artifact.deleted"},
	{"ArtifactStatusChanged", ArtifactStatusChanged, "artifact.status_changed"},
	{"ArtifactRestored", ArtifactRestored, "artifact.restored"},
	{"LinkCreated", LinkCreated, "link.created"},
	{"LinkUpdated", LinkUpdated, "link.updated"},
	{"LinkDeleted", LinkDeleted, "link.deleted"},
	{"BaselineCaptured", BaselineCaptured, "baseline.captured"},
	{"BaselineDeleted", BaselineDeleted, "baseline.deleted"},
	{"ReviewRoundStarted", ReviewRoundStarted, "project.review_round_started"},
	{"ChatterCreated", ChatterCreated, "chatter.created"},
	{"TestRunRecorded", TestRunRecorded, "testrun.recorded"},
	{"WorkItemCreated", WorkItemCreated, "workitem.created"},
	{"WorkItemMoved", WorkItemMoved, "workitem.moved"},
	{"WorkItemUpdated", WorkItemUpdated, "workitem.updated"},
	{"RunFinished", RunFinished, "agentrun.finished"},
	{"RunSuccessorsSkipped", RunSuccessorsSkipped, "agentrun.successors_skipped"},
	{"ProposalCreated", ProposalCreated, "proposal.created"},
	{"OrgMemberAdded", OrgMemberAdded, "org.member_added"},
	{"OrgMemberRoleChanged", OrgMemberRoleChanged, "org.member_role_changed"},
	{"OrgMemberRemoved", OrgMemberRemoved, "org.member_removed"},
	{"OrgInvitationSent", OrgInvitationSent, "org.invitation_sent"},
	{"OrgInvitationAccepted", OrgInvitationAccepted, "org.invitation_accepted"},
	{"ProjectMemberAdded", ProjectMemberAdded, "project.member_added"},
	{"ProjectMemberRoleChanged", ProjectMemberRoleChanged, "project.member_role_changed"},
	{"ProjectMemberRemoved", ProjectMemberRemoved, "project.member_removed"},
}

// pinnedActors lists the package's actor constants; user and agent actors
// are built elsewhere as "user:<id>" and "agent:<run id>".
var pinnedActors = []pinnedConst{
	{"ActorSystem", ActorSystem, "system"},
}

// pinnedEventTypeCount is the size of the vocabulary the refactor plan names.
const pinnedEventTypeCount = 27

// TestEventTypesArePinned checks each pinned constant's value against its
// literal, and that the package declares no constant the lists leave out.
func TestEventTypesArePinned(t *testing.T) {
	if len(pinnedEventTypes) != pinnedEventTypeCount {
		t.Fatalf("pinnedEventTypes has %d entries; the vocabulary has %d event types", len(pinnedEventTypes), pinnedEventTypeCount)
	}
	pinned := map[string]bool{}
	values := map[string]string{}
	var problems []string
	for _, list := range [][]pinnedConst{pinnedEventTypes, pinnedActors} {
		for _, c := range list {
			if pinned[c.name] {
				t.Fatalf("%s is pinned twice", c.name)
			}
			if other, dup := values[c.want]; dup {
				t.Fatalf("%s and %s are both pinned as %q", other, c.name, c.want)
			}
			pinned[c.name], values[c.want] = true, c.name
			if c.got != c.want {
				problems = append(problems, "changed value: "+c.name+" = "+quote(c.got)+", pinned "+quote(c.want))
			}
		}
	}
	for _, c := range pinnedEventTypes {
		if strings.HasPrefix(c.name, "Actor") {
			t.Fatalf("%s is pinned as an event type; actor constants belong in pinnedActors", c.name)
		}
	}
	declared := declaredConsts(t)
	for _, name := range declared {
		if !pinned[name] {
			problems = append(problems, "declared but not pinned: "+name)
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("the domain event vocabulary in internal/domain/events changed:\n  %s\n"+
			"Event type strings are stored in every event row and matched by subscribers and automations, "+
			"so a refactor never changes them. For a deliberate change, edit pinnedEventTypes or pinnedActors "+
			"in internal/domain/events/event_types_test.go in the same PR.",
			strings.Join(problems, "\n  "))
	}
}

// declaredConsts lists every package-level constant the package's non-test
// sources declare, in source order.
func declaredConsts(t *testing.T) []string {
	t.Helper()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	fset := token.NewFileSet()
	var consts []string
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				for _, n := range spec.(*ast.ValueSpec).Names {
					consts = append(consts, n.Name)
				}
			}
		}
	}
	if len(consts) == 0 {
		t.Fatal("found no constants in internal/domain/events: the scan has gone blind")
	}
	return consts
}

func quote(s string) string { return `"` + s + `"` }
