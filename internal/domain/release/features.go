package release

import "sort"

// Feature gating by channel (REQ-137).
//
// The shared service runs one build. A new feature ships in a release and
// is declared here with that release's version; a workspace on the nightly
// channel sees it at once, and a workspace on the stable channel sees it
// once the stable release it has turned on is that release or a later one.
// Maintenance updates and bug fixes are never gated: only what is filed
// under New features waits for a stable release.
//
// A feature stays in the registry until every stable release that could
// lack it has left the support window, then its gate is removed and the
// code path becomes unconditional.

// Feature is one gated change.
type Feature struct {
	// Key names the feature to code and clients ("crew-graph-v2").
	Key string
	// ShippedIn is the version of the release that first carried it.
	ShippedIn string
	// Summary is one line for the features endpoint and the What's new page.
	Summary string
}

// Registry lists every gated feature. Keep it sorted by ShippedIn so the
// oldest gates are the easiest to find and retire.
var Registry = []Feature{
	{Key: FeatureFlowDown, ShippedIn: "0.3.0", Summary: "Parent projects, requirements that refine a parent project's, and verification rolled up from child projects"},
	{Key: FeatureOwners, ShippedIn: "0.3.0", Summary: "Artifact owners, reference parties and owner-filtered downloads"},
	{Key: FeatureShareLinks, ShippedIn: "0.4.0", Summary: "Share links: a public read-only view of a project, or reviewer access, from one link; the reviewer role"},
	{Key: FeatureAssistantEdits, ShippedIn: "0.5.0", Summary: "The V&V Assistant adds any kind of artifact, edits one and moves one from the notes panel"},
	{Key: FeatureDefaultWorkspace, ShippedIn: "0.6.0", Summary: "A member chooses the workspace OpenV opens in when they sign in"},
	{Key: FeatureFigureTitles, ShippedIn: "0.6.0", Summary: "Figures carry a title of their own: rename one and the change is a tracked figure version"},
	// Password reset happens before there is a session, so there is no
	// workspace whose channel could gate it: the key is registered so the
	// features endpoint and What's new list it, and the flow itself is
	// unconditional.
	{Key: FeaturePasswordReset, ShippedIn: "0.6.0", Summary: "Forgot your password? An emailed reset link from the sign-in page, or one a platform admin makes for you"},
	{Key: FeatureAntigravity, ShippedIn: "0.7.0", Summary: "Antigravity CLI as an agent provider: Google's successor to the Gemini CLI, run from a workspace Gemini API key"},
	{Key: FeatureTodoList, ShippedIn: "0.8.0", Summary: "To-dos: raise one from a note that names someone, and see who owes what on a page of its own under Plan"},
	{Key: FeatureFigureRevert, ShippedIn: "0.8.0", Summary: "Restore an older version of a figure from its history, recorded as a new version so nothing is lost"},
	{Key: FeatureAttachmentFormats, ShippedIn: "0.9.0", Summary: "Attach PDFs and CAD files to an artifact, with a PDF reader and a 3D preview for STL"},
	{Key: FeatureFigureCitations, ShippedIn: "0.9.0", Summary: "Cite a figure on any artifact in the project with \"##\""},
	{Key: FeatureSearchByRef, ShippedIn: "0.10.0", Summary: "Search finds an artifact by its ref: type REQ-30 and get REQ-30, and every result shows the ref beside its title"},
	{Key: FeatureNoteTagging, ShippedIn: "0.11.0", Summary: "Tag people and references inside a note: @name, @@name to raise a to-do, and # / ## to cite a figure or an artifact"},
	{Key: FeatureProjectReviewRound, ShippedIn: "0.12.0", Summary: "Send a whole project for review in one action, and run it again later to pick up only what changed"},
}

// Feature keys the code gates on.
const (
	FeatureFlowDown       = "flow-down"
	FeatureOwners         = "artifact-owners"
	FeatureShareLinks     = "share-links"
	FeatureAssistantEdits = "assistant-project-edits"
	// FeatureDefaultWorkspace is the per-member choice of the workspace a
	// sign-in lands in, instead of always the personal one.
	FeatureDefaultWorkspace = "default-workspace"
	FeatureFigureTitles     = "figure-titles"
	FeaturePasswordReset    = "password-reset"
	// FeatureAntigravity is the antigravity-cli agent provider. Google moved
	// the consumer Gemini tiers onto this CLI on 18 June 2026.
	FeatureAntigravity = "antigravity-cli"
	// FeatureTodoList is the To-dos page and the to-do a note raises for
	// the person it names.
	FeatureTodoList = "todo-list"
	// FeatureFigureRevert is restoring an older version of a figure.
	FeatureFigureRevert = "figure-revert"
	// FeatureAttachmentFormats gates what may be ATTACHED, never what may be
	// read: a workspace that has not received it yet must still be able to
	// open a PDF a colleague on the nightly channel attached, or the gate
	// would turn a released feature into missing files.
	FeatureAttachmentFormats = "attachment-formats"
	// FeatureFigureCitations gates the "##" menu, and likewise only the
	// writing of one. A "##" citation already in a description resolves for
	// everybody, because a gate that broke existing prose would be worse than
	// no gate at all.
	FeatureFigureCitations = "figure-citations"
	// FeatureSearchByRef gates searching by a stable ref: matching refs,
	// ranking an exact one first, and showing each result's ref. A workspace
	// that has not received it searches titles and bodies as before, so the
	// gate narrows what search finds rather than hiding anything a colleague
	// has written.
	FeatureSearchByRef = "search-by-ref"
	// FeatureNoteTagging gates the tagging menus in the notes composer and
	// the "@@" to-do shortcut. Reading is never gated: a note already written
	// with "@@dana" or "##REQ-12" renders its tags for everybody, because a
	// gate that broke prose people had already written would be worse than no
	// gate at all.
	FeatureNoteTagging = "note-tagging"
	// FeatureProjectReviewRound is the project-wide review round: the Start
	// review action on the review queue and the endpoint behind it. It gates
	// only STARTING a round — the statuses a round leaves behind are ordinary
	// review states every workspace already reads and acts on, so a gate on
	// anything more would hide a colleague's work rather than a feature.
	FeatureProjectReviewRound = "project-review-round"
)

// Enabled reports whether a feature is on for a workspace on the given
// channel whose turned-on stable release is stableRelease ("" when the
// workspace has no stable release yet).
func Enabled(f Feature, channel, stableRelease string) bool {
	if channel != "stable" {
		return true
	}
	if stableRelease == "" {
		return false
	}
	return AtOrBefore(f.ShippedIn, stableRelease)
}

// FeaturesFor resolves every registered feature for a workspace: its
// channel and the stable release it has turned on. A version the notes do
// not know as a stable release leaves every gate closed on the stable
// channel, so a mistyped or withdrawn release never opens anything.
func FeaturesFor(svc Service, channel, stableVersion string) map[string]bool {
	stable := stableVersion
	if svc != nil && stableVersion != "" && svc.Stable(stableVersion) == nil {
		stable = ""
	}
	out := make(map[string]bool, len(Registry))
	for _, f := range Registry {
		out[f.Key] = Enabled(f, channel, stable)
	}
	return out
}

// Keys lists the registered feature keys, sorted.
func Keys() []string {
	keys := make([]string, 0, len(Registry))
	for _, f := range Registry {
		keys = append(keys, f.Key)
	}
	sort.Strings(keys)
	return keys
}
