package controller

import (
	"context"
	"fmt"
	"log/slog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CapacityCalculator calculates available cluster capacity
type CapacityCalculator struct {
	client           client.Client
	logger           *slog.Logger
	cpuBufferPercent int
	memBufferPercent int
}

// NewCapacityCalculator creates a new capacity calculator
func NewCapacityCalculator(client client.Client, logger *slog.Logger, cpuBufferPercent, memBufferPercent int) *CapacityCalculator {
	return &CapacityCalculator{
		client:           client,
		logger:           logger,
		cpuBufferPercent: cpuBufferPercent,
		memBufferPercent: memBufferPercent,
	}
}

// ClusterCapacity represents the total cluster capacity
type ClusterCapacity struct {
	TotalCPUMillis       int64
	TotalMemoryBytes     int64
	UsedCPUMillis        int64
	UsedMemoryBytes      int64
	AvailableCPUMillis   int64
	AvailableMemoryBytes int64
}

// Calculate calculates the available cluster capacity with safety buffers
func (c *CapacityCalculator) Calculate(ctx context.Context) (*ClusterCapacity, error) {
	// Get total cluster capacity from nodes
	totalCPU, totalMemory, nodeCount, err := c.getClusterCapacity(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster capacity: %w", err)
	}

	// Get current resource usage from pods
	usedCPU, usedMemory, excludedCPU, excludedMemory, podCount, excludedCount, err := c.getCurrentUsage(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get current usage: %w", err)
	}

	// Log detailed breakdown
	c.logger.Info("capacity breakdown",
		"nodes", nodeCount,
		"pods_counted", podCount,
		"pods_excluded", excludedCount,
		"excluded_cpu_millis", excludedCPU,
		"excluded_cpu_cores", float64(excludedCPU)/1000,
		"excluded_memory_bytes", excludedMemory,
		"excluded_memory_gb", float64(excludedMemory)/(1024*1024*1024))

	// Calculate available capacity with safety buffer
	rawAvailableCPU := max(totalCPU-usedCPU, 0)
	rawAvailableMemory := max(totalMemory-usedMemory, 0)

	// Apply safety buffer
	availableCPU := (rawAvailableCPU * int64(100-c.cpuBufferPercent)) / 100
	availableMemory := (rawAvailableMemory * int64(100-c.memBufferPercent)) / 100

	return &ClusterCapacity{
		TotalCPUMillis:       totalCPU,
		TotalMemoryBytes:     totalMemory,
		UsedCPUMillis:        usedCPU,
		UsedMemoryBytes:      usedMemory,
		AvailableCPUMillis:   availableCPU,
		AvailableMemoryBytes: availableMemory,
	}, nil
}

// getClusterCapacity gets the total allocatable resources from all nodes
func (c *CapacityCalculator) getClusterCapacity(ctx context.Context) (cpuMillis int64, memoryBytes int64, nodeCount int, err error) {
	nodeList := &corev1.NodeList{}
	if err := c.client.List(ctx, nodeList); err != nil {
		return 0, 0, 0, fmt.Errorf("failed to list nodes: %w", err)
	}

	var totalCPUMillis int64
	var totalMemoryBytes int64
	readyNodes := 0

	for _, node := range nodeList.Items {
		// Skip nodes that are not ready
		if !isNodeReady(node) {
			continue
		}
		readyNodes++

		// Get allocatable resources (what can actually be scheduled)
		cpu := node.Status.Allocatable[corev1.ResourceCPU]
		memory := node.Status.Allocatable[corev1.ResourceMemory]

		totalCPUMillis += cpu.MilliValue()
		totalMemoryBytes += memory.Value()
	}

	return totalCPUMillis, totalMemoryBytes, readyNodes, nil
}

// getCurrentUsage gets the current resource usage from all pods except runner pods
// We exclude runner pods because we're dynamically managing their capacity
func (c *CapacityCalculator) getCurrentUsage(ctx context.Context) (cpuMillis, memoryBytes, excludedCPU, excludedMemory int64, podCount, excludedCount int, err error) {
	podList := &corev1.PodList{}
	if err := c.client.List(ctx, podList); err != nil {
		return 0, 0, 0, 0, 0, 0, fmt.Errorf("failed to list pods: %w", err)
	}

	var totalCPUMillis int64
	var totalMemoryBytes int64
	var excludedCPUMillis int64
	var excludedMemoryBytes int64
	countedPods := 0
	excludedPods := 0

	for _, pod := range podList.Items {
		// Skip terminated pods
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}

		// Calculate this pod's resources
		var podCPU int64
		var podMemory int64
		for _, container := range pod.Spec.Containers {
			cpu := container.Resources.Requests[corev1.ResourceCPU]
			memory := container.Resources.Requests[corev1.ResourceMemory]
			podCPU += cpu.MilliValue()
			podMemory += memory.Value()
		}

		// Skip runner pods (they have this label from actions-runner-controller)
		if isRunnerPod(pod) {
			excludedCPUMillis += podCPU
			excludedMemoryBytes += podMemory
			excludedPods++
			continue
		}

		totalCPUMillis += podCPU
		totalMemoryBytes += podMemory
		countedPods++
	}

	return totalCPUMillis, totalMemoryBytes, excludedCPUMillis, excludedMemoryBytes, countedPods, excludedPods, nil
}

// isRunnerPod checks if a pod is a GitHub Actions runner pod
func isRunnerPod(pod corev1.Pod) bool {
	// Check for common runner pod labels from actions-runner-controller
	labels := pod.Labels
	if labels == nil {
		return false
	}

	// Pods managed by AutoscalingRunnerSet have these labels
	if labels["actions.github.com/scale-set-name"] != "" {
		return true
	}

	// Also check for the runner role label
	if labels["app.kubernetes.io/component"] == "runner" {
		return true
	}

	return false
}

// isNodeReady checks if a node is ready to accept pods
func isNodeReady(node corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// ParseCPU parses a CPU quantity string (e.g., "250m", "1", "2000m") to millicores
func ParseCPU(cpuString string) (int64, error) {
	q, err := resource.ParseQuantity(cpuString)
	if err != nil {
		return 0, fmt.Errorf("failed to parse CPU quantity %q: %w", cpuString, err)
	}
	return q.MilliValue(), nil
}

// ParseMemory parses a memory quantity string (e.g., "512Mi", "4Gi") to bytes
func ParseMemory(memString string) (int64, error) {
	q, err := resource.ParseQuantity(memString)
	if err != nil {
		return 0, fmt.Errorf("failed to parse memory quantity %q: %w", memString, err)
	}
	return q.Value(), nil
}
