package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/billing"
	"github.com/openv/requirements-platform/internal/domain/attributes"
	"github.com/openv/requirements-platform/internal/domain/evidence"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/invitations"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/settings"
	"github.com/openv/requirements-platform/internal/domain/sharedproducts"
)

// Refactor plan X3a (docs/plans/codebase-refactor.md §6.7): characterization
// of the eight bespoke error writers that X3b moves, unchanged, to
// errmap.go. Each writer gets one table: a row per sentinel or error type it
// matches, bare and wrapped the way the domain packages wrap them ("%w:
// detail"), plus an error it does not know. A row states the response as it
// leaves the writer: the status, every header and the body byte for byte,
// trailing newline included. No writer is called with a nil error in
// production (every call site sits under `if err != nil`), so no table has a
// nil row. What a writer logs is not asserted.

// errmapCase is one row of a writer's table.
type errmapCase struct {
	name       string
	err        error
	wantStatus int
	wantHeader http.Header
	wantBody   string
}

// errmapWrap wraps err as the domain packages do (evidence.ErrInvalid,
// settings.ErrInvalidRules, …: "%w: detail"), so a row shows both that the
// writer matches through the chain and what a pass-through writer answers.
func errmapWrap(err error) error { return fmt.Errorf("%w: detail", err) }

// errmapUnknown is the error no writer knows: the shape of a database
// failure, quotes and all, so a writer that passes err.Error() through shows
// it.
var errmapUnknown = errors.New(`pq: relation "secret_table" does not exist`)

// errmapJSON is the header writeJSONError, writeJSONErrorCode and respondJSON
// set, and nothing else.
func errmapJSON() http.Header { return http.Header{"Content-Type": {"application/json"}} }

// errmapRetryAfter is a JSON header plus Retry-After, as writeRateLimited
// and the billing writer's 503s set them.
func errmapRetryAfter(seconds string) http.Header {
	return http.Header{"Content-Type": {"application/json"}, "Retry-After": {seconds}}
}

// errmapRequest is the request a writer that takes one is handed: a
// workspace route with its {id} variable, as billing's slog line reads it.
func errmapRequest() *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/orgs/org-1/billing/checkout", nil)
	return mux.SetURLVars(r, map[string]string{"id": "org-1"})
}

// errmapCheck compares what a writer wrote with the row. The recorder's
// status is 200 when nothing called WriteHeader, and its header map is nil
// when nothing touched it; both read as "nothing written".
func errmapCheck(t *testing.T, tc errmapCase, w *httptest.ResponseRecorder) {
	t.Helper()
	res := w.Result()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if res.StatusCode != tc.wantStatus {
		t.Errorf("status = %d, want %d", res.StatusCode, tc.wantStatus)
	}
	header := res.Header
	if header == nil {
		header = http.Header{}
	}
	want := tc.wantHeader
	if want == nil {
		want = http.Header{}
	}
	if !reflect.DeepEqual(header, want) {
		t.Errorf("header = %v, want %v", header, want)
	}
	if string(body) != tc.wantBody {
		t.Errorf("body = %q\n          want %q", body, tc.wantBody)
	}
}

// errmapSelfHosted sets the deployment mode a LimitError's remedy is chosen
// by, restoring the previous mode when the test ends.
func errmapSelfHosted(t *testing.T, selfHosted bool) {
	t.Helper()
	prev := orgs.SelfHosted()
	t.Cleanup(func() { orgs.SetSelfHosted(prev) })
	orgs.SetSelfHosted(selfHosted)
}

