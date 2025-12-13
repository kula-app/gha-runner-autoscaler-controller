package config

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg == nil {
		t.Fatal("DefaultConfig() returned nil")
	}

	// Check CPU buffer percent
	if cfg.CPUBufferPercent != 10 {
		t.Errorf("CPUBufferPercent = %v, want 10", cfg.CPUBufferPercent)
	}

	// Check memory buffer percent
	if cfg.MemoryBufferPercent != 10 {
		t.Errorf("MemoryBufferPercent = %v, want 10", cfg.MemoryBufferPercent)
	}

	// Check reconcile interval
	expectedInterval := 30 * time.Second
	if cfg.ReconcileInterval != expectedInterval {
		t.Errorf("ReconcileInterval = %v, want %v", cfg.ReconcileInterval, expectedInterval)
	}

	// Check namespaces
	if cfg.Namespaces == nil {
		t.Error("Namespaces is nil, want empty slice")
	}
	if len(cfg.Namespaces) != 0 {
		t.Errorf("len(Namespaces) = %v, want 0", len(cfg.Namespaces))
	}

	// Check dry run
	if cfg.DryRun != false {
		t.Errorf("DryRun = %v, want false", cfg.DryRun)
	}
}

func TestConfigAnnotations(t *testing.T) {
	// Test that annotation constants are defined
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "AnnotationEnabled",
			value: AnnotationEnabled,
			want:  "kula.app/gha-runner-autoscaler-enabled",
		},
		{
			name:  "AnnotationCPU",
			value: AnnotationCPU,
			want:  "kula.app/gha-runner-autoscaler-cpu",
		},
		{
			name:  "AnnotationMemory",
			value: AnnotationMemory,
			want:  "kula.app/gha-runner-autoscaler-memory",
		},
		{
			name:  "AnnotationPriority",
			value: AnnotationPriority,
			want:  "kula.app/gha-runner-autoscaler-priority",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.value, tt.want)
			}
		})
	}
}
