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

		// Enforce minimum runners guarantee
		if rs.MinRunners > 0 && maxRunners < rs.MinRunners {
			maxRunners = rs.MinRunners
		}

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

// AllocateFairShare calculates maxRunners using fair share with priority weights
// Each runner set gets a proportional share of capacity based on its priority weight
// This prevents high-priority runner sets from starving low-priority ones
func (a *Allocator) AllocateFairShare(runnerSets []*RunnerSetResources, availableCPUMillis, availableMemoryBytes int64) ([]RunnerSetAllocation, error) {
	if len(runnerSets) == 0 {
		return []RunnerSetAllocation{}, nil
	}

	// Calculate total priority weight across all runner sets
	totalPriorityWeight := 0
	for _, rs := range runnerSets {
		// If priority is 0, treat it as 1 to avoid division by zero
		priority := rs.Priority
		if priority == 0 {
			priority = 1
		}
		totalPriorityWeight += priority
	}

	a.logger.Debug("starting fair share allocation",
		"available_cpu_millis", availableCPUMillis,
		"available_memory_bytes", availableMemoryBytes,
		"runner_sets", len(runnerSets),
		"total_priority_weight", totalPriorityWeight)

	// First pass: Allocate proportional shares
	type allocation struct {
		runnerSet       *RunnerSetResources
		maxRunners      int
		allocatedCPU    int64
		allocatedMemory int64
		cappedByMax     bool
	}

	allocations := make([]allocation, 0, len(runnerSets))
	totalAllocatedCPU := int64(0)
	totalAllocatedMemory := int64(0)

	for _, rs := range runnerSets {
		priority := rs.Priority
		if priority == 0 {
			priority = 1
		}

		// Calculate this runner set's proportional share of capacity
		cpuShare := (availableCPUMillis * int64(priority)) / int64(totalPriorityWeight)
		memoryShare := (availableMemoryBytes * int64(priority)) / int64(totalPriorityWeight)

		// Calculate how many runners fit in this share
		maxRunners := a.calculateMaxRunners(rs, cpuShare, memoryShare)

		// Check if we're capped by configured max
		cappedByMax := false
		if rs.ConfiguredMax > 0 && maxRunners > rs.ConfiguredMax {
			maxRunners = rs.ConfiguredMax
			cappedByMax = true
		}

		// Calculate actual resource allocation
		allocatedCPU := int64(maxRunners) * rs.CPUMillis
		allocatedMemory := int64(maxRunners) * rs.MemoryBytes

		totalAllocatedCPU += allocatedCPU
		totalAllocatedMemory += allocatedMemory

		a.logger.Debug("fair share allocation (first pass)",
			"name", rs.Name,
			"priority", rs.Priority,
			"priority_weight", priority,
			"cpu_share", cpuShare,
			"memory_share", memoryShare,
			"max_runners", maxRunners,
			"capped_by_max", cappedByMax,
			"allocated_cpu", allocatedCPU,
			"allocated_memory", allocatedMemory)

		allocations = append(allocations, allocation{
			runnerSet:       rs,
			maxRunners:      maxRunners,
			allocatedCPU:    allocatedCPU,
			allocatedMemory: allocatedMemory,
			cappedByMax:     cappedByMax,
		})
	}

	// Enforce minimum runners guarantee
	// This ensures each runner set gets at least its MinRunners, even if fair share calculated less
	for i := range allocations {
		alloc := &allocations[i]
		rs := alloc.runnerSet

		if rs.MinRunners > 0 && alloc.maxRunners < rs.MinRunners {
			// Need to allocate more to meet minimum
			additional := rs.MinRunners - alloc.maxRunners
			additionalCPU := int64(additional) * rs.CPUMillis
			additionalMemory := int64(additional) * rs.MemoryBytes

			alloc.maxRunners = rs.MinRunners
			alloc.allocatedCPU += additionalCPU
			alloc.allocatedMemory += additionalMemory

			totalAllocatedCPU += additionalCPU
			totalAllocatedMemory += additionalMemory

			a.logger.Debug("enforcing minimum runners",
				"name", rs.Name,
				"min_runners", rs.MinRunners,
				"additional_allocated", additional,
				"new_max_runners", alloc.maxRunners)
		}
	}

	// Second pass: Redistribute unused capacity to runner sets that were capped
	// Sort by priority for redistribution (higher priority first)
	remainingCPU := availableCPUMillis - totalAllocatedCPU
	remainingMemory := availableMemoryBytes - totalAllocatedMemory

	if remainingCPU > 0 || remainingMemory > 0 {
		a.logger.Debug("redistributing unused capacity",
			"remaining_cpu", remainingCPU,
			"remaining_memory", remainingMemory)

		// Sort allocations by priority (higher first) for redistribution
		sortedAllocations := make([]allocation, len(allocations))
		copy(sortedAllocations, allocations)
		sort.Slice(sortedAllocations, func(i, j int) bool {
			if sortedAllocations[i].runnerSet.Priority != sortedAllocations[j].runnerSet.Priority {
				return sortedAllocations[i].runnerSet.Priority > sortedAllocations[j].runnerSet.Priority
			}
			return sortedAllocations[i].runnerSet.Name < sortedAllocations[j].runnerSet.Name
		})

		// Try to allocate remaining capacity to runner sets that aren't capped
		for i := range sortedAllocations {
			alloc := &sortedAllocations[i]
			rs := alloc.runnerSet

			// Skip if already at configured max
			if rs.ConfiguredMax > 0 && alloc.maxRunners >= rs.ConfiguredMax {
				continue
			}

			// Calculate how many additional runners we can fit
			additionalRunners := a.calculateMaxRunners(rs, remainingCPU, remainingMemory)
			if additionalRunners == 0 {
				continue
			}

			// Apply configured max cap
			maxAdditional := additionalRunners
			if rs.ConfiguredMax > 0 {
				maxPossible := rs.ConfiguredMax - alloc.maxRunners
				maxAdditional = min(additionalRunners, maxPossible)
			}

			if maxAdditional > 0 {
				additionalCPU := int64(maxAdditional) * rs.CPUMillis
				additionalMemory := int64(maxAdditional) * rs.MemoryBytes

				alloc.maxRunners += maxAdditional
				alloc.allocatedCPU += additionalCPU
				alloc.allocatedMemory += additionalMemory

				remainingCPU -= additionalCPU
				remainingMemory -= additionalMemory

				a.logger.Debug("redistributed capacity",
					"name", rs.Name,
					"additional_runners", maxAdditional,
					"new_max_runners", alloc.maxRunners,
					"remaining_cpu", remainingCPU,
					"remaining_memory", remainingMemory)

				// Update the original allocation
				for j := range allocations {
					if allocations[j].runnerSet.Name == rs.Name {
						allocations[j] = *alloc
						break
					}
				}
			}
		}
	}

	// Convert to RunnerSetAllocation results
	results := make([]RunnerSetAllocation, 0, len(allocations))
	for _, alloc := range allocations {
		results = append(results, RunnerSetAllocation{
			Name:       alloc.runnerSet.Name,
			MaxRunners: alloc.maxRunners,
		})
	}

	return results, nil
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
