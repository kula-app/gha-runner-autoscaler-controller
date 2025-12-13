# Annotation-Based Configuration

The controller uses annotations on `AutoscalingRunnerSet` resources to determine which runner sets should be managed and how to allocate resources.

## Required Annotations

### Enable Autoscaling (Opt-in)

```yaml
kula.app/gha-runner-autoscaler-enabled: "true"
```

This annotation is **required** for the controller to manage a runner set. Without it, the runner set will be ignored.

## Resource Configuration

The controller needs to know the CPU and memory requirements for each runner. There are two ways to provide this information:

### Option 1: Annotations (Recommended)

Explicitly specify resources via annotations. These support **Kubernetes resource quantity formats**:

```yaml
# Using Kubernetes quantity format (recommended)
kula.app/gha-runner-autoscaler-cpu: "2000m" # or "2" for 2 CPUs
kula.app/gha-runner-autoscaler-memory: "8Gi" # or "8192Mi"
```

You can also use raw numbers:

```yaml
# Using raw numbers (millicores and bytes)
kula.app/gha-runner-autoscaler-cpu: "2000" # 2000 millicores = 2 CPUs
kula.app/gha-runner-autoscaler-memory: "8589934592" # 8Gi in bytes
```

### Option 2: Pod Template Spec

If annotations are not provided, the controller will try to extract resources from the runner container's resource requests in the pod template spec:

```yaml
spec:
  template:
    spec:
      containers:
        - name: runner
          resources:
            requests:
              cpu: "2000m"
              memory: "8Gi"
```

**Note:** Annotations take precedence over pod template spec values.

## Optional Annotations

### Priority

Control allocation priority (higher number = higher priority):

```yaml
kula.app/gha-runner-autoscaler-priority: "100"
```

- Default: `0`
- With fair share allocation, higher priority runner sets get a larger share of capacity
- Runner sets with equal priority are allocated in alphabetical order by name

### Minimum Runners

Guarantee minimum `maxRunners` allocation (does **not** keep pods running):

```yaml
kula.app/gha-runner-autoscaler-min-runners: "2"
```

- Default: `0` (no minimum)
- Ensures the runner set always has at least this many runners available in `maxRunners`
- **Important**: This does NOT keep pods running - use `spec.minRunners` for that
- Useful for ensuring fast scale-up for critical workloads without wasting resources on idle pods
- The autoscaler will guarantee this minimum even when capacity is tight

**Example use case:**

```yaml
spec:
  minRunners: 0 # No idle pods (saves resources)
  maxRunners: 10 # Will be set by autoscaler

annotations:
  kula.app/gha-runner-autoscaler-min-runners: "3" # Guaranteed capacity for 3 runners
```

Result: No pods run when idle, but when jobs arrive, there's guaranteed capacity for up to 3 runners to start immediately.

## Complete Example

Here's a complete example showing how to annotate an `AutoscalingRunnerSet`:

```yaml
apiVersion: actions.github.com/v1alpha1
kind: AutoscalingRunnerSet
metadata:
  name: k8s-ci-default-xl
  namespace: github-arc
  annotations:
    # Enable autoscaling for this runner set
    kula.app/gha-runner-autoscaler-enabled: "true"

    # Resource requirements (Kubernetes quantity format)
    kula.app/gha-runner-autoscaler-cpu: "4" # 4 CPUs
    kula.app/gha-runner-autoscaler-memory: "12Gi" # 12Gi memory

    # Priority (higher priority gets larger share with fair allocation)
    kula.app/gha-runner-autoscaler-priority: "400"

    # Minimum guaranteed maxRunners (doesn't keep pods running)
    kula.app/gha-runner-autoscaler-min-runners: "1"
spec:
  minRunners: 0 # No idle pods
  maxRunners: 4 # Will be updated by autoscaler
  # ... rest of spec
```

## Applying Annotations to Existing Resources

Use `kubectl annotate` to add annotations to existing runner sets:

```bash
# Enable autoscaling with Kubernetes quantity format
kubectl annotate autoscalingrunnersets k8s-ci-default-xl \
  -n github-arc \
  kula.app/gha-runner-autoscaler-enabled=true \
  kula.app/gha-runner-autoscaler-cpu=4 \
  kula.app/gha-runner-autoscaler-memory=12Gi \
  kula.app/gha-runner-autoscaler-priority=400 \
  kula.app/gha-runner-autoscaler-min-runners=1
```

## Supported Resource Formats

### CPU

All of these are equivalent (2 CPUs = 2000 millicores):

- `"2"` - 2 CPUs
- `"2000m"` - 2000 millicores
- `"2000"` - Raw millicores

### Memory

All of these are equivalent (8 GiB):

- `"8Gi"` - 8 Gibibytes
- `"8192Mi"` - 8192 Mebibytes
- `"8589934592"` - Raw bytes

**Supported units:**