// TestErrmapAttributeDefinitionWriter pins writeAttributeDefinitionError
// (attribute_definition_handlers.go). Its default answers 500 with a fixed
// message; the unknown error's text reaches only the log (#379's bug 187).
func TestErrmapAttributeDefinitionWriter(t *testing.T) {
	cases := []errmapCase{
		{"ErrNotFound", attributes.ErrNotFound, 404, errmapJSON(),
			`{"error":"attribute definition not found"}` + "\n"},
		{"ErrNotFound wrapped", errmapWrap(attributes.ErrNotFound), 404, errmapJSON(),
			`{"error":"attribute definition not found"}` + "\n"},
		{"ErrInvalidScope", attributes.ErrInvalidScope, 400, errmapJSON(),
			`{"error":"a definition must be either org-wide (org_id) or project-scoped (project_id), not both or neither"}` + "\n"},
		{"ErrInvalidScope wrapped", errmapWrap(attributes.ErrInvalidScope), 400, errmapJSON(),
			`{"error":"a definition must be either org-wide (org_id) or project-scoped (project_id), not both or neither: detail"}` + "\n"},
		{"ErrKeyRequired", attributes.ErrKeyRequired, 400, errmapJSON(),
			`{"error":"attribute key is required"}` + "\n"},
		{"ErrKeyRequired wrapped", errmapWrap(attributes.ErrKeyRequired), 400, errmapJSON(),
			`{"error":"attribute key is required: detail"}` + "\n"},
		{"ErrInvalidKey", attributes.ErrInvalidKey, 400, errmapJSON(),
			`{"error":"attribute key must contain only lowercase letters, numbers, and underscores"}` + "\n"},
		{"ErrInvalidKey wrapped", errmapWrap(attributes.ErrInvalidKey), 400, errmapJSON(),
			`{"error":"attribute key must contain only lowercase letters, numbers, and underscores: detail"}` + "\n"},
		{"ErrInvalidType", attributes.ErrInvalidType, 400, errmapJSON(),
			`{"error":"data_type must be one of: text, number, date, enum, boolean"}` + "\n"},
		{"ErrInvalidType wrapped", errmapWrap(attributes.ErrInvalidType), 400, errmapJSON(),
			`{"error":"data_type must be one of: text, number, date, enum, boolean: detail"}` + "\n"},
		{"ErrEnumValues", attributes.ErrEnumValues, 400, errmapJSON(),
			`{"error":"an enum definition needs at least one enum value"}` + "\n"},
		{"ErrEnumValues wrapped", errmapWrap(attributes.ErrEnumValues), 400, errmapJSON(),
			`{"error":"an enum definition needs at least one enum value: detail"}` + "\n"},
		{"ErrInvalidTarget", attributes.ErrInvalidTarget, 400, errmapJSON(),
			`{"error":"applies_to_type must be a known artifact type or empty for all types"}` + "\n"},
		{"ErrInvalidTarget wrapped", errmapWrap(attributes.ErrInvalidTarget), 400, errmapJSON(),
			`{"error":"applies_to_type must be a known artifact type or empty for all types: detail"}` + "\n"},
		{"unknown error", errmapUnknown, 500, errmapJSON(),
			`{"error":"failed to save attribute definition"}` + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeAttributeDefinitionError(w, errmapRequest(), tc.err)
			errmapCheck(t, tc, w)
			if strings.Contains(w.Body.String(), "secret_table") {
				t.Errorf("body %q carries the unknown error's text", w.Body.String())
			}
		})
	}
}

