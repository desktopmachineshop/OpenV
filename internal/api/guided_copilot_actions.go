package api

// projectActionRules is the rest of a project's API as cards the V&V
// Assistant may offer (FeatureAssistantFullScope), beside the artifact, edit
// and move shapes of projectChangeRules. Every shape here is applied in the
// member's browser by the same calls the app's own pages make
// (frontend/src/components/wizard/projectActions.ts), so a card can do no
// more than the member clicking it could, and nothing happens until they do.
// The assistant still writes nothing itself.
//
// The shapes and their field names are a contract with that parser: a field
// renamed here and not there makes a card that refuses with a reason.
func projectActionRules(projectID string) string {
	return `

Every other change a person can make to the project has a card too, in the same openv-suggestion blocks. Use them whenever the user asks for one of these changes instead of telling them where to click:
- {"kind":"release","name":"MVP","description":"","target_date":"YYYY-MM-DD","status":"planned|in_progress|released"} — add a target release at the end of the project's release order. To change one that exists, add "release":"<its current name or id>" and give only the fields that change ("target_date":"" clears the date).
- {"kind":"target","refs":["REQ-12","REQ-14"],"release":"<name or id of the release they are introduced in>","until":"<name or id of the last release they are in; omit for every later one>"} — set the releases artifacts target. {"kind":"target","refs":[...],"clear":true} removes an artifact's own targeting so it inherits its parent's again. A release named here may be one a release card earlier in the same reply adds.
- {"kind":"link","from":"TC-3","to":"REQ-4","type":"verifies|satisfies|mitigates|decomposes-to|refines|derives-from|validates|impacts|relates-to"} — add a traceability link. Add "action":"remove" to delete the link between the two (of that type, when given), or "action":"confirm" to clear a suspect link's flag after checking it still holds.
- {"kind":"status","refs":["REQ-4"],"status":"draft|in_review|approved|superseded"} — move artifacts through review. The server refuses a transition the workflow does not allow and the card says why.
- {"kind":"archive","refs":["REQ-4"]} — archive artifacts: they leave the project and keep their history, and the user can reinstate one from its page (open it from the activity trail). Offer this only when the user asks to remove something.
- {"kind":"comment","ref":"REQ-4","message":"…"} — post a comment on an artifact, in the user's name, so write it as they would.
- {"kind":"todo","text":"…","assignee":"<member name or email; omit for nobody>","due":"YYYY-MM-DD","ref":"<the artifact it is about; optional>"} — raise a to-do in the project.
- {"kind":"task","title":"…","description":"…","refs":["REQ-4"],"assignee":"<member name or email>","due":"YYYY-MM-DD"} — add a task to the project's board, linked to the artifacts named.
- {"kind":"baseline","name":"…"} — capture a baseline of the project as it stands.
- {"kind":"review","types":["requirement"]} — start a review round: every draft of those types (omit types for everything but headings and descriptions) goes to in review.
- {"kind":"test_run","name":"…","results":[{"ref":"TC-3","status":"pass|fail|blocked|not-run","notes":"…"}],"complete":true} — record a test run with a result per test case, completed when "complete" is true.
Name artifacts by reference throughout. Read before you propose: list_releases for the release names, list_links_for_artifact for an artifact's links, get_release_progress for what each release holds (project ` + projectID + `). A card does nothing until the user applies it, so never say a change was made; say what the card will do. Project settings, members and access, uploads and evidence files stay with the user in the app.`
}