- **Binary:** `Ki` (1024), `Mi` (1024²), `Gi` (1024³), `Ti` (1024⁴)
- **Decimal:** `k` (1000), `M` (1000²), `G` (1000³), `T` (1000⁴)
- **Raw bytes:** Plain numbers (e.g., `"8589934592"`)

## Common Resource Values

### Memory

| Format | Value                   |
| ------ | ----------------------- |
| 512Mi  | `512Mi` or `536870912`  |
| 1Gi    | `1Gi` or `1073741824`   |
| 2Gi    | `2Gi` or `2147483648`   |
| 4Gi    | `4Gi` or `4294967296`   |
| 8Gi    | `8Gi` or `8589934592`   |
| 12Gi   | `12Gi` or `12884901888` |
| 16Gi   | `16Gi` or `17179869184` |
| 18Gi   | `18Gi` or `19327352832` |

### CPU

| Format          | Value                    |
| --------------- | ------------------------ |
| 250m (0.25 CPU) | `250m` or `250`          |
| 500m (0.5 CPU)  | `500m` or `500`          |
| 1 CPU           | `1` or `1000m` or `1000` |
| 2 CPUs          | `2` or `2000m` or `2000` |
| 4 CPUs          | `4` or `4000m` or `4000` |
| 8 CPUs          | `8` or `8000m` or `8000` |

## Priority and Minimum Guidelines

### Priority Values

The controller uses **fair share allocation** with priority weights. Higher priority runner sets get a larger proportional share of capacity:

Recommended priority values:

- **XXL runners** (8+ CPUs): `500` - Gets largest share for large workloads
- **XL runners** (4 CPUs): `400`
- **Default/Large runners** (2 CPUs): `300`
- **Small runners** (1 CPU): `200`
- **XS runners** (< 1 CPU): `100`

**How it works:** A runner set with priority 400 gets twice the share of one with priority 200.

### Minimum Runners Guidelines

Set minimums based on how frequently you use each runner size:

- **Frequently used runners** (small/default): Set higher minimums (2-4) to ensure fast scale-up
- **Occasionally used runners** (XL/XXL): Set lower minimums (1) to reserve capacity without waste
- **Rarely used runners**: Consider `min-runners=0` to free capacity for others

**Total capacity check:** Sum all minimum requirements to ensure they fit in your cluster.

## Example Annotation Script

Enable all your runner sets with fair share allocation and minimum guarantees:

```bash
# XXL - Large workloads (occasional use)
kubectl annotate autoscalingrunnersets k8s-ci-default-xxl \
  -n github-arc \
  kula.app/gha-runner-autoscaler-enabled=true \
  kula.app/gha-runner-autoscaler-cpu=8 \
  kula.app/gha-runner-autoscaler-memory=18Gi \
  kula.app/gha-runner-autoscaler-priority=500 \
  kula.app/gha-runner-autoscaler-min-runners=1

# XL - Medium-large workloads
kubectl annotate autoscalingrunnersets k8s-ci-default-xl \
  -n github-arc \
  kula.app/gha-runner-autoscaler-enabled=true \
  kula.app/gha-runner-autoscaler-cpu=4 \
  kula.app/gha-runner-autoscaler-memory=12Gi \
  kula.app/gha-runner-autoscaler-priority=400 \
  kula.app/gha-runner-autoscaler-min-runners=1

# Default - Standard workloads (frequent use)
kubectl annotate autoscalingrunnersets k8s-ci-default \
  -n github-arc \
  kula.app/gha-runner-autoscaler-enabled=true \
  kula.app/gha-runner-autoscaler-cpu=2 \
  kula.app/gha-runner-autoscaler-memory=8Gi \
  kula.app/gha-runner-autoscaler-priority=300 \
  kula.app/gha-runner-autoscaler-min-runners=2

# Small - Light workloads (frequent use)
kubectl annotate autoscalingrunnersets k8s-ci-default-sm \
  -n github-arc \
  kula.app/gha-runner-autoscaler-enabled=true \
  kula.app/gha-runner-autoscaler-cpu=1 \
  kula.app/gha-runner-autoscaler-memory=4Gi \
  kula.app/gha-runner-autoscaler-priority=200 \
  kula.app/gha-runner-autoscaler-min-runners=2

# XS - Very light workloads (very frequent use)
kubectl annotate autoscalingrunnersets k8s-ci-default-xs \
  -n github-arc \
  kula.app/gha-runner-autoscaler-enabled=true \
  kula.app/gha-runner-autoscaler-cpu=250m \
  kula.app/gha-runner-autoscaler-memory=512Mi \
  kula.app/gha-runner-autoscaler-priority=100 \
  kula.app/gha-runner-autoscaler-min-runners=3
```

**Capacity check for this example:**

- Minimums: 1 XXL (8 CPU) + 1 XL (4 CPU) + 2 Default (4 CPU) + 2 Small (2 CPU) + 3 XS (0.75 CPU) = ~19 CPUs
- This fits comfortably in a 48 CPU cluster, leaving 29 CPUs for scaling above minimums