// TestErrmapBillingWriter pins writeBillingError (billing_handlers.go).
// Anything it does not name is the provider not answering: 503 with
// Retry-After, a fixed message and billing_upstream. Prices not confirmed
// yet answer the same, with their own message (#379's bug 189).
func TestErrmapBillingWriter(t *testing.T) {
	const upstream = `{"error":"the billing provider did not answer; the workspace was left as it was","code":"billing_upstream"}` + "\n"
	const unconfirmed = `{"error":"prices have not been confirmed with the billing provider yet; try again shortly","code":"billing_upstream"}` + "\n"
	cases := []errmapCase{
		{"orgs.ErrNotFound", orgs.ErrNotFound, 404, errmapJSON(),
			`{"error":"workspace not found"}` + "\n"},
		{"orgs.ErrNotFound wrapped", errmapWrap(orgs.ErrNotFound), 404, errmapJSON(),
			`{"error":"workspace not found"}` + "\n"},
		{"ErrCheckoutNotFound", billing.ErrCheckoutNotFound, 404, errmapJSON(),
			`{"error":"checkout not found"}` + "\n"},
		{"ErrCheckoutNotFound wrapped", errmapWrap(billing.ErrCheckoutNotFound), 404, errmapJSON(),
			`{"error":"checkout not found: detail"}` + "\n"},
		{"ErrNotConfigured", billing.ErrNotConfigured, 404, errmapJSON(),
			`{"error":"billing is not available on this deployment","code":"billing_unavailable"}` + "\n"},
		{"ErrNotConfigured wrapped", errmapWrap(billing.ErrNotConfigured), 404, errmapJSON(),
			`{"error":"billing is not available on this deployment: detail","code":"billing_unavailable"}` + "\n"},
		{"ErrUnknownPlan", billing.ErrUnknownPlan, 400, errmapJSON(),
			`{"error":"that plan and interval are not for sale","code":"unknown_plan"}` + "\n"},
		{"ErrUnknownPlan wrapped", errmapWrap(billing.ErrUnknownPlan), 400, errmapJSON(),
			`{"error":"that plan and interval are not for sale: detail","code":"unknown_plan"}` + "\n"},
		{"ErrUnknownCurrency", billing.ErrUnknownCurrency, 400, errmapJSON(),
			`{"error":"that currency is not offered for this plan","code":"unknown_plan"}` + "\n"},
		{"ErrUnknownCurrency wrapped", errmapWrap(billing.ErrUnknownCurrency), 400, errmapJSON(),
			`{"error":"that currency is not offered for this plan: detail","code":"unknown_plan"}` + "\n"},
		{"ErrCurrencyLocked", billing.ErrCurrencyLocked, 400, errmapJSON(),
			`{"error":"this workspace already pays in another currency","code":"unknown_plan"}` + "\n"},
		{"ErrCurrencyLocked wrapped", errmapWrap(billing.ErrCurrencyLocked), 400, errmapJSON(),
			`{"error":"this workspace already pays in another currency: detail","code":"unknown_plan"}` + "\n"},
		{"ErrPersonalWorkspace", billing.ErrPersonalWorkspace, 400, errmapJSON(),
			`{"error":"a personal workspace is one person; Business is for a shared workspace","code":"unknown_plan"}` + "\n"},
		{"ErrPersonalWorkspace wrapped", errmapWrap(billing.ErrPersonalWorkspace), 400, errmapJSON(),
			`{"error":"a personal workspace is one person; Business is for a shared workspace: detail","code":"unknown_plan"}` + "\n"},
		{"ErrAlreadySubscribed", billing.ErrAlreadySubscribed, 409, errmapJSON(),
			`{"error":"this workspace already has a live subscription","code":"already_subscribed"}` + "\n"},
		{"ErrAlreadySubscribed wrapped", errmapWrap(billing.ErrAlreadySubscribed), 409, errmapJSON(),
			`{"error":"this workspace already has a live subscription: detail","code":"already_subscribed"}` + "\n"},
		{"ErrNoSubscription", billing.ErrNoSubscription, 409, errmapJSON(),
			`{"error":"this workspace has no live subscription","code":"no_subscription"}` + "\n"},
		{"ErrNoSubscription wrapped", errmapWrap(billing.ErrNoSubscription), 409, errmapJSON(),
			`{"error":"this workspace has no live subscription: detail","code":"no_subscription"}` + "\n"},
		{"ErrGrantedPlan", billing.ErrGrantedPlan, 409, errmapJSON(),
			`{"error":"this workspace is on a plan a platform admin granted; there is nothing to buy","code":"granted_plan"}` + "\n"},
		{"ErrGrantedPlan wrapped", errmapWrap(billing.ErrGrantedPlan), 409, errmapJSON(),
			`{"error":"this workspace is on a plan a platform admin granted; there is nothing to buy: detail","code":"granted_plan"}` + "\n"},
		{"ErrNoCustomer", billing.ErrNoCustomer, 409, errmapJSON(),
			`{"error":"this workspace has no billing customer yet","code":"no_customer"}` + "\n"},
		{"ErrNoCustomer wrapped", errmapWrap(billing.ErrNoCustomer), 409, errmapJSON(),
			`{"error":"this workspace has no billing customer yet: detail","code":"no_customer"}` + "\n"},
		{"ErrSessionMismatch", billing.ErrSessionMismatch, 403, errmapJSON(),
			`{"error":"that checkout belongs to another workspace","code":"checkout_mismatch"}` + "\n"},
		{"ErrSessionMismatch wrapped", errmapWrap(billing.ErrSessionMismatch), 403, errmapJSON(),
			`{"error":"that checkout belongs to another workspace: detail","code":"checkout_mismatch"}` + "\n"},
		{"ErrPricesUnconfirmed", billing.ErrPricesUnconfirmed, 503, errmapRetryAfter("30"), unconfirmed},
		{"ErrPricesUnconfirmed wrapped", errmapWrap(billing.ErrPricesUnconfirmed), 503, errmapRetryAfter("30"), unconfirmed},
		{"unknown error", errmapUnknown, 503, errmapRetryAfter("30"), upstream},
	}
	h := newTestHandler(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.writeBillingError(w, errmapRequest(), tc.err)
			errmapCheck(t, tc, w)
		})
	}
}

// errmapEvidenceVerbs is every verb writeEvidenceError's callers pass
// (evidence_handlers.go), in source order.
var errmapEvidenceVerbs = []string{
	"failed to create the evidence bundle",
	"failed to update the evidence bundle",
	"failed to delete the evidence bundle",
	"failed to check the evidence storage limit",
	"failed to record the evidence file",
	"failed to load the evidence file",
	"failed to delete the evidence file",
	"failed to cite the evidence",
	"failed to remove the citation",
}

