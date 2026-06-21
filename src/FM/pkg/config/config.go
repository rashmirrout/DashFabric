package config

import (
	"time"
)

// CMConfig holds configuration for Config Management module
type CMConfig struct {
	CacheSize       int
	CacheTTL        time.Duration
	ControlBrokerURL string
	Topics           []string
	MaxRetries       int
	RetryBackoff     time.Duration
}

// DefaultCMConfig returns sensible defaults for CM
func DefaultCMConfig() CMConfig {
	return CMConfig{
		CacheSize:       10000,
		CacheTTL:        5 * time.Minute,
		ControlBrokerURL: "localhost:2379",
		Topics:           []string{"config"},
		MaxRetries:       3,
		RetryBackoff:     100 * time.Millisecond,
	}
}

// DMConfig holds configuration for Data Management module
type DMConfig struct {
	RegistrySize int
	// Consistency enforcement settings
	StrictMode bool
	EnforceRules bool
}

// DefaultDMConfig returns sensible defaults for DM
func DefaultDMConfig() DMConfig {
	return DMConfig{
		RegistrySize: 100000,
		StrictMode:   true,
		EnforceRules: true,
	}
}

// GMConfig holds configuration for Goal State Management module
type GMConfig struct {
	CacheSize int
	// Aggregation settings
	ParallelFetch bool
	AggregationTimeout time.Duration
	// Generation settings
	GenerationTimeout time.Duration
}

// DefaultGMConfig returns sensible defaults for GM
func DefaultGMConfig() GMConfig {
	return GMConfig{
		CacheSize:          50000,
		ParallelFetch:      true,
		AggregationTimeout: 30 * time.Second,
		GenerationTimeout:  30 * time.Second,
	}
}

// DALConfig holds configuration for DPU Abstraction Layer module
type DALConfig struct {
	PoolWorkers int
	// Vendor configurations
	Vendors map[string]map[string]interface{}
	// Programming timeout
	ProgrammingTimeout time.Duration
}

// DefaultDALConfig returns sensible defaults for DAL
func DefaultDALConfig() DALConfig {
	return DALConfig{
		PoolWorkers:        4,
		Vendors:            make(map[string]map[string]interface{}),
		ProgrammingTimeout: 60 * time.Second,
	}
}

// AppConfig holds all service configurations
type AppConfig struct {
	CM  CMConfig
	DM  DMConfig
	GM  GMConfig
	DAL DALConfig
}

// DefaultAppConfig returns sensible defaults for all modules
func DefaultAppConfig() AppConfig {
	return AppConfig{
		CM:  DefaultCMConfig(),
		DM:  DefaultDMConfig(),
		GM:  DefaultGMConfig(),
		DAL: DefaultDALConfig(),
	}
}
