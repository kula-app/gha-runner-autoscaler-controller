package config

import (
	"time"
)

// Annotation keys used on AutoscalingRunnerSet resources
const (
	// AnnotationEnabled enables autoscaling for this runner set (opt-in)
	AnnotationEnabled = "kula.app/gha-runner-autoscaler-enabled"

	// AnnotationCPU specifies CPU requirements (supports "2000m", "2", or raw millicores)
	AnnotationCPU = "kula.app/gha-runner-autoscaler-cpu"

	// AnnotationMemory specifies memory requirements (supports "8Gi", "512Mi", or raw bytes)
	AnnotationMemory = "kula.app/gha-runner-autoscaler-memory"

	// AnnotationPriority sets allocation priority (higher = allocated first)
	AnnotationPriority = "kula.app/gha-runner-autoscaler-priority"
)

// Config represents the controller configuration
type Config struct {
	// CPUBufferPercent is the percentage of CPU capacity to reserve as buffer (0-100)
	CPUBufferPercent int `json:"cpuBufferPercent" validate:"required,min=0,max=100"`

	// MemoryBufferPercent is the percentage of memory capacity to reserve as buffer (0-100)
	MemoryBufferPercent int `json:"memoryBufferPercent" validate:"required,min=0,max=100"`

	// ReconcileInterval is how often to run the reconciliation loop
	ReconcileInterval time.Duration `json:"reconcileInterval" validate:"required"`

	// Namespaces to watch for AutoscalingRunnerSets (empty slice means all namespaces)
	Namespaces []string `json:"namespaces"`

	// DryRun when enabled will calculate changes but not apply them to the cluster
	DryRun bool `json:"dryRun"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		CPUBufferPercent:    10,
		MemoryBufferPercent: 10,
		ReconcileInterval:   30 * time.Second,
		Namespaces:          []string{}, // Empty means all namespaces
		DryRun:              false,
	}
}