// TestErrmapEvidenceWriter pins writeEvidenceError (evidence_handlers.go).
// The verb is the public message of the default 500 and nothing else: every
// sentinel row answers the same under each verb.
func TestErrmapEvidenceWriter(t *testing.T) {
	sentinels := []errmapCase{
		{"ErrNotFound", evidence.ErrNotFound, 404, errmapJSON(),
			`{"error":"evidence bundle not found"}` + "\n"},
		{"ErrNotFound wrapped", errmapWrap(evidence.ErrNotFound), 404, errmapJSON(),
			`{"error":"evidence bundle not found"}` + "\n"},
		{"ErrFileNotFound", evidence.ErrFileNotFound, 404, errmapJSON(),
			`{"error":"evidence file not found"}` + "\n"},
		{"ErrFileNotFound wrapped", errmapWrap(evidence.ErrFileNotFound), 404, errmapJSON(),
			`{"error":"evidence file not found"}` + "\n"},
		{"ErrInvalid", evidence.ErrInvalid, 400, errmapJSON(),
			`{"error":"invalid evidence bundle"}` + "\n"},
		{"ErrInvalid wrapped", errmapWrap(evidence.ErrInvalid), 400, errmapJSON(),
			`{"error":"invalid evidence bundle: detail"}` + "\n"},
		{"ErrQuotaExceeded", evidence.ErrQuotaExceeded, 413, errmapJSON(),
			`{"error":"the workspace's evidence storage limit is full"}` + "\n"},
		{"ErrQuotaExceeded wrapped", errmapWrap(evidence.ErrQuotaExceeded), 413, errmapJSON(),
			`{"error":"the workspace's evidence storage limit is full: detail"}` + "\n"},
	}
	unknown := map[string]errmapCase{
		"failed to create the evidence bundle": {"unknown error", errmapUnknown, 500, errmapJSON(),
			`{"error":"failed to create the evidence bundle"}` + "\n"},
		"failed to update the evidence bundle": {"unknown error", errmapUnknown, 500, errmapJSON(),
			`{"error":"failed to update the evidence bundle"}` + "\n"},
		"failed to delete the evidence bundle": {"unknown error", errmapUnknown, 500, errmapJSON(),
			`{"error":"failed to delete the evidence bundle"}` + "\n"},
		"failed to check the evidence storage limit": {"unknown error", errmapUnknown, 500, errmapJSON(),
			`{"error":"failed to check the evidence storage limit"}` + "\n"},
		"failed to record the evidence file": {"unknown error", errmapUnknown, 500, errmapJSON(),
			`{"error":"failed to record the evidence file"}` + "\n"},
		"failed to load the evidence file": {"unknown error", errmapUnknown, 500, errmapJSON(),
			`{"error":"failed to load the evidence file"}` + "\n"},
		"failed to delete the evidence file": {"unknown error", errmapUnknown, 500, errmapJSON(),
			`{"error":"failed to delete the evidence file"}` + "\n"},
		"failed to cite the evidence": {"unknown error", errmapUnknown, 500, errmapJSON(),
			`{"error":"failed to cite the evidence"}` + "\n"},
		"failed to remove the citation": {"unknown error", errmapUnknown, 500, errmapJSON(),
			`{"error":"failed to remove the citation"}` + "\n"},
	}
	if len(unknown) != len(errmapEvidenceVerbs) {
		t.Fatalf("%d unknown-error rows for %d verbs", len(unknown), len(errmapEvidenceVerbs))
	}
	h := newTestHandler(t)
	for _, verb := range errmapEvidenceVerbs {
		u, ok := unknown[verb]
		if !ok {
			t.Fatalf("no unknown-error row for verb %q", verb)
		}
		for _, tc := range append(append([]errmapCase{}, sentinels...), u) {
			t.Run(verb+"/"+tc.name, func(t *testing.T) {
				w := httptest.NewRecorder()
				h.writeEvidenceError(w, errmapRequest(), verb, tc.err)
				errmapCheck(t, tc, w)
			})
		}
	}
}

// The limit refusals the invitation and limit writers are driven with.
// hostedCount/hostedFlag carry the hosted remedies, selfHosted* the
// self-hosted one; the rows set the mode they were written for.
const (
	errmapMembersHosted = `{"allowed":5,"code":"limit_reached",` +
		`"error":"Workspace members: this workspace allows 5 and already has 5. A workspace admin can raise this limit from the Billing tab in workspace settings, by moving the workspace to a plan that allows more.",` +
		`"label":"Workspace members","limit":"max_members",` +
		`"remedy":"A workspace admin can raise this limit from the Billing tab in workspace settings, by moving the workspace to a plan that allows more.",` +
		`"used":5}` + "\n"
	errmapMembersSelfHosted = `{"allowed":5,"code":"limit_reached",` +
		`"error":"Workspace members: this workspace allows 5 and already has 5. This deployment sets its own limits: raise max_members in OPENV_LIMITS to change it everywhere, or set it on this workspace alone to change it here.",` +
		`"label":"Workspace members","limit":"max_members",` +
		`"remedy":"This deployment sets its own limits: raise max_members in OPENV_LIMITS to change it everywhere, or set it on this workspace alone to change it here.",` +
		`"used":5}` + "\n"
)

