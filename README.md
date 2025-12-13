# GitHub Actions Runner Autoscaler Controller

A Kubernetes controller that dynamically adjusts GitHub Actions Runner Controller (ARC) `maxRunners` based on available cluster capacity, preventing resource exhaustion and keeping jobs in GitHub's queue instead of creating pending pods.

## Overview

This controller solves the problem of ARC overcommitting cluster resources when multiple runner scale sets scale up simultaneously. Instead of allowing pods to remain in `Pending` state until they timeout, the controller dynamically adjusts the `maxRunners` value for each `AutoscalingRunnerSet` based on real-time cluster capacity.

### Key Features

- **Annotation-Based Configuration**: Opt-in model with flexible per-runner-set configuration
- **Dynamic Capacity Management**: Automatically calculates available cluster resources (CPU and memory)
- **Priority-Based Allocation**: Configurable priority per runner set (higher priority = allocated first)
- **Safety Checks**: Never scales below currently running runners to protect active jobs
- **Runner Pod Exclusion**: Excludes runner pods from capacity calculations (only counts actual workload)
- **Safety Buffers**: Reserves configurable percentage of capacity to prevent over-allocation
- **Kubernetes Quantity Support**: Use familiar formats like "8Gi", "2000m" in annotations
- **Non-Disruptive**: Works alongside ARC without replacing it
- **Graceful Degradation**: Keeps jobs in GitHub's queue when cluster is at capacity

## How It Works

```
┌─────────────────────────────────────────────────────┐
│       Kubernetes Cluster                            │
│  ┌───────────────────────────────────────────────┐  │
│  │   Runner Autoscaler Controller                │  │
│  │   - Watches nodes, pods                       │  │
│  │   - Calculates available resources            │  │
│  │   - Excludes runner pods from "used"          │  │
│  │   - Respects annotation-based config          │  │
│  │   - Applies priority-based allocation         │  │
│  │   - Protects active runners (safety)          │  │
│  │   - Patches AutoscalingRunnerSet CRDs         │  │
│  └─────────────┬─────────────────────────────────┘  │
│                │ patches maxRunners                 │
│                ▼                                    │
│  ┌───────────────────────────────────────────────┐  │
│  │   AutoscalingRunnerSet CRDs                   │  │
│  │   (with annotations)                          │  │
│  │   - maxRunners adjusted dynamically           │  │
│  │   - ARC respects the new limits               │  │
│  └─────────────┬─────────────────────────────────┘  │
│                │                                    │
└────────────────┼────────────────────────────────────┘
                 │ ARC creates pods up to maxRunners
                 ▼
┌─────────────────────────────────────────────────────┐
│         GitHub Actions Queue                        │
│   Jobs beyond maxRunners wait here (✓)              │
└─────────────────────────────────────────────────────┘
```

### Algorithm

1. **Calculate Total Capacity**: Sum allocatable CPU and memory from all ready nodes
2. **Calculate Current Usage**: Sum resource requests from non-runner pods only
3. **Apply Safety Buffer**: Reserve configurable percentage (default: 10% CPU, 10% memory)
4. **Filter Enabled Runners**: Only process runner sets with opt-in annotation
5. **Extract Resources**: Get CPU/memory from annotations or pod template spec
6. **Sort by Priority**: Higher priority numbers get allocated first
7. **Allocate Capacity**: Distribute remaining capacity respecting priorities and caps
8. **Safety Check**: Never set maxRunners below currently running count
9. **Update maxRunners**: Patch `AutoscalingRunnerSet` CRDs with new values
10. **Repeat**: Run reconciliation loop every 30 seconds (configurable)

## Quick Start

### 1. Enable Autoscaling on Runner Sets

Add annotations to your `AutoscalingRunnerSet` resources:

```bash
# Enable autoscaling with priority and resources
kubectl annotate autoscalingrunnersets my-runners \
  -n github-arc \
  kula.app/gha-runner-autoscaler-enabled=true \
  kula.app/gha-runner-autoscaler-cpu=4 \
  kula.app/gha-runner-autoscaler-memory=12Gi \
  kula.app/gha-runner-autoscaler-priority=400
```

See [ANNOTATIONS.md](./docs/ANNOTATIONS.md) for complete annotation documentation.

### 2. Deploy the Controller

```bash
# Build and run locally (for testing)
make build
./dist/gha-runner-autoscaler-controller --dry-run

# Or run with hot reload during development
make dev-dry-run
```

## Configuration

### Annotation-Based Configuration

Each `AutoscalingRunnerSet` is configured via annotations. See [ANNOTATIONS.md](./docs/ANNOTATIONS.md) for details.

**Required:**

```yaml
kula.app/gha-runner-autoscaler-enabled: "true" # Opt-in to management
```

**Resource Specification** (required, one of):

```yaml
# Option 1: Annotations with Kubernetes quantity format
kula.app/gha-runner-autoscaler-cpu: "4"        # 4 CPUs
kula.app/gha-runner-autoscaler-memory: "12Gi"  # 12 GiB

# Option 2: Pod template spec resources (automatic fallback)
spec.template.spec.containers[runner].resources.requests
```

