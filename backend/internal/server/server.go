package server

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/tturner/rangerdanger/backend/internal/config"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
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
	engine          *gin.Engine
	cfg             *config.Config
	db              *gorm.DB
	loader          *labs.Loader
	catalogMu       sync.RWMutex
	catalog         *labs.Catalog // packages from the last successful seed
	orchestrator    *orchestrator.Orchestrator
	execInContainer func(context.Context, string, []string, int) (string, string, int, error)
	// rng is the range lifecycle. Range-bound handlers run against its
	// current generation, which owns the containd client.
	rng rangeLifecycle
	// The policy, capture and traffic state below describe the serving
	// range; activateRange resets it for every new generation.
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

// New constructs a server with routes registered. catalog is the result of
// the startup seed and must contain cfg.Package. rangeOpts wires the range
// lifecycle; the server supplies its catalog and activation hook.
func New(cfg *config.Config, db *gorm.DB, loader *labs.Loader, catalog *labs.Catalog, orchestrator *orchestrator.Orchestrator, rangeOpts lifecycle.Options) (*Server, error) {
	s := &Server{
		engine:       gin.Default(),
		cfg:          cfg,
		db:           db,
		loader:       loader,
		catalog:      catalog,
		orchestrator: orchestrator,
	}
	s.execInContainer = s.orchestrator.ExecCommand

	rangeOpts.Catalog = s.currentCatalog
	rangeOpts.Activate = s.activateRange
	manager, err := lifecycle.New(rangeOpts)
	if err != nil {
		return nil, fmt.Errorf("range lifecycle: %w", err)
	}
	s.rng = manager

	s.applyMigrations()
	s.registerMiddleware()
	s.registerRoutes()
	return s, nil
}

// Run reconciles the range left by the previous process, then serves HTTP.
func (s *Server) Run(_ context.Context) error {
	s.rng.Start()
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

		api.GET("/range", s.handleGetRange)
		api.POST("/range", s.handlePostRange)
		api.GET("/packages", s.handleListPackages)

		admin := api.Group("/admin")
		admin.POST("/seed", s.handleSeedDefinitions)

		// Curriculum and lab-instance records live in the database and
		// are served whatever the range is doing.
		labsGroup := api.Group("/labs")
		{
			labsGroup.GET("/templates", s.handleListLabTemplates)
			labsGroup.GET("/instances", s.handleListLabInstances)
			labsGroup.GET("/instances/:id", s.handleGetLabInstance)
			labsGroup.GET("/instances/:id/topology", s.handleGetTopology)
			labsGroup.PATCH("/instances/:id/topology", s.handlePatchTopology)
			labsGroup.GET("/instances/:id/metrics", s.handleGetMetrics)
			labsGroup.GET("/instances/:id/events", s.handleGetEvents)
		}
		api.POST("/nodes/:node_id/action", s.handleNodeAction)

		// Scenario routes serve the active package only.
		api.GET("/scenarios", s.handleListScenarios)
		api.GET("/scenarios/:id", s.handleGetScenario)
		api.POST("/scenarios/:id/run", s.handleStartScenarioRun)
		api.GET("/scenario-runs/:id", s.handleGetScenarioRun)

		s.registerRangeRoutes(api.Group("", s.rangeBound()))
	}
}

// registerRangeRoutes registers every route that reaches the range: its
// containers, its firewall, or its services.
func (s *Server) registerRangeRoutes(rng *gin.RouterGroup) {
	instances := rng.Group("/labs/instances")
	{
		instances.POST("", s.handleCreateLabInstance)
		instances.POST("/:id/start", s.handleStartLabInstance)
		instances.POST("/:id/stop", s.handleStopLabInstance)
		instances.DELETE("/:id", s.handleDeleteLabInstance)
		instances.Any("/:id/nodes/:nodeId/ui/*path", s.handleProxyNodeUI)
		instances.GET("/:id/nodes/:nodeId/terminal", s.handleTerminal)
		instances.GET("/:id/live-events", s.handleGetLiveEvents)
		instances.GET("/:id/graph", s.handleGetInstanceGraph)
	}

	firewall := rng.Group("/firewall")
	{
		firewall.GET("/health", s.handleGetFirewallHealth)
		firewall.GET("/flows", s.handleGetFirewallFlows)
		firewall.GET("/rules", s.handleGetFirewallRules)
		firewall.GET("/compare", s.handleFirewallCompare)
		firewall.GET("/active", s.handleFirewallActive)
		firewall.POST("/apply", s.handleFirewallApply)
		firewall.POST("/apply-custom", s.handleFirewallApplyCustom)
		firewall.POST("/validation-report", s.handleValidationReport)
	}

	workshop := rng.Group("/workshop")
	{
		workshop.GET("/graph", s.handleGetWorkshopGraph)
		workshop.GET("/status", s.handleGetWorkshopStatus)
		workshop.GET("/nodes/:nodeId/terminal", s.handleWorkshopTerminal)
		workshop.POST("/nodes/:nodeId/exec", s.handleWorkshopExec)
		workshop.POST("/reset", s.handleWorkshopReset)
		workshop.POST("/test-suite", s.handleWorkshopTestSuite)
	}

	// Substation data (proxied from rtac-sim)
	sub := rng.Group("/substation")
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
	pcap := rng.Group("/pcap")
	{
		pcap.POST("/start", s.handlePcapStart)
		pcap.POST("/stop", s.handlePcapStop)
		pcap.GET("/status", s.handlePcapStatus)
		pcap.GET("/download", s.handlePcapDownload)
		pcap.GET("/download/:name", s.handlePcapDownloadFile)
		pcap.GET("/list", s.handlePcapList)
	}

	// Traffic generation for Scenario 0
	rng.POST("/traffic/generate", s.handleTrafficGenerate)
	rng.GET("/traffic/status", s.handleTrafficStatus)

	rng.GET("/scenarios/:id/validate", s.handleValidateScenario)
	rng.POST("/scenarios/:id/steps/:stepIdx/execute", s.handleExecuteStep)

	// Containd proxy - enables same-origin access to containd UI
	rng.Any("/containd/*path", s.handleProxyContaind)
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