// TestErrmapInvitationWriter pins writeInvitationError
// (invitation_handlers.go). A limit refusal is handed to writeLimitError;
// orgs.ErrLimitReached that is not an *orgs.LimitError, which it does not
// write, answers as an unknown error does (#379's bug 188).
func TestErrmapInvitationWriter(t *testing.T) {
	errmapSelfHosted(t, false)
	throttled := &errThrottled{message: "Too many invitations from this account; try again later.", retryAfter: 90 * time.Second}
	cases := []errmapCase{
		{"errThrottled", throttled, 429, errmapRetryAfter("90"),
			`{"error":"Too many invitations from this account; try again later."}` + "\n"},
		{"errThrottled wrapped", errmapWrap(throttled), 429, errmapRetryAfter("90"),
			`{"error":"Too many invitations from this account; try again later."}` + "\n"},
		{"errThrottled, 1.5s rounds up", &errThrottled{message: "slow down", retryAfter: 1500 * time.Millisecond}, 429, errmapRetryAfter("2"),
			`{"error":"slow down"}` + "\n"},
		{"errThrottled, 0s answers 1", &errThrottled{message: "slow down", retryAfter: 0}, 429, errmapRetryAfter("1"),
			`{"error":"slow down"}` + "\n"},
		{"*orgs.LimitError", orgs.NewLimitError(orgs.LimitMaxMembers, 5, 5), 403, errmapJSON(), errmapMembersHosted},
		{"*orgs.LimitError wrapped", errmapWrap(orgs.NewLimitError(orgs.LimitMaxMembers, 5, 5)), 403, errmapJSON(), errmapMembersHosted},
		{"orgs.ErrLimitReached bare answers as unknown", orgs.ErrLimitReached, 500, errmapJSON(),
			`{"error":"failed to bring the address into the workspace"}` + "\n"},
		{"orgs.ErrLimitReached wrapped answers as unknown", errmapWrap(orgs.ErrLimitReached), 500, errmapJSON(),
			`{"error":"failed to bring the address into the workspace"}` + "\n"},
		{"invitations.ErrInvalidEmail", invitations.ErrInvalidEmail, 400, errmapJSON(),
			`{"error":"a valid email is required"}` + "\n"},
		{"invitations.ErrInvalidEmail wrapped", errmapWrap(invitations.ErrInvalidEmail), 400, errmapJSON(),
			`{"error":"a valid email is required: detail"}` + "\n"},
		{"orgs.ErrInvalidRole", orgs.ErrInvalidRole, 400, errmapJSON(),
			`{"error":"invalid org role"}` + "\n"},
		{"orgs.ErrInvalidRole wrapped", errmapWrap(orgs.ErrInvalidRole), 400, errmapJSON(),
			`{"error":"invalid org role: detail"}` + "\n"},
		{"orgs.ErrPersonalOrgMembers", orgs.ErrPersonalOrgMembers, 400, errmapJSON(),
			`{"error":"personal workspaces cannot have additional members. A personal workspace is only ever you. Create a shared workspace to work with other people."}` + "\n"},
		{"orgs.ErrPersonalOrgMembers wrapped", errmapWrap(orgs.ErrPersonalOrgMembers), 400, errmapJSON(),
			`{"error":"personal workspaces cannot have additional members. A personal workspace is only ever you. Create a shared workspace to work with other people.: detail"}` + "\n"},
		{"errAlreadyOrgMember", errAlreadyOrgMember, 409, errmapJSON(),
			`{"error":"that address is already a member of this workspace"}` + "\n"},
		{"errAlreadyOrgMember wrapped", errmapWrap(errAlreadyOrgMember), 409, errmapJSON(),
			`{"error":"that address is already a member of this workspace: detail"}` + "\n"},
		{"orgs.ErrNotFound", orgs.ErrNotFound, 404, errmapJSON(),
			`{"error":"organization not found"}` + "\n"},
		{"orgs.ErrNotFound wrapped", errmapWrap(orgs.ErrNotFound), 404, errmapJSON(),
			`{"error":"organization not found: detail"}` + "\n"},
		{"invitations.ErrNotFound", invitations.ErrNotFound, 404, errmapJSON(),
			`{"error":"invitation not found"}` + "\n"},
		{"invitations.ErrNotFound wrapped", errmapWrap(invitations.ErrNotFound), 404, errmapJSON(),
			`{"error":"invitation not found: detail"}` + "\n"},
		{"errInvitationsUnavailable", errInvitationsUnavailable, 404, errmapJSON(),
			`{"error":"invitations are not configured on this server"}` + "\n"},
		{"errInvitationsUnavailable wrapped", errmapWrap(errInvitationsUnavailable), 404, errmapJSON(),
			`{"error":"invitations are not configured on this server: detail"}` + "\n"},
		{"unknown error", errmapUnknown, 500, errmapJSON(),
			`{"error":"failed to bring the address into the workspace"}` + "\n"},
	}
	h := newTestHandler(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.writeInvitationError(w, errmapRequest(), tc.err)
			errmapCheck(t, tc, w)
		})
	}
}

