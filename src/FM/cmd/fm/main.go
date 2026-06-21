package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	cfg "github.com/dashfabric/fm/pkg/config"
	cm "github.com/dashfabric/fm/pkg/cm"
	dm "github.com/dashfabric/fm/pkg/dm"
	gm "github.com/dashfabric/fm/pkg/gm"
	dal "github.com/dashfabric/fm/pkg/dal"
	obs "github.com/dashfabric/fm/pkg/observability"
	api "github.com/dashfabric/fm/pkg/api"
)

func main() {
	// Command-line flags
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	port := flag.Int("port", 5051, "gRPC server port")
	restPort := flag.Int("rest-port", 8080, "REST API port")

	flag.Parse()

	// Initialize structured logger
	structuredLogger, err := obs.NewStructuredLogger(*logLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer structuredLogger.Sync()

	// Wrap in SimpleLogger adapter
	logger := &SimpleLogger{logger: structuredLogger}

	// Set structured logger for all modules
	cm.SetLogger(structuredLogger)
	dm.SetLogger(structuredLogger)
	gm.SetLogger(structuredLogger)
	dal.SetLogger(structuredLogger)

	// Initialize metrics registry
	metricsRegistry := obs.NewMetricsRegistry()
	logger.Info("Metrics registry initialized")

	// Initialize tracing context (OTLP/Jaeger)
	tracingContext, err := obs.NewTracingContext("localhost:4318", "fabric-manager")
	if err != nil {
		logger.Error("Failed to initialize tracing", "error", err)
		// Continue without tracing
	} else {
		defer tracingContext.Shutdown(context.Background())
		logger.Info("Tracing initialized", "endpoint", "localhost:4318")
	}

	// Set metrics registry for all modules
	cm.SetMetricsRegistry(metricsRegistry)
	dm.SetMetricsRegistry(metricsRegistry)
	gm.SetMetricsRegistry(metricsRegistry)
	dal.SetMetricsRegistry(metricsRegistry)

	// Set tracing context for all modules
	cm.SetTracingContext(tracingContext)
	dm.SetTracingContext(tracingContext)
	gm.SetTracingContext(tracingContext)
	dal.SetTracingContext(tracingContext)

	logger.Info("Starting Fabric Manager",
		"config", *configPath,
		"grpc_port", *port,
		"rest_port", *restPort,
	)

	// Load configuration
	config, err := LoadConfig(*configPath)
	if err != nil {
		logger.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize services
	services, err := InitializeServices(ctx, config, logger, metricsRegistry)
	if err != nil {
		logger.Error("Failed to initialize services", "error", err)
		os.Exit(1)
	}

	// Start all services
	if err := services.Start(ctx); err != nil {
		logger.Error("Failed to start services", "error", err)
		os.Exit(1)
	}

	// Start gRPC server (non-blocking)
	go func() {
		if err := services.StartGRPCServer(*port); err != nil {
			logger.Error("gRPC server error", "error", err)
		}
	}()

	// Start REST API server (non-blocking)
	go func() {
		if err := services.StartRESTServer(*restPort); err != nil {
			logger.Error("REST server error", "error", err)
		}
	}()

	logger.Info("Fabric Manager started",
		"grpc", fmt.Sprintf("0.0.0.0:%d", *port),
		"rest", fmt.Sprintf("0.0.0.0:%d", *restPort),
	)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigChan:
		logger.Info("Received signal", "signal", sig)
	case <-ctx.Done():
		logger.Info("Context cancelled")
	}

	// Graceful shutdown
	logger.Info("Shutting down Fabric Manager...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := services.Shutdown(shutdownCtx); err != nil {
		logger.Error("Shutdown error", "error", err)
		os.Exit(1)
	}

	logger.Info("Fabric Manager stopped")
}

// SimpleLogger is a basic logger implementation
type SimpleLogger struct {
	logger obs.Logger
}

func NewLogger(level string) (*SimpleLogger, error) {
	logger, err := obs.NewStructuredLogger(level)
	if err != nil {
		return nil, err
	}
	return &SimpleLogger{logger: logger}, nil
}

func (l *SimpleLogger) Info(msg string, keyvals ...interface{}) {
	l.logger.Info(msg, keyvals...)
}

func (l *SimpleLogger) Error(msg string, keyvals ...interface{}) {
	l.logger.Error(msg, keyvals...)
}

func (l *SimpleLogger) Debug(msg string, keyvals ...interface{}) {
	l.logger.Debug(msg, keyvals...)
}

func (l *SimpleLogger) Sync() error {
	if sl, ok := l.logger.(*obs.StructuredLogger); ok {
		return sl.Sync()
	}
	return nil
}

// Config holds application configuration
type Config struct {
	ControlBroker struct {
		Address string
		Topics  []string
	}
	Dedup struct {
		CacheSize int
		TTL       int // seconds
	}
	Database struct {
		EtcdEndpoints []string
		RocksDBPath   string
	}
	Observability struct {
		MetricsPort int
		JaegerURL   string
	}
}

// LoadConfig loads configuration from file
func LoadConfig(path string) (*Config, error) {
	// Placeholder: In production, parse YAML/JSON config
	config := &Config{}
	config.ControlBroker.Address = "localhost:2379"
	config.ControlBroker.Topics = []string{"/fm/events"}
	config.Dedup.CacheSize = 10000
	config.Dedup.TTL = 300
	config.Database.EtcdEndpoints = []string{"localhost:2379"}
	config.Database.RocksDBPath = "/var/lib/fm/db"
	config.Observability.MetricsPort = 8081
	config.Observability.JaegerURL = "http://localhost:14268/api/traces"

	return config, nil
}

// Services holds all initialized FM services
type Services struct {
	logger            *SimpleLogger
	metricsRegistry   *obs.MetricsRegistry
	healthChecker     *obs.HealthChecker
	cmPipeline        cm.EventPipeline
	dmManager         dm.DataManager
	gmService         gm.GoalStateManager
	dalService        dal.DPUAbstractionManager
	apiHandler        *api.APIHandler
}

// InitializeServices initializes all FM services using the ServiceFactory
func InitializeServices(ctx context.Context, config *Config, logger *SimpleLogger, metricsRegistry *obs.MetricsRegistry) (*Services, error) {
	services := &Services{
		logger:          logger,
		metricsRegistry: metricsRegistry,
		healthChecker:   obs.NewHealthChecker(),
	}

	// Convert old Config to new AppConfig format
	appConfig := cfg.AppConfig{
		CM: cfg.DefaultCMConfig(),
		DM: cfg.DefaultDMConfig(),
		GM: cfg.DefaultGMConfig(),
		DAL: cfg.DALConfig{
			PoolWorkers:        4,
			Vendors:            make(map[string]map[string]interface{}),
			ProgrammingTimeout: 60 * time.Second,
		},
	}

	// Create CM (Config Management) EventPipeline
	logger.Info("Creating CM (Config Management)...")
	cmCache := cm.NewLRUCache(10000)
	cmValidator := &cm.NullValidator{}
	cmPipeline := cm.NewEventPipeline(nil, cmCache, cmValidator) // nil subscriber will use NullSubscriber
	services.cmPipeline = cmPipeline

	// Create DM (Data Management) DataManager
	logger.Info("Creating DM (Data Management)...")
	dmManager := dm.NewDataManager()
	services.dmManager = dmManager

	// Create service factory
	factory := cfg.NewServiceFactory(appConfig, &configLogger{logger: logger})

	// Initialize all services
	if err := factory.CreateAllServices(ctx); err != nil {
		return nil, fmt.Errorf("failed to create services: %w", err)
	}

	// Get initialized services
	gmSvc, dalSvc := factory.GetServices()
	services.gmService = gmSvc
	services.dalService = dalSvc

	// Create API handler
	logger.Info("Creating API handler...")
	tracingContext, _ := obs.NewTracingContext("localhost:4318", "fabric-manager")
	services.apiHandler = api.NewAPIHandler(cmPipeline, dmManager, gmSvc, logger.logger, tracingContext)

	return services, nil
}

// configLogger adapts logger for config.Logger interface
type configLogger struct {
	logger *SimpleLogger
}

func (cl *configLogger) Printf(format string, v ...interface{}) {
	cl.logger.Info(fmt.Sprintf(format, v...))
}

func (cl *configLogger) Fatalf(format string, v ...interface{}) {
	cl.logger.Error(fmt.Sprintf(format, v...))
}

// StartGRPCServer starts the gRPC server (stub implementation)
func (s *Services) StartGRPCServer(port int) error {
	s.logger.Info("Starting gRPC server", "port", port)
	// TODO: Implement gRPC server with actual FM services
	return nil
}

// StartRESTServer starts the REST API server with metrics and health endpoints
func (s *Services) StartRESTServer(port int) error {
	s.logger.Info("Starting REST server", "port", port)

	mux := http.NewServeMux()

	// Register health check endpoints
	if s.healthChecker != nil {
		mux.HandleFunc("/healthz", s.healthChecker.HealthzHandler())
		mux.HandleFunc("/readyz", s.healthChecker.ReadyzHandler())
	}

	// Register metrics endpoint
	if s.metricsRegistry != nil {
		mux.HandleFunc("/metrics", obs.MetricsHandler())
	}

	// Register API endpoints
	if s.apiHandler != nil {
		mux.HandleFunc("/api/vnets", s.apiHandler.ListVNETs)
		mux.HandleFunc("/api/goal-state", s.apiHandler.GetGoalState)
		mux.HandleFunc("/api/program", s.apiHandler.ProgramDevice)
	}

	return http.ListenAndServe(fmt.Sprintf(":%d", port), mux)
}

// Start starts all services in dependency order
func (s *Services) Start(ctx context.Context) error {
	s.logger.Info("Starting all services...")

	// Start CM (Config Management) - event source
	if s.cmPipeline != nil {
		s.logger.Info("Starting CM (Config Management)...")
		if err := s.cmPipeline.Start(ctx); err != nil {
			return fmt.Errorf("failed to start CM: %w", err)
		}
	}

	// Start DM (Data Management) - subscribe to CM events
	if s.dmManager != nil {
		s.logger.Info("Starting DM (Data Management)...")
		eventStream := s.cmPipeline.GetEventStream()
		if err := s.dmManager.Start(ctx, eventStream); err != nil {
			return fmt.Errorf("failed to start DM: %w", err)
		}
	}

	// Start GM (Goal State Management) - has explicit lifecycle
	if gmImpl, ok := s.gmService.(*gm.GoalStateManagerImpl); ok {
		s.logger.Info("Starting GM (Goal State Management)...")
		if err := gmImpl.Start(ctx); err != nil {
			return fmt.Errorf("failed to start GM: %w", err)
		}
	}

	// Start DAL (DPU Abstraction Layer) - has explicit lifecycle
	if dalImpl, ok := s.dalService.(*dal.DPUAbstractionManagerImpl); ok {
		s.logger.Info("Starting DAL (DPU Abstraction Layer)...")
		if err := dalImpl.Start(ctx); err != nil {
			return fmt.Errorf("failed to start DAL: %w", err)
		}
	}

	// Mark all services as ready
	s.healthChecker.SetServiceStatus("cm", true)
	s.healthChecker.SetServiceStatus("dm", true)
	s.healthChecker.SetServiceStatus("gm", true)
	s.healthChecker.SetServiceStatus("dal", true)

	s.logger.Info("All services started successfully")
	return nil
}

// Shutdown gracefully shuts down all services in reverse order
func (s *Services) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down all services...")

	// Mark all services as not ready
	s.healthChecker.SetServiceStatus("cm", false)
	s.healthChecker.SetServiceStatus("dm", false)
	s.healthChecker.SetServiceStatus("gm", false)
	s.healthChecker.SetServiceStatus("dal", false)

	// Shutdown DAL (DPU Abstraction Layer)
	if dalImpl, ok := s.dalService.(*dal.DPUAbstractionManagerImpl); ok {
		s.logger.Info("Stopping DAL...")
		if err := dalImpl.Stop(); err != nil {
			s.logger.Error("Error stopping DAL", "error", err.Error())
		}
	}

	// Shutdown GM (Goal State Management)
	if gmImpl, ok := s.gmService.(*gm.GoalStateManagerImpl); ok {
		s.logger.Info("Stopping GM...")
		if err := gmImpl.Stop(); err != nil {
			s.logger.Error("Error stopping GM", "error", err.Error())
		}
	}

	// Shutdown DM (Data Management)
	if s.dmManager != nil {
		s.logger.Info("Stopping DM...")
		if err := s.dmManager.Stop(); err != nil {
			s.logger.Error("Error stopping DM", "error", err.Error())
		}
	}

	// Shutdown CM (Config Management)
	if s.cmPipeline != nil {
		s.logger.Info("Stopping CM...")
		if err := s.cmPipeline.Stop(); err != nil {
			s.logger.Error("Error stopping CM", "error", err.Error())
		}
	}

	s.logger.Info("All services stopped successfully")
	return nil
}
