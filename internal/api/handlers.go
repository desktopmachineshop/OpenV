package api

import (
	"net/http"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/attributes"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/downloads"
	"github.com/openv/requirements-platform/internal/domain/embeddings"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/reports"
	"github.com/openv/requirements-platform/internal/domain/settings"
	"github.com/openv/requirements-platform/internal/domain/sharedproducts"
	"github.com/openv/requirements-platform/internal/domain/sharelinks"
	"github.com/openv/requirements-platform/internal/domain/templates"

	"github.com/openv/requirements-platform/internal/billing"
	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/automations"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/evidence"
	"github.com/openv/requirements-platform/internal/domain/guided"
	"github.com/openv/requirements-platform/internal/domain/hostedworkers"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/invitations"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/products"
	"github.com/openv/requirements-platform/internal/domain/proposals"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/pushsubs"
	"github.com/openv/requirements-platform/internal/domain/repoconns"
	"github.com/openv/requirements-platform/internal/domain/runnersessions"
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/vv"
	"github.com/openv/requirements-platform/internal/domain/workerkeys"
	"github.com/openv/requirements-platform/internal/domain/workitems"
	"github.com/openv/requirements-platform/internal/hosting"
	"github.com/openv/requirements-platform/internal/notify"
)

// HandlerDeps carries every service the API layer depends on.
type HandlerDeps struct {
	ArtifactService      artifacts.Service
	LinkService          links.Service
	ProjectService       projects.Service
	AttachmentService    attachments.Service
	ExportService        exports.Service
	DownloadService      downloads.Service
	BaselineService      baselines.Service
	ReportService        reports.Service
	TemplateService      templates.Service
	ChatterService       chatter.Service
	AttributeService     attributes.Service
	SharedProductService sharedproducts.Service
	EmbeddingService     *embeddings.Service
	UploadsDir           string

	UserService     users.Service
	MemberService   members.Service
	ProductService  products.Service
	VVService       vv.Service
	EvidenceService evidence.Service
	SettingsService settings.Service
	ReleaseService  release.Service
	// DeploymentKind is "shared" (default) or "dedicated" (REQ-139).
	DeploymentKind string
	// BuildSHA is the git commit this binary was built from, empty where
	// nothing told it (a local `go run`, the compose stack). /health reports
	// it so a deployment can be matched to a commit — which is what lets the
	// staging smoke gate prove it tested the commit it thinks it did
	// (REQ-141).
	BuildSHA         string
	WorkItemService  workitems.Service
	GuidedService    guided.Service
	InterviewService interviews.Service
	// ShareLinkService mints and resolves project share links (REQ-149).
	ShareLinkService sharelinks.Service
	// FrontendURL is the origin the app is served from, for absolute links
	// in social previews and share pages.
	FrontendURL         string
	AgentService        agents.Service
	RunService          agentruns.Service
	AutomationService   automations.Service
	ProposalService     proposals.Service
	RepoConnService     repoconns.Service
	ProviderService     providers.Service
	LoginService        providers.LoginService
	TeamService         teams.Service
	OrgService          orgs.Service
	OrgTeamService      orgs.TeamService
	WorkerKeyService    workerkeys.Service
	HostedWorkerService hostedworkers.Service
	// RunnerSessionService drives transient runners; nil on deployments
	// with no runner pool configured.
	RunnerSessionService runnersessions.Service
	NotificationService  notifications.Service
	// PushSubService stores per-device web push subscriptions (REQ-109).
	// nil leaves the endpoints answering "not available".
	PushSubService pushsubs.Service
	// VAPID is the web push key pair; the zero value disables push and is
	// what /api/v1/me/push/config reports as enabled=false.
	VAPID       notify.VAPIDConfig
	Provisioner hosting.Provisioner
	// OrgSeeder provisions default agents/crew for a new workspace.
	OrgSeeder func(orgID string) error
	// PublicAPIURL is the externally-reachable API base (connector config).
	PublicAPIURL string
	// ConnectorDistDir holds downloadable Agent Connector bundles.
	ConnectorDistDir string
	Bus              events.Bus
	EventRepo        events.Repository
	SSEHub           *SSEHub
	GoogleOAuth      *GoogleOAuthConfig
	OIDC             *OIDCConfig
	SecureCookies    bool
	// Mailer sends the sign-up verification email; nil or disabled means the
	// feature is inert. EmailLinkBase is the frontend origin the emailed link
	// points at; EmailVerification is the deployment's policy (see
	// notify.VerificationPolicyFromEnv).
	Mailer            notify.Mailer
	EmailLinkBase     string
	EmailVerification users.EmailVerificationPolicy
	// InvitationService backs workspace invitations; nil leaves the
	// endpoints answering 404 and registration unable to see an invitation.
	InvitationService invitations.Service
	// BillingService is the subscription sync path; nil or disabled where
	// no provider is configured.
	BillingService *billing.Service
	// MinutesAlerts tells workspace admins when leased cloud-runner minutes
	// near or reach the month's allowance; nil means no alerts.
	MinutesAlerts *notify.MinutesMonitor
	// Registration is the deployment's sign-up policy ("open" or "closed",
	// see RegistrationPolicyFromEnv); empty means open.
	Registration string
	// SessionPolicy must be the policy the user service was given, so the
	// session cookie and the server agree on when a session ends (REQ-99).
	// The zero value means the defaults.
	SessionPolicy users.SessionPolicy
	// CrossSiteCookies marks deployments where the frontend and API are served
	// from different sites (e.g. two *.up.railway.app domains): auth cookies are
	// issued with SameSite=None, and Secure is forced on since browsers reject
	// SameSite=None cookies without it. They also carry the Partitioned
	// attribute (CHIPS), the only form of third-party cookie current
	// browsers still accept. Safari still refuses them, so prefer serving the
	// API on the frontend's origin (docs/railway.md) and leave this off.
	CrossSiteCookies bool
}

