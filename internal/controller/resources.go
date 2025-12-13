package controller

import (
	"fmt"
	"strconv"

	actionsv1alpha1 "github.com/actions/actions-runner-controller/apis/actions.github.com/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kula-app/gha-runner-autoscaler-controller/internal/config"
)

// RunnerSetResources contains the resource requirements for a runner set
type RunnerSetResources struct {
	Name          string
	CPUMillis     int64
	MemoryBytes   int64
	Priority      int
	CurrentMax    int
	ConfiguredMax int // From original spec, used as cap
}

// ExtractRunnerSetResources extracts resource requirements from a runner set
// It checks annotations first, then falls back to pod template spec resources
func ExtractRunnerSetResources(rs *actionsv1alpha1.AutoscalingRunnerSet) (*RunnerSetResources, error) {
	// Check if autoscaling is enabled via annotation (opt-in)
	if rs.Annotations[config.AnnotationEnabled] != "true" {
		return nil, fmt.Errorf("autoscaling not enabled (missing or false: %s)", config.AnnotationEnabled)
	}

	resources := &RunnerSetResources{
		Name:     rs.Name,
		Priority: 0, // Default priority
	}

	// Get current maxRunners
	if rs.Spec.MaxRunners != nil {
		resources.CurrentMax = *rs.Spec.MaxRunners
		resources.ConfiguredMax = *rs.Spec.MaxRunners // Use as cap
	}

	// Extract priority from annotation
	if priorityStr, ok := rs.Annotations[config.AnnotationPriority]; ok {
		priority, err := strconv.Atoi(priorityStr)
		if err != nil {
			return nil, fmt.Errorf("invalid priority annotation: %w", err)
		}
		resources.Priority = priority
	}

	// Try to get CPU from annotation first
	if cpuStr, ok := rs.Annotations[config.AnnotationCPU]; ok {
		cpu, err := parseResourceQuantityOrInt(cpuStr, true)
		if err != nil {
			return nil, fmt.Errorf("invalid CPU annotation: %w", err)
		}
		resources.CPUMillis = cpu
	} else {
		// Fall back to pod template spec
		cpu, err := extractCPUFromPodSpec(rs)
		if err != nil {
			return nil, fmt.Errorf("CPU not specified in annotation or pod spec: %w", err)
		}
		resources.CPUMillis = cpu
	}

	// Try to get memory from annotation first
	if memStr, ok := rs.Annotations[config.AnnotationMemory]; ok {
		mem, err := parseResourceQuantityOrInt(memStr, false)
		if err != nil {
			return nil, fmt.Errorf("invalid memory annotation: %w", err)
		}
		resources.MemoryBytes = mem
	} else {
		// Fall back to pod template spec
		mem, err := extractMemoryFromPodSpec(rs)
		if err != nil {
			return nil, fmt.Errorf("memory not specified in annotation or pod spec: %w", err)
		}
		resources.MemoryBytes = mem
	}

	return resources, nil
}

// extractCPUFromPodSpec extracts CPU request from the runner container in pod template
func extractCPUFromPodSpec(rs *actionsv1alpha1.AutoscalingRunnerSet) (int64, error) {
	for _, container := range rs.Spec.Template.Spec.Containers {
		if container.Name == "runner" {
			if container.Resources.Requests != nil {
				if cpu, ok := container.Resources.Requests["cpu"]; ok {
					return parseCPU(cpu)
				}
			}
		}
	}
	return 0, fmt.Errorf("no CPU request found in runner container")
}

// extractMemoryFromPodSpec extracts memory request from the runner container in pod template
func extractMemoryFromPodSpec(rs *actionsv1alpha1.AutoscalingRunnerSet) (int64, error) {
	for _, container := range rs.Spec.Template.Spec.Containers {
		if container.Name == "runner" {
			if container.Resources.Requests != nil {
				if mem, ok := container.Resources.Requests["memory"]; ok {
					return parseMemory(mem)
				}
			}
		}
	}
	return 0, fmt.Errorf("no memory request found in runner container")
}

// parseCPU parses a Kubernetes CPU quantity to millicores
func parseCPU(q resource.Quantity) (int64, error) {
	return q.MilliValue(), nil
}

// parseMemory parses a Kubernetes memory quantity to bytes
func parseMemory(q resource.Quantity) (int64, error) {
	return q.Value(), nil
}

// parseResourceQuantityOrInt parses a value that can be either:
// - A Kubernetes resource quantity (e.g., "2000m", "2", "8Gi", "512Mi")
// - A raw integer string (e.g., "2000", "8589934592")
//
// If isCPU is true, returns millicores; otherwise returns bytes
func parseResourceQuantityOrInt(value string, isCPU bool) (int64, error) {
	// Try parsing as raw integer first (for backward compatibility)
	if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
		return intVal, nil
	}

	// Try parsing as Kubernetes resource quantity
	q, err := resource.ParseQuantity(value)
	if err != nil {
		return 0, fmt.Errorf("invalid resource value %q: %w", value, err)
	}

	if isCPU {
		return q.MilliValue(), nil
	}
	return q.Value(), nil
}
