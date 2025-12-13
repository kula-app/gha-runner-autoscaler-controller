package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	actionsv1alpha1 "github.com/actions/actions-runner-controller/apis/actions.github.com/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kula-app/gha-runner-autoscaler-controller/internal/config"
	"github.com/kula-app/gha-runner-autoscaler-controller/internal/controller"
	"github.com/kula-app/gha-runner-autoscaler-controller/internal/logging"
)

// The run function is like the main function, except that it takes in operating system fundamentals as arguments, and returns an error.
//
// If the run function finishes without an error, it means the application completed.
// If the run function returns an error, it means the application failed to complete.
//
// The logic of the run function must stay isolated so it can be tested in parallel.
func run(ctx context.Context, args []string, _ func(key string) string, _ *os.File) error {
	// Parse command-line flags
	flags := flag.NewFlagSet(args[0], flag.ExitOnError)
	dryRun := flags.Bool("dry-run", false, "Calculate changes without applying them to the cluster")
	reconcileInterval := flags.Duration("reconcile-interval", 0, "Override reconcile interval (e.g., 30s, 5m)")
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	// Derive a context that is canceled on OS interrupt/termination. This allows
	// us to coordinate a graceful shutdown across goroutines when the process is
	// asked to stop (Ctrl+C, container stop, SIGTERM in production, etc.).
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Create a new logger that can write to both Sentry and terminal when Sentry is enabled.
	// This ensures developers always see logs in their terminal during development,
	// while also capturing important logs in Sentry for production monitoring.
	logger := slog.New(logging.NewTerminalHandler())

	logger.Info("GitHub Actions Runner Autoscaler Controller starting")
	if *dryRun {
		logger.Warn("DRY-RUN MODE ENABLED: Changes will be calculated but not applied to the cluster")
	}

	// Get Kubernetes configuration
	// Try in-cluster config first (for production), fall back to kubeconfig (for local dev)
	cfg, err := rest.InClusterConfig()
	if err != nil {
		logger.Info("not running in cluster, using kubeconfig for local development")
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		configOverrides := &clientcmd.ConfigOverrides{}
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
		cfg, err = kubeConfig.ClientConfig()
		if err != nil {
			return fmt.Errorf("failed to get kubeconfig: %w", err)
		}
	} else {
		logger.Info("running in cluster, using in-cluster configuration")
	}

	// Create a new Kubernetes client
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	// Register the AutoscalingRunnerSet CRD from official ARC
	if err := actionsv1alpha1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to register AutoscalingRunnerSet scheme: %w", err)
	}

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Load controller configuration
	controllerConfig := config.DefaultConfig()
	controllerConfig.DryRun = *dryRun

	// Override reconcile interval if provided
	if *reconcileInterval > 0 {
		controllerConfig.ReconcileInterval = *reconcileInterval
	}

	logger.Info("controller configuration loaded",
		"cpu_buffer_percent", controllerConfig.CPUBufferPercent,
		"memory_buffer_percent", controllerConfig.MemoryBufferPercent,
		"reconcile_interval", controllerConfig.ReconcileInterval,
		"namespaces", controllerConfig.Namespaces,
		"dry_run", controllerConfig.DryRun)

	// Create the reconciler
	reconciler := controller.NewReconciler(k8sClient, logger, controllerConfig)

	// Run the reconciliation loop
	logger.Info("starting reconciliation loop")
	if err := reconciler.Run(ctx); err != nil && err != context.Canceled {
		return fmt.Errorf("reconciliation loop failed: %w", err)
	}

	logger.Info("controller stopped gracefully")
	return nil
}
