package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/tturner/rangerdanger/backend/internal/config"
	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/models"
	"github.com/tturner/rangerdanger/backend/internal/orchestrator"
	"github.com/tturner/rangerdanger/backend/internal/version"
)

// pcapState tracks an active PCAP capture session.
// containd has no capture ID — we track by filePrefix + startedAt and
// match resulting files from the list endpoint.
type pcapState struct {
	Capturing   bool     `json:"capturing"`
	DurationSec int      `json:"duration_sec"`
	StartedAt   string   `json:"started_at,omitempty"`
	FileReady   bool     `json:"file_ready"`
	FilePrefix  string   `json:"file_prefix,omitempty"` // prefix used in containd config
	Files       []string `json:"files,omitempty"`       // resulting PCAP filenames from containd
	Fallback    bool     `json:"fallback"`              // true when using tcpdump fallback
}

// trafficState tracks an active traffic generation session.
type trafficState struct {
	Generating     bool   `json:"generating"`
	DurationSec    int    `json:"duration_sec"`
	StartedAt      string `json:"started_at,omitempty"`
	FlowsGenerated int    `json:"flows_generated"`
}

// Server wraps the Gin router and dependencies.
type Server struct {
	engine         *gin.Engine
	cfg            *config.Config
	db             *gorm.DB
	loader         *labs.Loader
	orchestrator   *orchestrator.Orchestrator
	containdClient *containd.Client
	activeConfigMu sync.RWMutex
	activeConfig   string // "weak", "improved", or "custom"
	// policySource records how the active policy was applied. Possible
	// values:
	//   "weak"              — canned weak baseline (/api/firewall/apply)
	//   "hardened-reference" — canned hardened policy (/api/firewall/apply)
	//   "plan-custom"       — student's Lab 1.4 plan, pushed by the
	//                         frontend "Apply Your Plan" button
	//                         (/api/firewall/apply-custom)
	//   "manual-custom"     — observed: a policy the student committed
	//                         directly in containd's CLI/UI. Set by
	//                         the background policyObserver goroutine
	//                         when the running-config hash diverges
	//                         from the last applied hash for >5s.
	//   ""                  — never applied / initial state
	// The frontend uses this to label the status banner accurately
	// ("Your custom policy (Lab 1.4 plan)" vs "(your containd commit)")
	// and survives page reloads, which a frontend-only session-state
	// approach would not.
	policySource string
	// lastAppliedHash is the canonical SHA-256 of the firewall sub-
	// document at the moment of the last successful backend apply
	// (weak / improved / plan-custom). The observer compares it to
	// containd's live running-config hash to spot manual commits.
	lastAppliedHash string
	// lastAppliedAt records when lastAppliedHash was set. The
	// observer skips its check for >graceWindow (5s) after an
	// apply so a hash-in-flight race doesn't briefly flip to
	// manual-custom on a normal button-driven apply.
	lastAppliedAt time.Time
	pcapMu        sync.Mutex
	pcap          pcapState
	trafficMu     sync.Mutex
	traffic       trafficState
}

// New constructs a server with routes registered.
func New(cfg *config.Config, db *gorm.DB, loader *labs.Loader, orchestrator *orchestrator.Orchestrator, containdClient *containd.Client) *Server {
	engine := gin.Default()

	// Determine initial active config from seed path
	activeConfig := "weak"
	if strings.Contains(cfg.ContaindConfigPath, "improved") {
		activeConfig = "improved"
	}

	s := &Server{
		engine:         engine,
		cfg:            cfg,
		db:             db,
		containdClient: containdClient,
		activeConfig:   activeConfig,
		loader:         loader,
		orchestrator:   orchestrator,
	}

	s.applyMigrations()
	s.registerMiddleware()
	s.registerRoutes()
	// Kick off the background poller that watches containd's running
	// config for manual commits the backend didn't initiate. context.
	// Background() is fine here — the goroutine lives for the
	// lifetime of the process. (Tests construct Server directly
	// without going through New, so they get a no-op observer.)
	s.startPolicyObserver(context.Background())
	return s
}

// Run starts the HTTP server.
func (s *Server) Run(_ context.Context) error {
	addr := fmt.Sprintf(":%d", s.cfg.HTTPPort)
	return s.engine.Run(addr)
}

func (s *Server) applyMigrations() {
	_ = s.db.AutoMigrate(
		&models.LabTemplate{},
		&models.LabInstance{},
		&models.NodeDefinition{},
		&models.Scenario{},
		&models.ScenarioRun{},
		&models.TelemetryPoint{},
	)
}

