package controller

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	actionsv1alpha1 "github.com/actions/actions-runner-controller/apis/actions.github.com/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kula-app/gha-runner-autoscaler-controller/internal/config"
)

// Reconciler is the main controller that manages runner capacity
type Reconciler struct {
	client     client.Client
	logger     *slog.Logger
	config     *config.Config
	calculator *CapacityCalculator
	allocator  *Allocator
}

// NewReconciler creates a new reconciler
func NewReconciler(client client.Client, logger *slog.Logger, cfg *config.Config) *Reconciler {
	calculator := NewCapacityCalculator(client, logger, cfg.CPUBufferPercent, cfg.MemoryBufferPercent)
	allocator := NewAllocator(logger)

	return &Reconciler{
		client:     client,
		logger:     logger,
		config:     cfg,
		calculator: calculator,
		allocator:  allocator,
	}
}

// Run starts the reconciliation loop
func (r *Reconciler) Run(ctx context.Context) error {
	r.logger.Info("starting reconciliation loop",
		"interval", r.config.ReconcileInterval,
		"namespaces", r.config.Namespaces,
		"dry_run", r.config.DryRun)

	// Run initial reconciliation immediately
	if err := r.ReconcileOnce(ctx); err != nil {
		r.logger.Error("initial reconciliation failed", "error", err)
	}

	// Start periodic reconciliation
	ticker := time.NewTicker(r.config.ReconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("reconciliation loop stopped")
			return ctx.Err()
		case <-ticker.C:
			if err := r.ReconcileOnce(ctx); err != nil {
				r.logger.Error("reconciliation failed", "error", err)
			}
		}
	}
}

// ReconcileOnce performs a single reconciliation cycle
func (r *Reconciler) ReconcileOnce(ctx context.Context) error {
	startTime := time.Now()
	r.logger.Info("reconciliation started")

	// 1. Calculate available cluster capacity
	capacity, err := r.calculator.Calculate(ctx)
	if err != nil {
		return fmt.Errorf("failed to calculate capacity: %w", err)
	}

	r.logger.Info("cluster capacity calculated",
		"total_cpu_millis", capacity.TotalCPUMillis,
		"total_cpu_cores", float64(capacity.TotalCPUMillis)/1000,
		"total_memory_bytes", capacity.TotalMemoryBytes,
		"total_memory_gb", float64(capacity.TotalMemoryBytes)/(1024*1024*1024),
		"used_cpu_millis", capacity.UsedCPUMillis,
		"used_cpu_cores", float64(capacity.UsedCPUMillis)/1000,
		"used_memory_bytes", capacity.UsedMemoryBytes,
		"used_memory_gb", float64(capacity.UsedMemoryBytes)/(1024*1024*1024),
		"available_cpu_millis", capacity.AvailableCPUMillis,
		"available_cpu_cores", float64(capacity.AvailableCPUMillis)/1000,
		"available_memory_bytes", capacity.AvailableMemoryBytes,
		"available_memory_gb", float64(capacity.AvailableMemoryBytes)/(1024*1024*1024))

	// 2. List all AutoscalingRunnerSets
	runnerSets, err := r.listRunnerSets(ctx)
	if err != nil {
		return fmt.Errorf("failed to list runner sets: %w", err)
	}

	r.logger.Info("runner sets found", "count", len(runnerSets))

	if len(runnerSets) == 0 {
		r.logger.Warn("no runner sets found")
		return nil
	}

	// 3. Extract resource requirements from enabled runner sets
	enabledRunnerSets := make([]*RunnerSetResources, 0, len(runnerSets))
	for i := range runnerSets {
		resources, err := ExtractRunnerSetResources(&runnerSets[i])
		if err != nil {
			r.logger.Debug("skipping runner set",
				"name", runnerSets[i].Name,
				"reason", err.Error())
			continue
		}

		r.logger.Info("runner set enabled for autoscaling",
			"name", resources.Name,
			"cpu_millis", resources.CPUMillis,
			"memory_bytes", resources.MemoryBytes,
			"priority", resources.Priority,
			"configured_max", resources.ConfiguredMax)

		enabledRunnerSets = append(enabledRunnerSets, resources)
	}

	r.logger.Info("enabled runner sets", "count", len(enabledRunnerSets))

	if len(enabledRunnerSets) == 0 {
		r.logger.Warn("no runner sets enabled for autoscaling (missing annotation)")
		return nil
	}

	// 4. Calculate new maxRunners for each runner set
	allocations, err := r.allocator.Allocate(enabledRunnerSets, capacity.AvailableCPUMillis, capacity.AvailableMemoryBytes)
	if err != nil {
		return fmt.Errorf("failed to allocate runners: %w", err)
	}

	// 5. Apply the new maxRunners values
	updatedCount := 0
	for _, alloc := range allocations {
		// Find the corresponding runner set
		var runnerSet *actionsv1alpha1.AutoscalingRunnerSet
		for i := range runnerSets {
			if runnerSets[i].Name == alloc.Name {
				runnerSet = &runnerSets[i]
				break
			}
		}

		if runnerSet == nil {
			r.logger.Warn("runner set not found for allocation", "name", alloc.Name)
			continue
		}

		// Check if we need to update
		currentMax := 0
		if runnerSet.Spec.MaxRunners != nil {
			currentMax = *runnerSet.Spec.MaxRunners
		}

		// Get currently running count from status
		currentlyRunning := runnerSet.Status.CurrentRunners

		// Safety check: never scale below currently running runners
		// This prevents killing active runners that are processing jobs
		newMax := alloc.MaxRunners
		if newMax < currentlyRunning {
			r.logger.Info("capping maxRunners to current running count (safety)",
				"name", alloc.Name,
				"calculated_max", alloc.MaxRunners,
				"currently_running", currentlyRunning,
				"new_max", currentlyRunning)
			newMax = currentlyRunning
		}

		if currentMax == newMax {
			r.logger.Debug("maxRunners unchanged",
				"name", alloc.Name,
				"max_runners", newMax,
				"currently_running", currentlyRunning)
			continue
		}

		// Update the maxRunners
		if r.config.DryRun {
			// In dry-run mode, just log what would have been changed
			r.logger.Warn("[DRY-RUN] would update maxRunners",
				"name", alloc.Name,
				"old_max", currentMax,
				"new_max", newMax,
				"currently_running", currentlyRunning)
			updatedCount++
		} else {
			// Actually update the resource
			if err := r.updateRunnerSet(ctx, runnerSet, newMax); err != nil {
				r.logger.Error("failed to update runner set",
					"name", alloc.Name,
					"error", err)
				continue
			}

			r.logger.Info("updated maxRunners",
				"name", alloc.Name,
				"old_max", currentMax,
				"new_max", newMax,
				"currently_running", currentlyRunning)

			updatedCount++
		}
	}

	elapsed := time.Since(startTime)
	if r.config.DryRun {
		r.logger.Info("reconciliation completed (dry-run)",
			"duration", elapsed,
			"runner_sets_total", len(runnerSets),
			"runner_sets_enabled", len(enabledRunnerSets),
			"runner_sets_would_update", updatedCount)
	} else {
		r.logger.Info("reconciliation completed",
			"duration", elapsed,
			"runner_sets_total", len(runnerSets),
			"runner_sets_enabled", len(enabledRunnerSets),
			"runner_sets_updated", updatedCount)
	}

	return nil
}

