package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	cfg "github.com/dashfabric/fm/pkg/config"
	gm "github.com/dashfabric/fm/pkg/gm"
	dal "github.com/dashfabric/fm/pkg/dal"
)

func main() {
	// Command-line flags
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	port := flag.Int("port", 5051, "gRPC server port")
	restPort := flag.Int("rest-port", 8080, "REST API port")

	flag.Parse()

	// Initialize logger
	logger := NewLogger(*logLevel)
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
	services, err := InitializeServices(ctx, config, logger)
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

// Logger interface (placeholder for structured logging)
type Logger interface {
	Info(msg string, keyvals ...interface{})
	Error(msg string, keyvals ...interface{})
	Debug(msg string, keyvals ...interface{})
}

// SimpleLogger is a basic logger implementation
type SimpleLogger struct {
	level string
}

func NewLogger(level string) Logger {
	return &SimpleLogger{level: level}
}

func (l *SimpleLogger) Info(msg string, keyvals ...interface{}) {
	log.Printf("[INFO] %s %v\n", msg, keyvals)
}

func (l *SimpleLogger) Error(msg string, keyvals ...interface{}) {
	log.Printf("[ERROR] %s %v\n", msg, keyvals)
}

func (l *SimpleLogger) Debug(msg string, keyvals ...interface{}) {
	if l.level == "debug" {
		log.Printf("[DEBUG] %s %v\n", msg, keyvals)
	}
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
	logger    Logger
	gmService gm.GoalStateManager
	dalService dal.DPUAbstractionManager
}

// InitializeServices initializes all FM services using the ServiceFactory
func InitializeServices(ctx context.Context, config *Config, logger Logger) (*Services, error) {
	services := &Services{
		logger: logger,
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

	return services, nil
}

// configLogger adapts the old Logger interface to the new config.Logger interface
type configLogger struct {
	logger Logger
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

// StartRESTServer starts the REST API server (stub implementation)
func (s *Services) StartRESTServer(port int) error {
	s.logger.Info("Starting REST server", "port", port)
	// TODO: Implement REST API server with actual FM endpoints
	return nil
}

// Start starts all services in dependency order
func (s *Services) Start(ctx context.Context) error {
	s.logger.Info("Starting all services...")

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

	// CM and DM are passive services without explicit lifecycle
	s.logger.Info("All services initialized successfully")
	return nil
}

// Shutdown gracefully shuts down all services in reverse order
func (s *Services) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down all services...")

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

	s.logger.Info("All services stopped successfully")
	return nil
}
