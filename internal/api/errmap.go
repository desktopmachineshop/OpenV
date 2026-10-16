package api

import (
	"errors"
	"net/http"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/attributes"
	"github.com/openv/requirements-platform/internal/domain/evidence"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/invitations"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/providers/usertokens"
	"github.com/openv/requirements-platform/internal/domain/settings"
	"github.com/openv/requirements-platform/internal/domain/sharedproducts"
	"github.com/openv/requirements-platform/internal/domain/workerkeys"
)

// writeAttributeDefinitionError maps domain validation errors to 400s. Any
// other failure is a 500 whose text reaches only the server log (#379's bug
// 187).
func writeAttributeDefinitionError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, attributes.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "attribute definition not found")
	case errors.Is(err, attributes.ErrInvalidScope),
		errors.Is(err, attributes.ErrKeyRequired),
		errors.Is(err, attributes.ErrInvalidKey),
		errors.Is(err, attributes.ErrInvalidType),
		errors.Is(err, attributes.ErrEnumValues),
		errors.Is(err, attributes.ErrInvalidTarget):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		respondInternal(w, r, "failed to save attribute definition", err)
	}
}

// writeEvidenceError maps the domain's errors onto status codes once, so every
// handler answers the same way.
func (h *Handler) writeEvidenceError(w http.ResponseWriter, r *http.Request, verb string, err error) {
	switch {
	case errors.Is(err, evidence.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "evidence bundle not found")
	case errors.Is(err, evidence.ErrFileNotFound):
		writeJSONError(w, http.StatusNotFound, "evidence file not found")
	case errors.Is(err, evidence.ErrInvalid):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, evidence.ErrQuotaExceeded):
		writeJSONError(w, http.StatusRequestEntityTooLarge, err.Error())
	default:
		respondInternal(w, r, verb, err)
	}
}

// writeRevokeKeyError answers a failed key revocation: a key no row has, or
// one that belongs to another workspace, is a 404 as for any id no row has
// (I3); any other failure keeps its 400.
func writeRevokeKeyError(w http.ResponseWriter, err error) {
	if errors.Is(err, workerkeys.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSONError(w, http.StatusBadRequest, err.Error())
}

// writeInvitationError maps the domain's user-facing failures onto statuses.
func (h *Handler) writeInvitationError(w http.ResponseWriter, r *http.Request, err error) {
	const failed = "failed to bring the address into the workspace"
	var throttled *errThrottled
	switch {
	case errors.As(err, &throttled):
		writeRateLimited(w, throttled.message, throttled.retryAfter)
	case errors.Is(err, orgs.ErrLimitReached):
		// A full workspace is not a bad request: the caller did nothing
		// wrong, the workspace is simply out of seats, and the refusal
		// carries the remedy. A sentinel with no *orgs.LimitError behind
		// it has no numbers to show, so it answers as the default does
		// (#379's bug 188).
		if !h.writeLimitError(w, err) {
			respondInternal(w, r, failed, err)
		}
	case errors.Is(err, invitations.ErrInvalidEmail), errors.Is(err, orgs.ErrInvalidRole):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, orgs.ErrPersonalOrgMembers):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, errAlreadyOrgMember):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, orgs.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, invitations.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errInvitationsUnavailable):
		writeJSONError(w, http.StatusNotFound, err.Error())
	default:
		respondInternal(w, r, failed, err)
	}
}

// launchErr is one row of a launch error table: a failed run launch whose
// error wraps sentinel (any error, when sentinel is nil) answers status with
// the error's own text or, when public is set, with public while the error
// reaches only the server log.
type launchErr struct {
	sentinel error
	status   int
	public   string
}