// TestErrmapLimitWriter pins writeLimitError (limits.go): an
// *orgs.LimitError anywhere in the chain answers 403 with the numbers and
// the remedy, keys in the encoder's sorted order, and true; anything else
// writes nothing and answers false. Each refusal's text and remedy follow
// the limit's unit, whether it is a flag, its detail clause and the
// deployment mode, so the rows cover each.
func TestErrmapLimitWriter(t *testing.T) {
	cases := []struct {
		errmapCase
		selfHosted bool
		wantOK     bool
	}{
		{errmapCase{"count limit", orgs.NewLimitError(orgs.LimitMaxMembers, 5, 5), 403, errmapJSON(), errmapMembersHosted}, false, true},
		{errmapCase{"count limit wrapped", errmapWrap(orgs.NewLimitError(orgs.LimitMaxMembers, 5, 5)), 403, errmapJSON(), errmapMembersHosted}, false, true},
		{errmapCase{"count limit, self-hosted remedy", orgs.NewLimitError(orgs.LimitMaxMembers, 5, 5), 403, errmapJSON(), errmapMembersSelfHosted}, true, true},
		{errmapCase{"MB limit", orgs.NewLimitError(orgs.LimitEvidenceStorageMB, 120, 100), 403, errmapJSON(),
			`{"allowed":100,"code":"limit_reached",` +
				`"error":"Test evidence storage: this workspace allows 100 MB and already has 120 MB. A workspace admin can raise this limit from the Billing tab in workspace settings, by moving the workspace to a plan that allows more.",` +
				`"label":"Test evidence storage","limit":"evidence_storage_mb",` +
				`"remedy":"A workspace admin can raise this limit from the Billing tab in workspace settings, by moving the workspace to a plan that allows more.",` +
				`"used":120}` + "\n"}, false, true},
		{errmapCase{"minutes limit with detail",
			orgs.NewLimitError(orgs.LimitHostedRunnerMinutesMonth, 600, 600).
				WithDetail("agents on your own machine through the Agent Connector are never counted"), 403, errmapJSON(),
			`{"allowed":600,"code":"limit_reached",` +
				`"error":"Cloud runner minutes this month: this workspace allows 600 minutes and already has 600 minutes (agents on your own machine through the Agent Connector are never counted). A workspace admin can raise this limit from the Billing tab in workspace settings, by moving the workspace to a plan that allows more.",` +
				`"label":"Cloud runner minutes this month","limit":"hosted_runner_minutes_month",` +
				`"remedy":"A workspace admin can raise this limit from the Billing tab in workspace settings, by moving the workspace to a plan that allows more.",` +
				`"used":600}` + "\n"}, false, true},
		{errmapCase{"CPUs limit", orgs.NewLimitError(orgs.LimitRunnerCPUs, 4, 2), 403, errmapJSON(),
			`{"allowed":2,"code":"limit_reached",` +
				`"error":"Hosted runner CPUs: this workspace allows 2 CPUs and already has 4 CPUs. A workspace admin can raise this limit from the Billing tab in workspace settings, by moving the workspace to a plan that allows more.",` +
				`"label":"Hosted runner CPUs","limit":"runner_cpus",` +
				`"remedy":"A workspace admin can raise this limit from the Billing tab in workspace settings, by moving the workspace to a plan that allows more.",` +
				`"used":4}` + "\n"}, false, true},
		{errmapCase{"uncatalogued key", orgs.NewLimitError("max_widgets", 3, 2), 403, errmapJSON(),
			`{"allowed":2,"code":"limit_reached",` +
				`"error":"max_widgets: this workspace allows 2 and already has 3. A workspace admin can raise this limit from the Billing tab in workspace settings, by moving the workspace to a plan that allows more.",` +
				`"label":"max_widgets","limit":"max_widgets",` +
				`"remedy":"A workspace admin can raise this limit from the Billing tab in workspace settings, by moving the workspace to a plan that allows more.",` +
				`"used":3}` + "\n"}, false, true},
		{errmapCase{"flag", orgs.NewFlagError(orgs.LimitTeams), 403, errmapJSON(),
			`{"allowed":0,"code":"limit_reached",` +
				`"error":"Teams and per-project access: not included in this workspace's plan. A workspace admin can add it from the Billing tab in workspace settings; the pricing page says which plan includes it.",` +
				`"label":"Teams and per-project access","limit":"teams",` +
				`"remedy":"A workspace admin can add it from the Billing tab in workspace settings; the pricing page says which plan includes it.",` +
				`"used":0}` + "\n"}, false, true},
		{errmapCase{"flag, self-hosted remedy", orgs.NewFlagError(orgs.LimitTeams), 403, errmapJSON(),
			`{"allowed":0,"code":"limit_reached",` +
				`"error":"Teams and per-project access: turned off on this deployment. This deployment sets its own limits: raise teams in OPENV_LIMITS to change it everywhere, or set it on this workspace alone to change it here.",` +
				`"label":"Teams and per-project access","limit":"teams",` +
				`"remedy":"This deployment sets its own limits: raise teams in OPENV_LIMITS to change it everywhere, or set it on this workspace alone to change it here.",` +
				`"used":0}` + "\n"}, true, true},
		{errmapCase{"orgs.ErrLimitReached bare", orgs.ErrLimitReached, 200, nil, ""}, false, false},
		{errmapCase{"orgs.ErrLimitReached wrapped", errmapWrap(orgs.ErrLimitReached), 200, nil, ""}, false, false},
		{errmapCase{"unknown error", errmapUnknown, 200, nil, ""}, false, false},
	}
	h := newTestHandler(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errmapSelfHosted(t, tc.selfHosted)
			w := httptest.NewRecorder()
			if ok := h.writeLimitError(w, tc.err); ok != tc.wantOK {
				t.Errorf("writeLimitError = %v, want %v", ok, tc.wantOK)
			}
			errmapCheck(t, tc.errmapCase, w)
		})
	}
}

