package main

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/openv/requirements-platform/internal/api"
	"github.com/openv/requirements-platform/internal/billing"
	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/attributes"
	"github.com/openv/requirements-platform/internal/domain/automations"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/downloads"
	"github.com/openv/requirements-platform/internal/domain/embeddings"
	"github.com/openv/requirements-platform/internal/domain/evidence"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/guided"
	"github.com/openv/requirements-platform/internal/domain/hostedworkers"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/invitations"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/products"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/proposals"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/pushsubs"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/repoconns"
	"github.com/openv/requirements-platform/internal/domain/reports"
	"github.com/openv/requirements-platform/internal/domain/runnersessions"
	"github.com/openv/requirements-platform/internal/domain/settings"
	"github.com/openv/requirements-platform/internal/domain/sharedproducts"
	"github.com/openv/requirements-platform/internal/domain/sharelinks"
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/domain/templates"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/vv"
	"github.com/openv/requirements-platform/internal/domain/workerkeys"
	"github.com/openv/requirements-platform/internal/domain/workitems"
	eventbus "github.com/openv/requirements-platform/internal/events"
	"github.com/openv/requirements-platform/internal/hosting"
	"github.com/openv/requirements-platform/internal/metrics"
	"github.com/openv/requirements-platform/internal/notify"
	"github.com/openv/requirements-platform/internal/orchestration"
	"github.com/openv/requirements-platform/internal/persistence/postgres"
)

// app holds the locals of main() that more than one of its stages uses,
// as fields: main() makes one and calls the stages, its methods in the
// wire_*.go files, in the order main() ran their code before refactor step
// M4 split it.
type app struct {
	ctx                  context.Context
	stop                 context.CancelFunc
	dsn                  string
	port                 string
	uploadsDir           string
	agentsDir            string
	workerKey            string
	selfHosted           bool
	db                   *sql.DB
	artifactRepo         *postgres.ArtifactRepository
	linkRepo             *postgres.LinkRepository
	projectRepo          projects.Repository
	attachmentRepo       attachments.Repository
	projectInfoRepo      *postgres.ProjectInfoRepository
	baselineRepo         *postgres.BaselineRepository
	templateRepo         *postgres.TemplateRepository
	chatterRepo          *postgres.ChatterRepository
	userRepo             *postgres.UserRepository
	memberRepo           *postgres.MemberRepository
	eventRepo            *postgres.EventRepository
	productProfileRepo   *postgres.ProductProfileRepository
	vvRepo               *postgres.VVRepository
	evidenceRepo         *postgres.EvidenceRepository
	workItemRepo         *postgres.WorkItemRepository
	guidedRepo           *postgres.GuidedRepository
	interviewRepo        *postgres.InterviewRepository
	agentRepo            *postgres.AgentRepository
	agentRunRepo         *postgres.AgentRunRepository
	automationRepo       *postgres.AutomationRepository
	proposalRepo         *postgres.ProposalRepository
	repoConnRepo         *postgres.RepoConnectionRepository
	providerRepo         *postgres.ProviderSettingRepository
	teamRepo             *postgres.TeamRepository
	orgRepo              *postgres.OrgRepository
	invitationRepo       *postgres.InvitationRepository
	workerKeyRepo        *postgres.WorkerKeyRepository
	hostedWorkerRepo     *postgres.HostedWorkerRepository
	runnerSessionRepo    *postgres.RunnerSessionRepository
	notificationRepo     *postgres.NotificationRepository
	pushSubRepo          *postgres.PushSubscriptionRepository
	attributeDefRepo     *postgres.AttributeDefinitionRepository
	sharedProductRepo    *postgres.SharedProductRepository
	bus                  *eventbus.DefaultBus
	artifactService      *artifacts.DefaultService
	linkService          *links.DefaultService
	embeddingService     *embeddings.Service
	projectService       projects.Service
	attachmentService    attachments.Service
	baselineService      *baselines.DefaultService
	chatterService       *chatter.DefaultService
	exportService        *exports.DefaultService
	reportService        *reports.DefaultService
	downloadService      *downloads.DefaultService
	templateService      *templates.DefaultService
	userService          *users.DefaultService
	memberService        *members.DefaultService
	orgService           *orgs.DefaultService
	orgTeamService       *orgs.DefaultTeamService
	invitationService    *invitations.DefaultService
	workerKeyService     *workerkeys.DefaultService
	hostedWorkerService  *hostedworkers.DefaultService
	runnerPoolKey        string
	runnerSessionService runnersessions.Service
	provisioner          hosting.Provisioner
	bootstrapOrgID       func() string
	productService       *products.DefaultService
	settingsService      *settings.DefaultService
	attributeService     *attributes.DefaultService
	sharedProductService *sharedproducts.DefaultService
	vvService            *vv.DefaultService
	evidenceService      *evidence.DefaultService
	workItemService      *workitems.DefaultService
	guidedService        *guided.DefaultService
	interviewService     *interviews.DefaultService
	shareLinkService     *sharelinks.DefaultService
	agentService         *agents.FileService
	runService           *agentruns.DefaultService
	automationService    *automations.DefaultService
	repoConnService      *repoconns.DefaultService
	providerService      *providers.DefaultService
	loginService         *providers.DefaultLoginService
	teamService          *teams.DefaultService
	proposalService      *proposals.DefaultService
	metricsCollector     *metrics.Metrics
	sseHub               *api.SSEHub
	hooks                *orchestration.Hooks
	emailMailer          *notify.SMTPMailer
	emailLinkBase        string
	emailVerification    users.EmailVerificationPolicy
	sessionPolicy        users.SessionPolicy
	registrationPolicy   string
	emailDispatcher      *notify.EmailDispatcher
	vapid                notify.VAPIDConfig
	pushSubService       *pushsubs.DefaultService
	pushDispatcher       *notify.PushDispatcher
	notificationService  *notifications.DefaultService
	minutesMonitor       *notify.MinutesMonitor
	deploymentKind       string
	buildSHA             string
	releaseService       *release.DefaultService
	googleOAuth          *api.GoogleOAuthConfig
	oidcConfig           *api.OIDCConfig
	billingService       *billing.Service
	handler              *api.Handler
	srv                  *http.Server
}