// The launch error tables (quirk Q9): one failed launch answers by route, so
// an over-budget refusal is a 402 on two routes, a 400 on three and a 500 on
// delegation. writeLaunchError tests a table's rows in order, and every
// table ends with the row for any other error.
var (
	// launchErrs402 answers LaunchAgentRun and DraftTestCases: an
	// over-budget soft-block (enforcement on) is a distinct, expected
	// refusal, a 402 the UI can message clearly.
	launchErrs402 = []launchErr{
		{agentruns.ErrBudgetExceeded, http.StatusPaymentRequired, ""},
		{nil, http.StatusBadRequest, ""},
	}
	// launchErrs400 answers RunAutomationNow, LaunchTeamRun and
	// LaunchTestRunAgent: every refusal, an over-budget one included.
	launchErrs400 = []launchErr{
		{nil, http.StatusBadRequest, ""},
	}
	// launchErrsDelegate answers DelegateRun: an over-budget refusal is
	// among the 500s.
	launchErrsDelegate = []launchErr{
		{agentruns.ErrInvalidTransition, http.StatusConflict, ""},
		{nil, http.StatusInternalServerError, "failed to launch delegated run"},
	}
)

// writeLaunchError answers a failed run launch by the first row of table
// that err matches.
func writeLaunchError(w http.ResponseWriter, r *http.Request, table []launchErr, err error) {
	for _, row := range table {
		if row.sentinel != nil && !errors.Is(err, row.sentinel) {
			continue
		}
		if row.public == "" {
			writeJSONError(w, row.status, err.Error())
		} else {
			respondError(w, r, row.status, row.public, err)
		}
		return
	}
}

// writeLimitError answers a limit refusal with the numbers and the remedy
// attached, so the frontend can show a person what stopped them and what to do
// without parsing prose.
func (h *Handler) writeLimitError(w http.ResponseWriter, err error) bool {
	var limitErr *orgs.LimitError
	if !errors.As(err, &limitErr) {
		return false
	}
	respondJSON(w, http.StatusForbidden, map[string]interface{}{
		"error":   limitErr.Error(),
		"code":    ErrCodeLimitReached,
		"limit":   limitErr.Key,
		"label":   limitErr.Label,
		"used":    limitErr.Used,
		"allowed": limitErr.Allowed,
		"remedy":  limitErr.Remedy(),
	})
	return true
}

// writeSharedProductError maps domain errors onto status codes. Validation
// refusals are reported verbatim so the publisher can see why their product
// was turned away (a link, a marker, an overlong field).
func writeSharedProductError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, sharedproducts.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, sharedproducts.ErrDuplicate):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, sharedproducts.ErrRateLimited), errors.Is(err, sharedproducts.ErrPoolFull):
		writeJSONError(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, sharedproducts.ErrNotPublishable), errors.Is(err, sharedproducts.ErrNotVotable):
		writeJSONError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, sharedproducts.ErrBadSort),
		errors.Is(err, sharedproducts.ErrEmptyField),
		errors.Is(err, sharedproducts.ErrTooLong),
		errors.Is(err, sharedproducts.ErrLinksNotAllowed),
		errors.Is(err, sharedproducts.ErrDisallowedText):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		respondInternal(w, r, "failed to update shared products", err)
	}
}

// respondRulesError maps a settings failure to its status: a rule set naming
// an unknown convention, rule or severity is the caller's mistake. Any other
// failure is a 500 with the caller's verb, so a read says "failed to load
// quality rules" and a write "failed to save quality rules" (#379's bug 190).
func (h *Handler) respondRulesError(w http.ResponseWriter, r *http.Request, verb string, err error) {
	if errors.Is(err, settings.ErrInvalidRules) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondInternal(w, r, verb, err)
}

// respondInviteError answers a failed invite-token resolution: the
// participant-facing verdicts (unknown, revoked, expired, interview closed)
// pass through as 404s, anything else is an internal failure.
func respondInviteError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, interviews.ErrInviteNotFound),
		errors.Is(err, interviews.ErrInviteRevoked),
		errors.Is(err, interviews.ErrInviteExpired),
		errors.Is(err, interviews.ErrInterviewClosed):
		writeJSONError(w, http.StatusNotFound, err.Error())
	default:
		respondInternal(w, r, "failed to resolve invite", err)
	}
}

// writeProviderTokenError answers a token store error: a caller mistake is
// 400, a server with no secrets key 503, anything else 500.
func writeProviderTokenError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, usertokens.ErrInvalid):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, usertokens.ErrUnavailable):
		writeJSONError(w, http.StatusServiceUnavailable, err.Error())
	default:
		respondInternal(w, r, "failed to read or write the provider token", err)
	}
}
