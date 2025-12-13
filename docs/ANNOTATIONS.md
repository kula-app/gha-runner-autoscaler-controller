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
- Higher priority runner sets are allocated resources first
- Runner sets with equal priority are allocated in alphabetical order by name

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

    # Priority (higher = allocated first)
    kula.app/gha-runner-autoscaler-priority: "200"
spec:
  maxRunners: 4
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
  kula.app/gha-runner-autoscaler-priority=200
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

## Priority Guidelines

Recommended priority values based on runner size:

- **XXL runners** (8+ CPUs): `500` - Highest priority
- **XL runners** (4 CPUs): `400`
- **Default/Large runners** (2 CPUs): `300`
- **Small runners** (1 CPU): `200`
- **XS runners** (< 1 CPU): `100` - Lowest priority

This ensures larger, more capable runners are allocated first when capacity is available.

## Example Annotation Script

Enable all your runner sets with clean Kubernetes quantity format:

```bash
# XXL - Highest priority
kubectl annotate autoscalingrunnersets k8s-ci-default-xxl \
  -n github-arc \
  kula.app/gha-runner-autoscaler-enabled=true \
  kula.app/gha-runner-autoscaler-cpu=8 \
  kula.app/gha-runner-autoscaler-memory=18Gi \
  kula.app/gha-runner-autoscaler-priority=500

# XL
kubectl annotate autoscalingrunnersets k8s-ci-default-xl \
  -n github-arc \
  kula.app/gha-runner-autoscaler-enabled=true \
  kula.app/gha-runner-autoscaler-cpu=4 \
  kula.app/gha-runner-autoscaler-memory=12Gi \
  kula.app/gha-runner-autoscaler-priority=400

# Default (2 CPUs)
kubectl annotate autoscalingrunnersets k8s-ci-default \
  -n github-arc \
  kula.app/gha-runner-autoscaler-enabled=true \
  kula.app/gha-runner-autoscaler-cpu=2 \
  kula.app/gha-runner-autoscaler-memory=8Gi \
  kula.app/gha-runner-autoscaler-priority=300

# Small
kubectl annotate autoscalingrunnersets k8s-ci-default-sm \
  -n github-arc \
  kula.app/gha-runner-autoscaler-enabled=true \
  kula.app/gha-runner-autoscaler-cpu=1 \
  kula.app/gha-runner-autoscaler-memory=4Gi \
  kula.app/gha-runner-autoscaler-priority=200

# XS
kubectl annotate autoscalingrunnersets k8s-ci-default-xs \
  -n github-arc \
  kula.app/gha-runner-autoscaler-enabled=true \
  kula.app/gha-runner-autoscaler-cpu=250m \
  kula.app/gha-runner-autoscaler-memory=512Mi \
  kula.app/gha-runner-autoscaler-priority=100
```