**Optional:**

```yaml
kula.app/gha-runner-autoscaler-priority: "400" # Higher = allocated first (default: 0)
```

### Global Configuration

Configure via code in `internal/config/config.go`:

```go
&config.Config{
    CPUBufferPercent:    10,                // Reserve 10% of available CPU
    MemoryBufferPercent: 10,                // Reserve 10% of available memory
    ReconcileInterval:   30 * time.Second,  // Reconcile every 30 seconds
    Namespaces:          []string{},        // Empty = all namespaces
    DryRun:              false,             // Set via --dry-run flag
}
```

### CLI Flags

```bash
# Run in dry-run mode (calculate but don't apply changes)
./controller --dry-run

# Override reconcile interval
./controller --reconcile-interval 5s

# Combine flags
./controller --dry-run --reconcile-interval 10s
```

## Safety Features

### 1. Active Runner Protection

The controller **never scales maxRunners below the currently running count**:

```
Current state: 13 runners active, maxRunners=16
Calculated: maxRunners should be 0 (cluster full)
Result: maxRunners set to 13 (protects active jobs)
```

Logs will show:

```
capping maxRunners to current running count (safety)
  calculated_max=0 currently_running=13 new_max=13
```

### 2. Runner Pod Exclusion

Runner pods are **excluded from "used" capacity** calculations, since we're dynamically managing them:

```
Total: 45 CPUs
Used (non-runner): 24.16 CPUs  ← Actual workload
Excluded (runner): 2.6 CPUs    ← Runner pods (not counted)
Available: 18.756 CPUs         ← For allocation
```

### 3. Priority-Based Allocation

Higher priority runner sets get capacity first:

```yaml
# Priority 500 - Gets allocated first
kula.app/gha-runner-autoscaler-priority: "500" # XXL runners

# Priority 400 - Allocated second
kula.app/gha-runner-autoscaler-priority: "400" # XL runners

# Priority 300 - Allocated third
kula.app/gha-runner-autoscaler-priority: "300" # Default runners
```

### 4. Configured Max Caps

The original `spec.maxRunners` value acts as a hard cap:

```yaml
spec:
  maxRunners: 20 # Never exceed this, even if capacity available
```

## Example Configuration

### Complete Example

```yaml
apiVersion: actions.github.com/v1alpha1
kind: AutoscalingRunnerSet
metadata:
  name: k8s-ci-xl
  namespace: github-arc
  annotations:
    # Enable autoscaling
    kula.app/gha-runner-autoscaler-enabled: "true"

    # Resource requirements (Kubernetes quantity format)
    kula.app/gha-runner-autoscaler-cpu: "4"
    kula.app/gha-runner-autoscaler-memory: "12Gi"

    # Allocation priority (higher = first)
    kula.app/gha-runner-autoscaler-priority: "400"
spec:
  maxRunners: 8 # Hard cap (never exceed)
  # ... rest of spec
```

### Multiple Runner Sets with Priorities

```bash
# XXL - Highest priority
kubectl annotate autoscalingrunnersets k8s-ci-xxl \
  -n github-arc \
  kula.app/gha-runner-autoscaler-enabled=true \
  kula.app/gha-runner-autoscaler-cpu=8 \
  kula.app/gha-runner-autoscaler-memory=18Gi \
  kula.app/gha-runner-autoscaler-priority=500

# XL - High priority
kubectl annotate autoscalingrunnersets k8s-ci-xl \
  -n github-arc \
  kula.app/gha-runner-autoscaler-enabled=true \
  kula.app/gha-runner-autoscaler-cpu=4 \
  kula.app/gha-runner-autoscaler-memory=12Gi \
  kula.app/gha-runner-autoscaler-priority=400

# Default - Medium priority
kubectl annotate autoscalingrunnersets k8s-ci-default \
  -n github-arc \
  kula.app/gha-runner-autoscaler-enabled=true \
  kula.app/gha-runner-autoscaler-cpu=2 \
  kula.app/gha-runner-autoscaler-memory=8Gi \
  kula.app/gha-runner-autoscaler-priority=300
```

## Installation

### Prerequisites

- Kubernetes cluster with GitHub Actions Runner Controller (ARC) installed
- `kubectl` configured to access your cluster
- Appropriate RBAC permissions (see below)

### Deploy to Kubernetes

1. **Create RBAC resources**:
   ```yaml
   apiVersion: v1
   kind: ServiceAccount
   metadata:
     name: runner-autoscaler-controller
     namespace: github-arc
   ---
   apiVersion: rbac.authorization.k8s.io/v1
   kind: ClusterRole
   metadata:
     name: runner-autoscaler-controller
   rules:
     # Read cluster capacity
     - apiGroups: [""]
       resources: ["nodes"]
       verbs: ["get", "list", "watch"]

     # Read current workloads
     - apiGroups: [""]
       resources: ["pods"]
       verbs: ["get", "list", "watch"]

     # Read and patch AutoscalingRunnerSets
     - apiGroups: ["actions.github.com"]
       resources: ["autoscalingrunnersets"]
       verbs: ["get", "list", "watch", "patch"]

     # Read runner set status
     - apiGroups: ["actions.github.com"]
       resources: ["autoscalingrunnersets/status"]
       verbs: ["get"]
   ---
   apiVersion: rbac.authorization.k8s.io/v1
   kind: ClusterRoleBinding
   metadata:
     name: runner-autoscaler-controller
   roleRef:
     apiGroup: rbac.authorization.k8s.io
     kind: ClusterRole
     name: runner-autoscaler-controller
   subjects:
     - kind: ServiceAccount
       name: runner-autoscaler-controller
       namespace: github-arc
   ```