// TestErrmapInviteWriter pins respondInviteError
// (public_interview_handlers.go): the participant-facing verdicts pass
// through as 404s, anything else is a 500 with a fixed message.
func TestErrmapInviteWriter(t *testing.T) {
	cases := []errmapCase{
		{"ErrInviteNotFound", interviews.ErrInviteNotFound, 404, errmapJSON(),
			`{"error":"invite not found"}` + "\n"},
		{"ErrInviteNotFound wrapped", errmapWrap(interviews.ErrInviteNotFound), 404, errmapJSON(),
			`{"error":"invite not found: detail"}` + "\n"},
		{"ErrInviteRevoked", interviews.ErrInviteRevoked, 404, errmapJSON(),
			`{"error":"invite has been revoked"}` + "\n"},
		{"ErrInviteRevoked wrapped", errmapWrap(interviews.ErrInviteRevoked), 404, errmapJSON(),
			`{"error":"invite has been revoked: detail"}` + "\n"},
		{"ErrInviteExpired", interviews.ErrInviteExpired, 404, errmapJSON(),
			`{"error":"invite has expired"}` + "\n"},
		{"ErrInviteExpired wrapped", errmapWrap(interviews.ErrInviteExpired), 404, errmapJSON(),
			`{"error":"invite has expired: detail"}` + "\n"},
		{"ErrInterviewClosed", interviews.ErrInterviewClosed, 404, errmapJSON(),
			`{"error":"interview is closed"}` + "\n"},
		{"ErrInterviewClosed wrapped", errmapWrap(interviews.ErrInterviewClosed), 404, errmapJSON(),
			`{"error":"interview is closed: detail"}` + "\n"},
		{"unknown error", errmapUnknown, 500, errmapJSON(),
			`{"error":"failed to resolve invite"}` + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			respondInviteError(w, errmapRequest(), tc.err)
			errmapCheck(t, tc, w)
		})
	}
}

// errmapRulesVerbs is every verb respondRulesError's callers pass
// (quality_rules_handlers.go): the read routes' and the write routes'.
var errmapRulesVerbs = []string{
	"failed to load quality rules",
	"failed to save quality rules",
}

// TestErrmapRulesWriter pins respondRulesError (quality_rules_handlers.go).
// The verb is the public message of the default 500 and nothing else: a
// read says "load", a write "save" (#379's bug 190), and every sentinel row
// answers the same under each verb.
func TestErrmapRulesWriter(t *testing.T) {
	sentinels := []errmapCase{
		{"ErrInvalidRules", settings.ErrInvalidRules, 400, errmapJSON(),
			`{"error":"invalid quality rules"}` + "\n"},
		{"ErrInvalidRules wrapped", errmapWrap(settings.ErrInvalidRules), 400, errmapJSON(),
			`{"error":"invalid quality rules: detail"}` + "\n"},
	}
	unknown := map[string]errmapCase{
		"failed to load quality rules": {"unknown error", errmapUnknown, 500, errmapJSON(),
			`{"error":"failed to load quality rules"}` + "\n"},
		"failed to save quality rules": {"unknown error", errmapUnknown, 500, errmapJSON(),
			`{"error":"failed to save quality rules"}` + "\n"},
	}
	if len(unknown) != len(errmapRulesVerbs) {
		t.Fatalf("%d unknown-error rows for %d verbs", len(unknown), len(errmapRulesVerbs))
	}
	h := newTestHandler(t)
	for _, verb := range errmapRulesVerbs {
		u, ok := unknown[verb]
		if !ok {
			t.Fatalf("no unknown-error row for verb %q", verb)
		}
		for _, tc := range append(append([]errmapCase{}, sentinels...), u) {
			t.Run(verb+"/"+tc.name, func(t *testing.T) {
				w := httptest.NewRecorder()
				h.respondRulesError(w, errmapRequest(), verb, tc.err)
				errmapCheck(t, tc, w)
			})
		}
	}
}