// listRunnerSets lists all AutoscalingRunnerSets in the configured namespaces
func (r *Reconciler) listRunnerSets(ctx context.Context) ([]actionsv1alpha1.AutoscalingRunnerSet, error) {
	runnerSetList := &actionsv1alpha1.AutoscalingRunnerSetList{}

	// If no namespaces configured, list from all namespaces
	if len(r.config.Namespaces) == 0 {
		if err := r.client.List(ctx, runnerSetList); err != nil {
			return nil, fmt.Errorf("failed to list AutoscalingRunnerSets: %w", err)
		}
		return runnerSetList.Items, nil
	}

	// Otherwise, list from each configured namespace
	allRunnerSets := []actionsv1alpha1.AutoscalingRunnerSet{}
	for _, namespace := range r.config.Namespaces {
		listOpts := []client.ListOption{client.InNamespace(namespace)}
		if err := r.client.List(ctx, runnerSetList, listOpts...); err != nil {
			r.logger.Warn("failed to list runner sets in namespace",
				"namespace", namespace,
				"error", err)
			continue
		}
		allRunnerSets = append(allRunnerSets, runnerSetList.Items...)
	}

	return allRunnerSets, nil
}

// updateRunnerSet updates the maxRunners value for a runner set
func (r *Reconciler) updateRunnerSet(ctx context.Context, runnerSet *actionsv1alpha1.AutoscalingRunnerSet, newMaxRunners int) error {
	// Create a copy to modify
	updated := runnerSet.DeepCopy()
	updated.Spec.MaxRunners = &newMaxRunners

	// Patch the resource
	if err := r.client.Patch(ctx, updated, client.MergeFrom(runnerSet)); err != nil {
		return fmt.Errorf("failed to patch AutoscalingRunnerSet: %w", err)
	}

	return nil
}