2. **Build and push the Docker image**:
   ```bash
   make build-docker
   docker push ghcr.io/kula-app/gha-runner-autoscaler-controller:latest
   ```

3. **Deploy the controller**:
   ```yaml
   apiVersion: apps/v1
   kind: Deployment
   metadata:
     name: runner-autoscaler-controller
     namespace: github-arc
   spec:
     replicas: 1
     selector:
       matchLabels:
         app: runner-autoscaler-controller
     template:
       metadata:
         labels:
           app: runner-autoscaler-controller
       spec:
         serviceAccountName: runner-autoscaler-controller
         containers:
           - name: controller
             image: ghcr.io/kula-app/gha-runner-autoscaler-controller:latest
             resources:
               requests:
                 cpu: 50m
                 memory: 64Mi
               limits:
                 cpu: 200m
                 memory: 256Mi
   ```

## Development

### Local Development

```bash
# Install dependencies
make init

# Run tests
make test

# Format code
make format

# Run locally with dry-run
make run-dry-run

# Run with hot reload (5s interval)
make dev-dry-run
```

### Build Commands

```bash
# Build binary
make build

# Build Docker image
make build-docker

# Run static analysis
make analyze
```

## Monitoring

The controller provides detailed structured logging:

### Capacity Breakdown

```
capacity breakdown
  nodes=3
  pods_counted=63 pods_excluded=26
  excluded_cpu_cores=2.6 excluded_memory_gb=8.75
```

### Capacity Summary

```
cluster capacity calculated
  total_cpu_cores=45 used_cpu_cores=24.16
  available_cpu_cores=18.756 available_memory_gb=72.51
```

### Safety Actions

```
capping maxRunners to current running count (safety)
  name=k8s-ci-default
  calculated_max=0 currently_running=11 new_max=11
```

### Allocation Results

```
[DRY-RUN] would update maxRunners
  name=k8s-ci-xl old_max=4 new_max=1 currently_running=0
```

## Troubleshooting

### Check Annotations

```bash
# View all runner set annotations
kubectl get autoscalingrunnersets -n github-arc -o json | \
  jq -r '.items[] | "\(.metadata.name): enabled=\(.metadata.annotations["kula.app/gha-runner-autoscaler-enabled"] // "not set")"'
```

### Verify RBAC Permissions

```bash
SA="system:serviceaccount:github-arc:runner-autoscaler-controller"
kubectl auth can-i get nodes --as=$SA
kubectl auth can-i list pods --as=$SA
kubectl auth can-i patch autoscalingrunnersets.actions.github.com --as=$SA
```

### Check Controller Logs

```bash
# Follow logs
kubectl logs -f -n github-arc deployment/runner-autoscaler-controller

# Check for errors
kubectl logs -n github-arc deployment/runner-autoscaler-controller | grep ERR

# View capacity calculations
kubectl logs -n github-arc deployment/runner-autoscaler-controller | grep "capacity"
```

### Dry-Run Locally

```bash
# Test allocation without making changes
./dist/gha-runner-autoscaler-controller --dry-run --reconcile-interval 10s
```

## Architecture Decisions

### Why Annotation-Based?

- **Flexibility**: Each runner set can have different resources and priorities
- **No Restart Required**: Change configuration without restarting the controller
- **Self-Documenting**: Configuration lives with the resources
- **Kubernetes-Native**: Follows standard Kubernetes patterns

### Why Exclude Runner Pods?

Runner pods themselves don't consume significant resources until they run jobs. Excluding them allows:

- Accurate capacity calculation based on actual workload
- Dynamic scaling without circular dependencies
- Better utilization during idle periods

### Why Safety Checks?

Prevents disruption:

- Never kills active runners processing jobs
- Maintains SLAs during high load
- Graceful scaling behavior

## Contributing

See [AGENTS.md](./AGENTS.md) for development guidelines and best practices.

### Commit Message Format

This project uses [Conventional Commits 1.0.0](https://www.conventionalcommits.org/):

```
feat(controller): add annotation-based configuration
fix(capacity): exclude runner pods from usage calculation
docs: update README with new annotation system
refactor(allocator): simplify priority-based allocation
```

## License

MIT License. See [LICENSE](./LICENSE) for details.

## Related Projects

- [GitHub Actions Runner Controller (ARC)](https://github.com/actions/actions-runner-controller)
- [Kubernetes](https://kubernetes.io/)