// TestErrmapSharedProductWriter pins writeSharedProductError
// (shared_product_handlers.go). The refusals pass the error's text through,
// so one row carries the characters the JSON encoder escapes.
func TestErrmapSharedProductWriter(t *testing.T) {
	cases := []errmapCase{
		{"ErrNotFound", sharedproducts.ErrNotFound, 404, errmapJSON(),
			`{"error":"shared product not found"}` + "\n"},
		{"ErrNotFound wrapped", errmapWrap(sharedproducts.ErrNotFound), 404, errmapJSON(),
			`{"error":"shared product not found: detail"}` + "\n"},
		{"ErrDuplicate", sharedproducts.ErrDuplicate, 409, errmapJSON(),
			`{"error":"a product with that name has already been shared"}` + "\n"},
		{"ErrDuplicate wrapped", errmapWrap(sharedproducts.ErrDuplicate), 409, errmapJSON(),
			`{"error":"a product with that name has already been shared: detail"}` + "\n"},
		{"ErrRateLimited", sharedproducts.ErrRateLimited, 429, errmapJSON(),
			`{"error":"this workspace has shared too many products today"}` + "\n"},
		{"ErrRateLimited wrapped", errmapWrap(sharedproducts.ErrRateLimited), 429, errmapJSON(),
			`{"error":"this workspace has shared too many products today: detail"}` + "\n"},
		{"ErrPoolFull", sharedproducts.ErrPoolFull, 429, errmapJSON(),
			`{"error":"the shared product pool is full"}` + "\n"},
		{"ErrPoolFull wrapped", errmapWrap(sharedproducts.ErrPoolFull), 429, errmapJSON(),
			`{"error":"the shared product pool is full: detail"}` + "\n"},
		{"ErrNotPublishable", sharedproducts.ErrNotPublishable, 403, errmapJSON(),
			`{"error":"only a signed-in person can share a product"}` + "\n"},
		{"ErrNotPublishable wrapped", errmapWrap(sharedproducts.ErrNotPublishable), 403, errmapJSON(),
			`{"error":"only a signed-in person can share a product: detail"}` + "\n"},
		{"ErrNotVotable", sharedproducts.ErrNotVotable, 403, errmapJSON(),
			`{"error":"only a signed-in person can vote for a shared product"}` + "\n"},
		{"ErrNotVotable wrapped", errmapWrap(sharedproducts.ErrNotVotable), 403, errmapJSON(),
			`{"error":"only a signed-in person can vote for a shared product: detail"}` + "\n"},
		{"ErrBadSort", sharedproducts.ErrBadSort, 400, errmapJSON(),
			`{"error":"sort must be one of: recent, top, top_week"}` + "\n"},
		{"ErrBadSort wrapped", errmapWrap(sharedproducts.ErrBadSort), 400, errmapJSON(),
			`{"error":"sort must be one of: recent, top, top_week: detail"}` + "\n"},
		{"ErrEmptyField", sharedproducts.ErrEmptyField, 400, errmapJSON(),
			`{"error":"every field is required: category, name, description, vision, problem, target_users"}` + "\n"},
		{"ErrEmptyField wrapped", errmapWrap(sharedproducts.ErrEmptyField), 400, errmapJSON(),
			`{"error":"every field is required: category, name, description, vision, problem, target_users: detail"}` + "\n"},
		{"ErrTooLong", sharedproducts.ErrTooLong, 400, errmapJSON(),
			`{"error":"a field is longer than the shared pool allows"}` + "\n"},
		{"ErrTooLong wrapped", errmapWrap(sharedproducts.ErrTooLong), 400, errmapJSON(),
			`{"error":"a field is longer than the shared pool allows: detail"}` + "\n"},
		{"ErrLinksNotAllowed", sharedproducts.ErrLinksNotAllowed, 400, errmapJSON(),
			`{"error":"shared products cannot contain links"}` + "\n"},
		{"ErrLinksNotAllowed wrapped", errmapWrap(sharedproducts.ErrLinksNotAllowed), 400, errmapJSON(),
			`{"error":"shared products cannot contain links: detail"}` + "\n"},
		{"ErrLinksNotAllowed wrapped, escaped characters",
			fmt.Errorf(`%w: field "name" holds <a href> & more`, sharedproducts.ErrLinksNotAllowed), 400, errmapJSON(),
			`{"error":"shared products cannot contain links: field \"name\" holds \u003ca href\u003e \u0026 more"}` + "\n"},
		{"ErrDisallowedText", sharedproducts.ErrDisallowedText, 400, errmapJSON(),
			`{"error":"shared products cannot contain agent instruction markup"}` + "\n"},
		{"ErrDisallowedText wrapped", errmapWrap(sharedproducts.ErrDisallowedText), 400, errmapJSON(),
			`{"error":"shared products cannot contain agent instruction markup: detail"}` + "\n"},
		{"unknown error", errmapUnknown, 500, errmapJSON(),
			`{"error":"failed to update shared products"}` + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeSharedProductError(w, errmapRequest(), tc.err)
			errmapCheck(t, tc, w)
		})
	}
}