// Handler holds references to domain services
type Handler struct {
	artifactService   artifacts.Service
	linkService       links.Service
	projectService    projects.Service
	attachmentService attachments.Service
	exportService     exports.Service
	downloadService   downloads.Service
	baselineService   baselines.Service
	reportService     reports.Service
	templateService   templates.Service
	chatterService    chatter.Service
	attributeService  attributes.Service
	// sharedProductService backs the cross-tenant community pool of demo
	// products (see shared_product_handlers.go).
	sharedProductService sharedproducts.Service
	embeddingService     *embeddings.Service
	uploadsDir           string

	userService          users.Service
	memberService        members.Service
	productService       products.Service
	vvService            vv.Service
	evidenceService      evidence.Service
	settingsService      settings.Service
	releaseService       release.Service
	deploymentKind       string
	buildSHA             string
	workItemService      workitems.Service
	guidedService        guided.Service
	interviewService     interviews.Service
	shareLinkService     sharelinks.Service
	frontendURL          string
	agentService         agents.Service
	runService           agentruns.Service
	automationService    automations.Service
	proposalService      proposals.Service
	repoConnService      repoconns.Service
	providerService      providers.Service
	loginService         providers.LoginService
	teamService          teams.Service
	orgService           orgs.Service
	orgTeamService       orgs.TeamService
	workerKeyService     workerkeys.Service
	hostedWorkerService  hostedworkers.Service
	runnerSessionService runnersessions.Service
	notificationService  notifications.Service
	pushSubService       pushsubs.Service
	vapid                notify.VAPIDConfig
	provisioner          hosting.Provisioner
	orgSeeder            func(orgID string) error
	publicAPIURL         string
	connectorDistDir     string
	bus                  events.Bus
	eventRepo            events.Repository
	sseHub               *SSEHub
	googleOAuth          *GoogleOAuthConfig
	oidc                 *OIDCConfig
	secureCookies        bool
	cookieSameSite       http.SameSite

	// Rate limiters for the public (invite-token) interview endpoints; see
	// ratelimit.go for defaults and environment overrides.
	interviewMsgLimiter    *rateLimiter // per-invite participant messages
	interviewIPLimiter     *rateLimiter // per-IP intro GETs
	interviewStreamLimiter *rateLimiter // per-IP SSE stream connects (more generous: reconnects are routine)

	// Throttles for the credential endpoints (see ratelimit.go): every
	// sign-in attempt per client address, failed sign-ins per account,
	// registrations per address, and SSO starts/callbacks per address.
	authIPLimiter      *rateLimiter
	authAccountLimiter *rateLimiter
	registerIPLimiter  *rateLimiter
	ssoIPLimiter       *rateLimiter
	// verifyResendLimiter bounds verification mails per account (resend and
	// change of address share it).
	verifyResendLimiter *rateLimiter
	// passwordResetLimiter bounds reset mails per address (REQ-158), so
	// the sign-in page cannot be used to flood one inbox.
	passwordResetLimiter *rateLimiter
	// invitePreviewLimiter bounds invite-link previews per address. Separate
	// from authIPLimiter on purpose: opening an invite link must never spend
	// somebody's sign-in budget (see ratelimit.go).
	invitePreviewLimiter *rateLimiter
	// billingRefreshLimiter bounds synchronous subscription re-reads per
	// workspace; billingWriteLimiter bounds the purchase writes (see
	// ratelimit.go).
	billingRefreshLimiter *rateLimiter
	billingWriteLimiter   *rateLimiter
	// billing is the subscription sync path; nil, or disabled, on a
	// deployment with no billing provider, where every billing route
	// answers 404 billing_unavailable.
	billing *billing.Service
	// minutesAlerts is nil-safe; see notify.MinutesMonitor.Check.
	minutesAlerts *notify.MinutesMonitor
	// inviteLimiter bounds invitations per INVITING ACCOUNT: creating one
	// mails an address the sender chose, so the endpoint is a mail relay
	// (see ratelimit.go).
	inviteLimiter *rateLimiter

	// Sign-up email verification (see email_verification_handlers.go).
	mailer            notify.Mailer
	emailLinkBase     string
	emailVerification users.EmailVerificationPolicy

	// Workspace invitations and the registration policy (REQ-95; see
	// invitation_handlers.go and registration_policy.go).
	invitationService invitations.Service
	registration      string
	// sessionPolicy is the same policy the user service enforces; the handler
	// holds it so the cookie it writes expires when the session does.
	sessionPolicy users.SessionPolicy
}

