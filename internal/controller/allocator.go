package controller

import (
	"log/slog"
	"sort"
)

// RunnerSetAllocation represents the calculated maxRunners for a runner set
type RunnerSetAllocation struct {
	Name       string
	MaxRunners int
}

// Allocator calculates maxRunners for each runner set based on available capacity
type Allocator struct {
	logger *slog.Logger
}

// NewAllocator creates a new allocator
func NewAllocator(logger *slog.Logger) *Allocator {
	return &Allocator{
		logger: logger,
	}
}

// Allocate calculates maxRunners for all runner sets based on available capacity
// It respects priority (higher number = higher priority) and ensures we don't exceed available resources
func (a *Allocator) Allocate(runnerSets []*RunnerSetResources, availableCPUMillis, availableMemoryBytes int64) ([]RunnerSetAllocation, error) {
	allocations := make([]RunnerSetAllocation, 0, len(runnerSets))

	// Sort runner sets by priority (higher priority first)
	sortedRunnerSets := make([]*RunnerSetResources, len(runnerSets))
	copy(sortedRunnerSets, runnerSets)
	sort.Slice(sortedRunnerSets, func(i, j int) bool {
		// Higher priority first
		if sortedRunnerSets[i].Priority != sortedRunnerSets[j].Priority {
			return sortedRunnerSets[i].Priority > sortedRunnerSets[j].Priority
		}
		// If priority is equal, sort by name for deterministic behavior
		return sortedRunnerSets[i].Name < sortedRunnerSets[j].Name
	})

	// Track remaining capacity as we allocate
	remainingCPU := availableCPUMillis
	remainingMemory := availableMemoryBytes

	a.logger.Debug("starting allocation",
		"available_cpu_millis", availableCPUMillis,
		"available_memory_bytes", availableMemoryBytes,
		"runner_sets", len(runnerSets))

	// Process runner sets in priority order
	for _, rs := range sortedRunnerSets {
		// Calculate how many runners we can fit
		maxRunners := a.calculateMaxRunners(rs, remainingCPU, remainingMemory)

		// Apply hard cap from configured maxRunners
		if rs.ConfiguredMax > 0 && maxRunners > rs.ConfiguredMax {
			maxRunners = rs.ConfiguredMax
		}

		// Allocate the resources
		allocatedCPU := int64(maxRunners) * rs.CPUMillis
		allocatedMemory := int64(maxRunners) * rs.MemoryBytes

		remainingCPU -= allocatedCPU
		remainingMemory -= allocatedMemory

		a.logger.Debug("allocated runner set",
			"name", rs.Name,
			"priority", rs.Priority,
			"max_runners", maxRunners,
			"cpu_millis", rs.CPUMillis,
			"memory_bytes", rs.MemoryBytes,
			"allocated_cpu_millis", allocatedCPU,
			"allocated_memory_bytes", allocatedMemory,
			"remaining_cpu_millis", remainingCPU,
			"remaining_memory_bytes", remainingMemory)

		allocations = append(allocations, RunnerSetAllocation{
			Name:       rs.Name,
			MaxRunners: maxRunners,
		})
	}

	return allocations, nil
}

// calculateMaxRunners calculates how many runners of a given spec can fit in the available capacity
func (a *Allocator) calculateMaxRunners(rs *RunnerSetResources, availableCPUMillis, availableMemoryBytes int64) int {
	if rs.CPUMillis <= 0 || rs.MemoryBytes <= 0 {
		return 0
	}

	// Calculate how many runners we can fit based on CPU
	maxByCPU := availableCPUMillis / rs.CPUMillis

	// Calculate how many runners we can fit based on memory
	maxByMemory := availableMemoryBytes / rs.MemoryBytes

	// Take the minimum (most constrained resource)
	maxRunners := max(0, min(maxByMemory, maxByCPU))

	return int(maxRunners)
}
