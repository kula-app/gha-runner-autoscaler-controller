# GitHub Actions Runner Autoscaler Controller

A Kubernetes controller that dynamically adjusts GitHub Actions Runner Controller (ARC) `maxRunners` based on available cluster capacity, preventing resource exhaustion and keeping jobs in GitHub's queue instead of creating pending pods.

## Overview

This controller solves the problem of ARC overcommitting cluster resources when multiple runner scale sets scale up simultaneously. Instead of allowing pods to remain in `Pending` state until they timeout after 10 minutes, the controller dynamically adjusts the `maxRunners` value for each `AutoscalingRunnerSet` based on real-time cluster capacity.

### Key Features

- **Dynamic Capacity Management**: Automatically calculates available cluster resources (CPU and memory)
- **Priority-Based Allocation**: Allocates runners by priority (e.g. `xxl` → `xl` → `default` → `sm` → `xs` runners)
- **Safety Buffers**: Reserves configurable percentage of capacity to prevent over-allocation
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
│  │   - Patches AutoscalingRunnerSet CRDs         │  │
│  └─────────────┬─────────────────────────────────┘  │
│                │ patches maxRunners                 │
│                ▼                                    │
│  ┌───────────────────────────────────────────────┐  │
│  │   AutoscalingRunnerSet CRDs                   │  │
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
2. **Calculate Current Usage**: Sum resource requests from all non-terminated pods
3. **Apply Safety Buffer**: Reserve configurable percentage (default: 10% CPU, 10% memory)
4. **Allocate by Priority**: Distribute remaining capacity to runner sets in priority order
5. **Update maxRunners**: Patch `AutoscalingRunnerSet` CRDs with new values
6. **Repeat**: Run reconciliation loop every 30 seconds (configurable)

## Installation

### Prerequisites

- Kubernetes cluster with GitHub Actions Runner Controller (ARC) installed
- `kubectl` configured to access your cluster
- Appropriate RBAC permissions (see below)

### Deploy to Kubernetes

1. **Clone the repository**:
   ```bash
   git clone https://github.com/kula-app/gha-runner-autoscaler-controller
   cd gha-runner-autoscaler-controller
   ```

2. **Build and push the Docker image** (or use pre-built image):
   ```bash
   make build-docker
   docker push ghcr.io/kula-app/gha-runner-autoscaler-controller:latest
   ```

3. **Create RBAC resources**:
   ```yaml
   apiVersion: v1
   kind: ServiceAccount
   metadata:
     name: runner-capacity-controller
     namespace: github-arc
   ---
   apiVersion: rbac.authorization.k8s.io/v1
   kind: ClusterRole
   metadata:
     name: runner-capacity-controller
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
     name: runner-capacity-controller
   roleRef:
     apiGroup: rbac.authorization.k8s.io
     kind: ClusterRole
     name: runner-capacity-controller
   subjects:
     - kind: ServiceAccount
       name: runner-capacity-controller
       namespace: github-arc
   ```

4. **Deploy the controller**:
   ```yaml
   apiVersion: apps/v1
   kind: Deployment
   metadata:
     name: runner-capacity-controller
     namespace: github-arc
   spec:
     replicas: 1
     selector:
       matchLabels:
         app: runner-capacity-controller
     template:
       metadata:
         labels:
           app: runner-capacity-controller
       spec:
         serviceAccountName: runner-capacity-controller
         containers:
           - name: controller
             image: ghcr.io/kula-app/gha-runner-autoscaler-controller:latest
             resources:
               requests:
                 cpu: 50m
                 memory: 64Mi
               limits:
                 cpu: 100m
                 memory: 128Mi
   ```

## Configuration

The controller is configured via code (see `internal/config/config.go`). Default configuration:

```go
&config.Config{
    CPUBufferPercent:    10,              // Reserve 10% of available CPU
    MemoryBufferPercent: 10,              // Reserve 10% of available memory
    ReconcileInterval:   30 * time.Second, // Reconcile every 30 seconds
    Namespace:           "github-arc",     // Watch this namespace
    PriorityOrder:       []string{"xxl", "xl", "default", "sm", "xs"},
    RunnerSpecs: map[string]RunnerSpec{
        "xs":      {CPUMillis: 250,  MemoryBytes: 512Mi,  MaxRunners: 32},
        "sm":      {CPUMillis: 1000, MemoryBytes: 4Gi,    MaxRunners: 16},
        "default": {CPUMillis: 2000, MemoryBytes: 8Gi,    MaxRunners: 8},
        "xl":      {CPUMillis: 4000, MemoryBytes: 14Gi,   MaxRunners: 4},
        "xxl":     {CPUMillis: 8000, MemoryBytes: 18Gi,   MaxRunners: 2},
    },
}
```

## Development

### Local Development

1. **Install dependencies**:
   ```bash
   make init
   ```

2. **Run tests**:
   ```bash
   make test
   ```

3. **Run locally** (requires kubeconfig):
   ```bash
   make run
   ```

4. **Run with hot reload**:
   ```bash
   make dev
   ```

5. **Format code**:
   ```bash
   make format
   ```

6. **Run static analysis**:
   ```bash
   make analyze
   ```

### Running Tests

```bash
# Run all tests
make test

# Run tests with coverage
go test -cover ./...

# Run tests with verbose output
go test -v ./...

# Run specific test
go test -v ./internal/controller -run TestCapacityCalculator_Calculate
```

## Monitoring

The controller logs all decisions using structured logging (slog):

```json
{
  "time": "2025-01-15T10:30:00Z",
  "level": "INFO",
  "msg": "cluster capacity calculated",
  "total_cpu_millis": 45000,
  "total_memory_bytes": 193273528320,
  "used_cpu_millis": 20000,
  "used_memory_bytes": 85899345920,
  "available_cpu_millis": 22500,
  "available_memory_bytes": 96636764160
}
```

```json
{
  "time": "2025-01-15T10:30:00Z",
  "level": "INFO",
  "msg": "updated maxRunners",
  "name": "k8s-ci-default-xl",
  "size": "xl",
  "old_max": 4,
  "new_max": 2
}
```

## Troubleshooting

### Controller not starting

**Check RBAC permissions**:

```bash
kubectl auth can-i get nodes --as=system:serviceaccount:github-arc:runner-capacity-controller
kubectl auth can-i list pods --as=system:serviceaccount:github-arc:runner-capacity-controller
kubectl auth can-i patch autoscalingrunnersets --as=system:serviceaccount:github-arc:runner-capacity-controller
```

### maxRunners not updating

**Check controller logs**:

```bash
kubectl logs -n github-arc deployment/runner-capacity-controller
```

**Verify AutoscalingRunnerSet exists**:

```bash
kubectl get autoscalingrunnersets -n github-arc
```

### Jobs still timing out

**Check if maxRunners is being updated**:

```bash
kubectl get autoscalingrunnersets -n github-arc -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.maxRunners}{"\n"}{end}'
```

**Verify cluster has available capacity**:

```bash
kubectl top nodes
kubectl describe nodes
```

## Contributing

See [AGENTS.md](./AGENTS.md) for development guidelines and best practices.

### Commit Message Format

This project uses [Conventional Commits 1.0.0](https://www.conventionalcommits.org/):

```
feat(controller): add capacity calculation algorithm
fix: resolve race condition in reconciliation loop
docs: update configuration documentation
test: add tests for allocator
```
