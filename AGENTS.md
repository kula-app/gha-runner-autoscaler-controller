# Agent Guidelines for GitHub Actions Runner Autoscaler Controller

This document outlines the development patterns, conventions, and best practices used in the GitHub Actions Runner Autoscaler Controller project.

## Table of Contents

1. [Development Workflow](#development-workflow)
2. [Go Best Practices](#go-best-practices)
3. [Kubernetes Controller Patterns](#kubernetes-controller-patterns)
4. [Testing](#testing)
5. [Git and Pull Requests](#git-and-pull-requests)

---

## Development Workflow

### Using the Makefile

**ALWAYS use the Makefile for project commands.** This project includes a comprehensive Makefile with predefined commands for common tasks.

**Why use Makefile commands:**

- Ensures consistent behavior across the team
- Handles complex command sequences and flags automatically
- Provides helpful output and error messages
- Abstracts away tool-specific details
- Reduces errors from typos or incorrect flags

**How to discover available commands:**

- Use `make help` to see all available commands with descriptions
- Commands are organized by topic (setup, building, testing, etc.)

**Examples:**

- Instead of `go test ./...`, use `make test`
- Instead of `go build`, use `make build`
- Instead of `go fmt`, use `make format`

---

## Go Best Practices

### Project Structure

Follow standard Go project layout with clear separation of concerns:

```
project/
├── main.go                 # Application entry point
├── run.go                  # Main run function
├── logging.go              # Logging configuration
├── internal/               # Private application code
│   ├── controller/         # Controller implementation
│   │   ├── reconciler.go   # Main reconciliation logic
│   │   ├── capacity.go     # Capacity calculation
│   │   └── allocator.go    # MaxRunners allocation
│   ├── config/             # Configuration management
│   │   └── config.go       # Configuration structs
│   └── k8s/                # Kubernetes client wrappers
│       └── client.go       # K8s client utilities
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

**Key Principles:**

1. **Flat Package Structure**: Avoid deep nesting
2. **Group Related Files**: Files in a package should be related
3. **Internal Package**: Keep implementation details private
4. **Dependency Direction**: Dependencies should flow inward (main → internal → libraries)

**File Naming:**

- Use snake_case: `capacity_calculator.go`, not `CapacityCalculator.go`
- Group by feature: `reconciler_test.go`, `capacity_test.go`
- Test files: `<filename>_test.go`

**Package Naming:**

- Short, lowercase, single word
- Descriptive of contents
- No plurals (except for common ones like `types`)

Good:

```
internal/controller/
internal/config/
internal/k8s/
```

Bad:

```
internal/controllers/        # Plural
internal/configuration/      # Too long
internal/k8s_client/        # Underscore
```

### Import Organization

Group imports in standard order:

```go
import (
    // 1. Standard library
    "context"
    "fmt"
    "log/slog"
    "time"

    // 2. Kubernetes dependencies
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/rest"

    // 3. Controller runtime
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/manager"

    // 4. Internal packages
    "github.com/kula-app/gha-runner-autoscaler-controller/internal/config"
    "github.com/kula-app/gha-runner-autoscaler-controller/internal/controller"
)
```

### Validation Patterns

**ALWAYS use `github.com/go-playground/validator/v10` for struct validation** instead of custom validation logic.

**Pattern:**

```go
import "github.com/go-playground/validator/v10"

var validate = validator.New()

type Config struct {
    CPUBufferPercent    int    `json:"cpuBufferPercent" validate:"required,min=0,max=100"`
    MemoryBufferPercent int    `json:"memoryBufferPercent" validate:"required,min=0,max=100"`
    ReconcileInterval   string `json:"reconcileInterval" validate:"required"`
}

func ParseConfig(data []byte) (*Config, error) {
    var config Config
    if err := json.Unmarshal(data, &config); err != nil {
        return nil, err
    }

    if err := validate.Struct(&config); err != nil {
        return nil, formatValidationError(err)
    }

    return &config, nil
}
```

**Common Validators:**

- `required` - Field cannot be zero value
- `min=N`, `max=N` - Numeric bounds
- `len=N` - Exact length
- `oneof=a b c` - Must be one of the values

---

## Kubernetes Controller Patterns

### Controller Structure

**Core Components:**

1. **Reconciler**: Main reconciliation loop that watches resources
2. **Capacity Calculator**: Calculates available cluster resources
3. **Allocator**: Determines maxRunners for each scale set

**Reconciliation Pattern:**

```go
type Reconciler struct {
    client   client.Client
    logger   *slog.Logger
    config   *config.Config
}

func (r *Reconciler) Reconcile(ctx context.Context) error {
    // 1. Get cluster capacity
    totalCPU, totalMemory, err := r.getClusterCapacity(ctx)
    if err != nil {
        return fmt.Errorf("failed to get cluster capacity: %w", err)
    }

    // 2. Get current usage
    usedCPU, usedMemory, err := r.getCurrentUsage(ctx)
    if err != nil {
        return fmt.Errorf("failed to get current usage: %w", err)
    }

    // 3. Calculate available capacity
    availableCPU := r.calculateAvailable(totalCPU, usedCPU)
    availableMemory := r.calculateAvailable(totalMemory, usedMemory)

    // 4. Get all runner scale sets
    runnerSets, err := r.listRunnerSets(ctx)
    if err != nil {
        return fmt.Errorf("failed to list runner sets: %w", err)
    }

    // 5. Calculate and apply new maxRunners
    for _, rs := range runnerSets {
        newMaxRunners := r.calculateMaxRunners(rs, availableCPU, availableMemory)
        if rs.Spec.MaxRunners != newMaxRunners {
            if err := r.updateRunnerSet(ctx, rs, newMaxRunners); err != nil {
                r.logger.Error("failed to update runner set",
                    "name", rs.Name,
                    "error", err)
                continue
            }
            r.logger.Info("updated maxRunners",
                "name", rs.Name,
                "old", rs.Spec.MaxRunners,
                "new", newMaxRunners)
        }
    }

    return nil
}
```

### Resource Calculations

**CPU and Memory Handling:**

```go
import "k8s.io/apimachinery/pkg/api/resource"

// Parse CPU resources (supports "250m", "1", "2000m", etc.)
func parseCPU(cpuString string) (int64, error) {
    q, err := resource.ParseQuantity(cpuString)
    if err != nil {
        return 0, err
    }
    return q.MilliValue(), nil // Always use milliCPU
}

// Parse memory resources (supports "512Mi", "4Gi", etc.)
func parseMemory(memString string) (int64, error) {
    q, err := resource.ParseQuantity(memString)
    if err != nil {
        return 0, err
    }
    return q.Value(), nil // Always use bytes
}
```

### Structured Logging

**Use structured logging with context:**

```go
// Info logs
logger.Info("reconciliation started",
    "total_cpu", totalCPU,
    "total_memory", totalMemory)

// Error logs
logger.Error("failed to list runner sets",
    "error", err,
    "namespace", namespace)

// Debug logs (only when enabled)
logger.Debug("checking runner set",
    "name", rs.Name,
    "current_max", rs.Spec.MaxRunners)
```

### Error Handling

**Always wrap errors with context:**

```go
// Bad
return err

// Good
return fmt.Errorf("failed to get nodes: %w", err)

// Better
return fmt.Errorf("failed to get nodes in namespace %s: %w", namespace, err)
```

**Handle partial failures gracefully:**

```go
var errors []error
for _, rs := range runnerSets {
    if err := r.updateRunnerSet(ctx, rs, newMax); err != nil {
        errors = append(errors, fmt.Errorf("runner set %s: %w", rs.Name, err))
        logger.Error("failed to update runner set", "name", rs.Name, "error", err)
        continue // Continue processing other runner sets
    }
}

if len(errors) > 0 {
    logger.Warn("reconciliation completed with errors", "error_count", len(errors))
}
```

### Context Handling

**Always respect context cancellation:**

```go
func (r *Reconciler) Reconcile(ctx context.Context) error {
    ticker := time.NewTicker(r.config.ReconcileInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            r.logger.Info("reconciliation stopped")
            return ctx.Err()
        case <-ticker.C:
            if err := r.reconcileOnce(ctx); err != nil {
                r.logger.Error("reconciliation failed", "error", err)
            }
        }
    }
}
```

---

## Testing

### Unit Testing

**Test Structure:**

```go
func TestCalculateAvailableCapacity(t *testing.T) {
    tests := []struct {
        name           string
        total          int64
        used           int64
        bufferPercent  int
        want           int64
    }{
        {
            name:          "50% usage with 10% buffer",
            total:         1000,
            used:          500,
            bufferPercent: 10,
            want:          450, // (1000 - 500) * 0.9
        },
        {
            name:          "full capacity",
            total:         1000,
            used:          1000,
            bufferPercent: 10,
            want:          0,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := calculateAvailableCapacity(tt.total, tt.used, tt.bufferPercent)
            if got != tt.want {
                t.Errorf("calculateAvailableCapacity() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

### Integration Testing

**Use fake clients for Kubernetes testing:**

```go
import (
    "sigs.k8s.io/controller-runtime/pkg/client/fake"
    "k8s.io/apimachinery/pkg/runtime"
)

func TestReconciler_Reconcile(t *testing.T) {
    scheme := runtime.NewScheme()
    _ = corev1.AddToScheme(scheme)

    fakeClient := fake.NewClientBuilder().
        WithScheme(scheme).
        WithObjects(/* initial objects */).
        Build()

    reconciler := &Reconciler{
        client: fakeClient,
        logger: slog.Default(),
        config: &config.Config{},
    }

    err := reconciler.Reconcile(context.Background())
    if err != nil {
        t.Fatalf("Reconcile() error = %v", err)
    }

    // Verify expected changes
}
```

---

## Git and Pull Requests

### Git Commit Messages

This project uses [Conventional Commits 1.0.0](https://www.conventionalcommits.org/) for all commit messages.

**Commit Message Structure:**

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

**Required Types:**

- `feat:` - A new feature (correlates with MINOR in SemVer)
- `fix:` - A bug fix (correlates with PATCH in SemVer)

**Other Allowed Types:**

- `build:` - Changes to build system or dependencies
- `chore:` - Routine tasks, maintenance
- `ci:` - Changes to CI configuration
- `docs:` - Documentation changes
- `style:` - Code style changes (formatting, missing semi-colons, etc.)
- `refactor:` - Code refactoring without changing functionality
- `perf:` - Performance improvements
- `test:` - Adding or updating tests

**Breaking Changes:**

- Add `!` after type/scope: `feat!:` or `feat(controller)!:`
- Or use footer: `BREAKING CHANGE: description`

**Examples:**

```
feat(controller): add capacity calculation algorithm
fix: resolve race condition in reconciliation loop
docs: update configuration documentation
refactor(allocator): simplify maxRunners calculation
feat!: change configuration format

BREAKING CHANGE: Configuration now uses JSON instead of YAML
```

### No AI References

**NEVER mention AI assistant names (like Claude, ChatGPT, Cursor, etc.) in commit messages or PR descriptions.**

Keep commit messages focused on the technical changes made and their purpose.

**What to avoid:**

- ❌ "Add feature X with Claude's help"
- ❌ "Co-Authored-By: Claude <noreply@anthropic.com>"
- ❌ "Generated with Claude Code"

**Good examples:**

- ✅ "feat: add capacity calculation algorithm"
- ✅ "fix: resolve resource leak in reconciliation loop"
- ✅ "refactor: simplify error handling logic"

### Pull Request Workflow

**Feature Branches from Main:**

- Always branch from `main` for new features
- Create focused PRs with a single, cohesive feature
- After merge, create new branches from updated `main`

**Branch Naming Conventions:**

- `feature/` - New features
- `fix/` - Bug fixes
- `refactor/` - Code refactoring
- `docs/` - Documentation only
- `chore:` - Maintenance tasks

### Good PR Practices

**1. Atomic Commits**

One logical change per commit:

✅ Good:

```bash
git commit -m "feat: add capacity calculator"
git commit -m "feat: add maxRunners allocator"
git commit -m "feat: implement reconciliation loop"
```

❌ Bad:

```bash
git commit -m "Add controller stuff, fix bug, update docs"
```

**2. Self-Review**

Before creating PR:

```bash
# Review your changes
git diff main...HEAD

# Check commit messages
git log main..HEAD --oneline

# Verify build passes
make build

# Run tests
make test

# Check formatter
make format
```

### PR Checklist

Before submitting:

- [ ] Branch name follows convention
- [ ] All changes are staged and committed
- [ ] Commit messages follow Conventional Commits
- [ ] Code builds successfully (`make build`)
- [ ] Tests pass (`make test`)
- [ ] Code is formatted (`make format`)
- [ ] Documentation updated
- [ ] PR title is clear and follows convention
- [ ] No AI references in commits or PR description

---

## Code Quality

### Diagnostics

- Address linter warnings
- Validate all edge cases
- Handle partial failures gracefully

### Security Considerations

- Validate all input from Kubernetes API
- Use RBAC with minimum required permissions
- Handle context cancellation properly
- Log sensitive operations for audit trail

---

## Compaction

After every compaction operation, read this file again to ensure all guidelines are followed and up to date.