// NewHandler creates a new API handler
func NewHandler(deps HandlerDeps) *Handler {
	cookieSameSite := http.SameSiteLaxMode
	secureCookies := deps.SecureCookies
	if deps.CrossSiteCookies {
		cookieSameSite = http.SameSiteNoneMode
		secureCookies = true
	}
	h := &Handler{
		artifactService:        deps.ArtifactService,
		linkService:            deps.LinkService,
		projectService:         deps.ProjectService,
		attachmentService:      deps.AttachmentService,
		exportService:          deps.ExportService,
		downloadService:        deps.DownloadService,
		baselineService:        deps.BaselineService,
		reportService:          deps.ReportService,
		templateService:        deps.TemplateService,
		chatterService:         deps.ChatterService,
		attributeService:       deps.AttributeService,
		sharedProductService:   deps.SharedProductService,
		embeddingService:       deps.EmbeddingService,
		uploadsDir:             deps.UploadsDir,
		userService:            deps.UserService,
		memberService:          deps.MemberService,
		productService:         deps.ProductService,
		settingsService:        deps.SettingsService,
		releaseService:         deps.ReleaseService,
		deploymentKind:         deps.DeploymentKind,
		buildSHA:               deps.BuildSHA,
		vvService:              deps.VVService,
		evidenceService:        deps.EvidenceService,
		workItemService:        deps.WorkItemService,
		guidedService:          deps.GuidedService,
		interviewService:       deps.InterviewService,
		shareLinkService:       deps.ShareLinkService,
		frontendURL:            strings.TrimRight(deps.FrontendURL, "/"),
		agentService:           deps.AgentService,
		runService:             deps.RunService,
		automationService:      deps.AutomationService,
		proposalService:        deps.ProposalService,
		repoConnService:        deps.RepoConnService,
		providerService:        deps.ProviderService,
		loginService:           deps.LoginService,
		teamService:            deps.TeamService,
		orgService:             deps.OrgService,
		orgTeamService:         deps.OrgTeamService,
		workerKeyService:       deps.WorkerKeyService,
		hostedWorkerService:    deps.HostedWorkerService,
		runnerSessionService:   deps.RunnerSessionService,
		notificationService:    deps.NotificationService,
		pushSubService:         deps.PushSubService,
		vapid:                  deps.VAPID,
		provisioner:            deps.Provisioner,
		orgSeeder:              deps.OrgSeeder,
		publicAPIURL:           deps.PublicAPIURL,
		connectorDistDir:       deps.ConnectorDistDir,
		bus:                    deps.Bus,
		eventRepo:              deps.EventRepo,
		sseHub:                 deps.SSEHub,
		googleOAuth:            deps.GoogleOAuth,
		oidc:                   deps.OIDC,
		secureCookies:          secureCookies,
		cookieSameSite:         cookieSameSite,
		interviewMsgLimiter:    newRateLimiterFromEnv(envInterviewMsgBurst, envInterviewMsgRefill, defaultInterviewMsgBurst, defaultInterviewMsgRefill),
		interviewIPLimiter:     newRateLimiterFromEnv(envInterviewIPBurst, envInterviewIPRefill, defaultInterviewIPBurst, defaultInterviewIPRefill),
		interviewStreamLimiter: newRateLimiterFromEnv(envInterviewStreamBurst, envInterviewStreamRefill, defaultInterviewStreamBurst, defaultInterviewStreamRefill),
		authIPLimiter:          newRateLimiterFromEnv(envAuthIPBurst, envAuthIPRefill, defaultAuthIPBurst, defaultAuthIPRefill),
		authAccountLimiter:     newRateLimiterFromEnv(envAuthAccountBurst, envAuthAccountRefill, defaultAuthAccountBurst, defaultAuthAccountRefill),
		registerIPLimiter:      newRateLimiterFromEnv(envRegisterIPBurst, envRegisterIPRefill, defaultRegisterIPBurst, defaultRegisterIPRefill),
		ssoIPLimiter:           newRateLimiterFromEnv(envSSOIPBurst, envSSOIPRefill, defaultSSOIPBurst, defaultSSOIPRefill),
		verifyResendLimiter:    newRateLimiterFromEnv(envVerifyResendBurst, envVerifyResendRefill, defaultVerifyResendBurst, defaultVerifyResendRefill),
		passwordResetLimiter:   newRateLimiterFromEnv(envPasswordResetBurst, envPasswordResetRefill, defaultPasswordResetBurst, defaultPasswordResetRefill),
		invitePreviewLimiter:   newRateLimiterFromEnv(envInvitePreviewBurst, envInvitePreviewRefill, defaultInvitePreviewBurst, defaultInvitePreviewRefill),
		billingRefreshLimiter:  newRateLimiterFromEnv(envBillingRefreshBurst, envBillingRefreshRefill, defaultBillingRefreshBurst, defaultBillingRefreshRefill),
		billingWriteLimiter:    newRateLimiterFromEnv(envBillingWriteBurst, envBillingWriteRefill, defaultBillingWriteBurst, defaultBillingWriteRefill),
		billing:                deps.BillingService,
		minutesAlerts:          deps.MinutesAlerts,
		inviteLimiter:          newRateLimiterFromEnv(envInviteBurst, envInviteRefill, defaultInviteBurst, defaultInviteRefill),
		mailer:                 deps.Mailer,
		emailLinkBase:          deps.EmailLinkBase,
		emailVerification:      deps.EmailVerification,
		invitationService:      deps.InvitationService,
		registration:           deps.Registration,
		sessionPolicy:          deps.SessionPolicy,
	}
	if h.billing != nil {
		// Billed seats are the seat limit's own reading, and the return
		// origin is the app's unless the operator named another.
		h.billing.SetSeatCounter(h.countOrgSeats)
		h.billing.DefaultReturnURL(h.frontendURL)
	}
	readRequestSettingsAtBoot()
	return h
}