func (s *Server) registerRoutes() {
	api := s.engine.Group("/api")
	{
		api.GET("/health", s.handleHealth)
		// Note: /api/build (not /api/version) — nginx proxies /api/version
		// to FUXA's HMI for its own version endpoint. See proxy/nginx.conf.
		api.GET("/build", s.handleVersion)

		admin := api.Group("/admin")
		admin.POST("/seed", s.handleSeedDefinitions)

		labsGroup := api.Group("/labs")
		{
			labsGroup.GET("/templates", s.handleListLabTemplates)
			labsGroup.POST("/templates", s.handleCreateLabTemplate)

			labsGroup.POST("/instances", s.handleCreateLabInstance)
			labsGroup.GET("/instances", s.handleListLabInstances)
			labsGroup.GET("/instances/:id", s.handleGetLabInstance)
			labsGroup.POST("/instances/:id/start", s.handleStartLabInstance)
			labsGroup.POST("/instances/:id/stop", s.handleStopLabInstance)
			labsGroup.DELETE("/instances/:id", s.handleDeleteLabInstance)
			labsGroup.GET("/instances/:id/topology", s.handleGetTopology)
			labsGroup.GET("/instances/:id/graph", s.handleGetInstanceGraph)
			labsGroup.PATCH("/instances/:id/topology", s.handlePatchTopology)
			labsGroup.GET("/instances/:id/metrics", s.handleGetMetrics)
			labsGroup.GET("/instances/:id/events", s.handleGetEvents)
			labsGroup.Any("/instances/:id/nodes/:nodeId/ui/*path", s.handleProxyNodeUI)
			labsGroup.GET("/instances/:id/nodes/:nodeId/terminal", s.handleTerminal)
			labsGroup.GET("/instances/:id/live-events", s.handleGetLiveEvents)
		}

		// Firewall endpoints
		api.GET("/firewall/health", s.handleGetFirewallHealth)
		api.GET("/firewall/sessions", s.handleGetFirewallSessions)
		api.GET("/firewall/rules", s.handleGetFirewallRules)
		api.GET("/firewall/compare", s.handleFirewallCompare)
		api.GET("/firewall/active", s.handleFirewallActive)
		api.POST("/firewall/apply", s.handleFirewallApply)
		api.POST("/firewall/apply-custom", s.handleFirewallApplyCustom)
		api.POST("/firewall/validation-report", s.handleValidationReport)

		// Workshop endpoints
		api.GET("/workshop/graph", s.handleGetWorkshopGraph)
		api.GET("/workshop/status", s.handleGetWorkshopStatus)
		api.GET("/workshop/nodes/:nodeId/terminal", s.handleWorkshopTerminal)
		api.POST("/workshop/nodes/:nodeId/exec", s.handleWorkshopExec)
		api.POST("/workshop/reset", s.handleWorkshopReset)
		api.POST("/workshop/test-suite", s.handleWorkshopTestSuite)

		api.POST("/nodes/:node_id/action", s.handleNodeAction)

		// Substation data (proxied from rtac-sim)
		sub := api.Group("/substation")
		{
			sub.GET("/tags", s.handleSubstationTags)
			sub.GET("/state", s.handleSubstationState)
			sub.POST("/command/:device", s.handleSubstationCommand)
			sub.POST("/lab-control", s.handleSubstationLabControl)
			sub.GET("/audit", s.handleSubstationAudit)
			sub.GET("/health", s.handleSubstationHealth)
			sub.GET("/network-events", s.handleSubstationNetworkEvents)
		}

		// PCAP capture endpoints (containd API with tcpdump fallback)
		pcap := api.Group("/pcap")
		{
			pcap.POST("/start", s.handlePcapStart)
			pcap.POST("/stop", s.handlePcapStop)
			pcap.GET("/status", s.handlePcapStatus)
			pcap.GET("/download", s.handlePcapDownload)
			pcap.GET("/download/:name", s.handlePcapDownloadFile)
			pcap.GET("/list", s.handlePcapList)
		}

		// Traffic generation for Scenario 0
		api.POST("/traffic/generate", s.handleTrafficGenerate)
		api.GET("/traffic/status", s.handleTrafficStatus)

		api.GET("/scenarios", s.handleListScenarios)
		api.POST("/scenarios", s.handleCreateScenario)
		api.GET("/scenarios/:id", s.handleGetScenario)
		api.POST("/scenarios/:id/run", s.handleStartScenarioRun)
		api.GET("/scenario-runs/:id", s.handleGetScenarioRun)
		api.GET("/scenarios/:id/validate", s.handleValidateScenario)
		api.POST("/scenarios/:id/steps/:stepIdx/execute", s.handleExecuteStep)

		// Containd proxy - enables same-origin access to containd UI
		api.Any("/containd/*path", s.handleProxyContaind)
	}
}

func (s *Server) registerMiddleware() {
	s.engine.Use(s.corsMiddleware())
}

func (s *Server) corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		allowedOrigin := ""

		if origin != "" && s.isOriginAllowed(origin) {
			allowedOrigin = origin
		} else if s.isOriginAllowed("*") {
			allowedOrigin = "*"
		}

		if allowedOrigin != "" {
			c.Writer.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		}
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		// Allow-Credentials is only valid when Allow-Origin is a specific
		// host. Browsers reject the combination with "*". Skip the header
		// in the wildcard case (the lab default for single-tenant use).
		if allowedOrigin != "" && allowedOrigin != "*" {
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func (s *Server) isOriginAllowed(origin string) bool {
	if len(s.cfg.AllowedOrigins) == 0 {
		return true
	}
	for _, allowed := range s.cfg.AllowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}

func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) handleVersion(c *gin.Context) {
	c.JSON(http.StatusOK, version.Get())
}
