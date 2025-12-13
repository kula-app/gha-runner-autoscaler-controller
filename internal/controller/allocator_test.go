package controller

import (
	"log/slog"
	"os"
	"testing"
)

func TestAllocator_Allocate(t *testing.T) {
	tests := []struct {
		name                 string
		runnerSets           []*RunnerSetResources
		availableCPUMillis   int64
		availableMemoryBytes int64
		want                 map[string]int // name -> maxRunners
	}{
		{
			name: "sufficient capacity for all runner sets",
			runnerSets: []*RunnerSetResources{
				{Name: "low-priority", CPUMillis: 1000, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 1, ConfiguredMax: 10},
				{Name: "high-priority", CPUMillis: 2000, MemoryBytes: 4 * 1024 * 1024 * 1024, Priority: 10, ConfiguredMax: 5},
				{Name: "medium-priority", CPUMillis: 1000, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 5, ConfiguredMax: 8},
			},
			availableCPUMillis:   20000,                   // 20 CPUs
			availableMemoryBytes: 40 * 1024 * 1024 * 1024, // 40Gi
			want: map[string]int{
				// Priority order: high (10), medium (5), low (1)
				"high-priority":   5, // First: 20000/2000=10, 40Gi/4Gi=10 -> min=10, capped at 5. Uses 10000 CPU, 20Gi
				"medium-priority": 8, // Next: 10000/1000=10, 20Gi/2Gi=10 -> min=10, capped at 8. Uses 8000 CPU, 16Gi
				"low-priority":    2, // Last: 2000/1000=2, 4Gi/2Gi=2 -> min=2. Uses 2000 CPU, 4Gi
			},
		},
		{
			name: "limited capacity - priority matters",
			runnerSets: []*RunnerSetResources{
				{Name: "low-priority", CPUMillis: 2000, MemoryBytes: 4 * 1024 * 1024 * 1024, Priority: 1, ConfiguredMax: 10},
				{Name: "high-priority", CPUMillis: 2000, MemoryBytes: 4 * 1024 * 1024 * 1024, Priority: 10, ConfiguredMax: 10},
			},
			availableCPUMillis:   6000,                    // 6 CPUs
			availableMemoryBytes: 12 * 1024 * 1024 * 1024, // 12Gi
			want: map[string]int{
				"high-priority": 3, // First: 6000/2000=3, 12Gi/4Gi=3 -> min=3. Uses 6000 CPU, 12Gi
				"low-priority":  0, // Next: 0/2000=0, 0Gi/4Gi=0 -> min=0
			},
		},
		{
			name: "memory constrained",
			runnerSets: []*RunnerSetResources{
				{Name: "runner-set", CPUMillis: 1000, MemoryBytes: 8 * 1024 * 1024 * 1024, Priority: 5, ConfiguredMax: 10},
			},
			availableCPUMillis:   16000,                   // 16 CPUs (enough for 16 runners)
			availableMemoryBytes: 16 * 1024 * 1024 * 1024, // 16Gi (only enough for 2 runners)
			want: map[string]int{
				"runner-set": 2, // Memory limited: 16Gi / 8Gi = 2
			},
		},
		{
			name: "CPU constrained",
			runnerSets: []*RunnerSetResources{
				{Name: "runner-set", CPUMillis: 4000, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 5, ConfiguredMax: 10},
			},
			availableCPUMillis:   8000,                     // 8 CPUs (enough for 2 runners)
			availableMemoryBytes: 100 * 1024 * 1024 * 1024, // 100Gi (enough for 50 runners)
			want: map[string]int{
				"runner-set": 2, // CPU limited: 8000 / 4000 = 2
			},
		},
		{
			name: "no capacity available",
			runnerSets: []*RunnerSetResources{
				{Name: "runner-set", CPUMillis: 1000, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 5, ConfiguredMax: 10},
			},
			availableCPUMillis:   0,
			availableMemoryBytes: 0,
			want: map[string]int{
				"runner-set": 0,
			},
		},
		{
			name: "configured max is respected",
			runnerSets: []*RunnerSetResources{
				{Name: "runner-set", CPUMillis: 1000, MemoryBytes: 1 * 1024 * 1024 * 1024, Priority: 5, ConfiguredMax: 3},
			},
			availableCPUMillis:   10000,                   // Can fit 10 runners by CPU
			availableMemoryBytes: 10 * 1024 * 1024 * 1024, // Can fit 10 runners by memory
			want: map[string]int{
				"runner-set": 3, // Capped at ConfiguredMax
			},
		},
		{
			name: "zero configured max is treated as no cap",
			runnerSets: []*RunnerSetResources{
				{Name: "runner-set", CPUMillis: 1000, MemoryBytes: 1 * 1024 * 1024 * 1024, Priority: 5, ConfiguredMax: 0},
			},
			availableCPUMillis:   5000,                    // Can fit 5 runners by CPU
			availableMemoryBytes: 10 * 1024 * 1024 * 1024, // Can fit 10 runners by memory
			want: map[string]int{
				"runner-set": 5, // Not capped, limited by CPU
			},
		},
		{
			name: "multiple runner sets with same priority - alphabetical order",
			runnerSets: []*RunnerSetResources{
				{Name: "zebra", CPUMillis: 1000, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 5, ConfiguredMax: 10},
				{Name: "alpha", CPUMillis: 1000, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 5, ConfiguredMax: 10},
			},
			availableCPUMillis:   3000,                   // 3 CPUs
			availableMemoryBytes: 6 * 1024 * 1024 * 1024, // 6Gi
			want: map[string]int{
				"alpha": 3, // First alphabetically: uses 3000 CPU, 6Gi
				"zebra": 0, // Second: no capacity left
			},
		},
		{
			name: "invalid runner spec - zero CPU",
			runnerSets: []*RunnerSetResources{
				{Name: "runner-set", CPUMillis: 0, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 5, ConfiguredMax: 10},
			},
			availableCPUMillis:   10000,
			availableMemoryBytes: 20 * 1024 * 1024 * 1024,
			want: map[string]int{
				"runner-set": 0, // Invalid spec
			},
		},
		{
			name: "invalid runner spec - zero memory",
			runnerSets: []*RunnerSetResources{
				{Name: "runner-set", CPUMillis: 1000, MemoryBytes: 0, Priority: 5, ConfiguredMax: 10},
			},
			availableCPUMillis:   10000,
			availableMemoryBytes: 20 * 1024 * 1024 * 1024,
			want: map[string]int{
				"runner-set": 0, // Invalid spec
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
			allocator := NewAllocator(logger)

			allocations, err := allocator.Allocate(tt.runnerSets, tt.availableCPUMillis, tt.availableMemoryBytes)
			if err != nil {
				t.Fatalf("Allocate() error = %v", err)
			}

			// Convert allocations to map for easier comparison
			got := make(map[string]int)
			for _, alloc := range allocations {
				got[alloc.Name] = alloc.MaxRunners
			}

			// Check all expected allocations
			for name, wantMax := range tt.want {
				gotMax, ok := got[name]
				if !ok {
					t.Errorf("missing allocation for %s", name)
					continue
				}

				if gotMax != wantMax {
					t.Errorf("allocation for %s = %v, want %v", name, gotMax, wantMax)
				}
			}

			// Check for unexpected allocations
			for name := range got {
				if _, ok := tt.want[name]; !ok {
					t.Errorf("unexpected allocation for %s", name)
				}
			}
		})
	}
}

func TestAllocator_AllocateFairShare(t *testing.T) {
	tests := []struct {
		name                 string
		runnerSets           []*RunnerSetResources
		availableCPUMillis   int64
		availableMemoryBytes int64
		want                 map[string]int // name -> maxRunners
	}{
		{
			name: "fair distribution with different priorities",
			runnerSets: []*RunnerSetResources{
				{Name: "low", CPUMillis: 1000, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 100, ConfiguredMax: 20},
				{Name: "high", CPUMillis: 1000, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 400, ConfiguredMax: 20},
				{Name: "medium", CPUMillis: 1000, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 200, ConfiguredMax: 20},
			},
			availableCPUMillis:   14000,                   // 14 CPUs
			availableMemoryBytes: 28 * 1024 * 1024 * 1024, // 28Gi
			want: map[string]int{
				// Total weight = 100 + 400 + 200 = 700
				// high: 400/700 = 57.1% -> 8 CPUs, 16Gi -> 8 runners
				// medium: 200/700 = 28.6% -> 4 CPUs, 8Gi -> 4 runners
				// low: 100/700 = 14.3% -> 2 CPUs, 4Gi -> 2 runners
				"high":   8,
				"medium": 4,
				"low":    2,
			},
		},
		{
			name: "realistic scenario - small jobs more common",
			runnerSets: []*RunnerSetResources{
				{Name: "xxl", CPUMillis: 7000, MemoryBytes: 24 * 1024 * 1024 * 1024, Priority: 500, ConfiguredMax: 10},
				{Name: "xl", CPUMillis: 4000, MemoryBytes: 16 * 1024 * 1024 * 1024, Priority: 400, ConfiguredMax: 10},
				{Name: "default", CPUMillis: 2000, MemoryBytes: 12 * 1024 * 1024 * 1024, Priority: 300, ConfiguredMax: 10},
				{Name: "small", CPUMillis: 1000, MemoryBytes: 8 * 1024 * 1024 * 1024, Priority: 200, ConfiguredMax: 10},
				{Name: "xs", CPUMillis: 250, MemoryBytes: 1 * 1024 * 1024 * 1024, Priority: 100, ConfiguredMax: 10},
			},
			availableCPUMillis:   48000,                    // 48 CPUs (3 nodes with 16 CPUs each)
			availableMemoryBytes: 192 * 1024 * 1024 * 1024, // 192Gi
			want: map[string]int{
				// First pass fair share:
				// xxl: 2, xl: 3, default: 3, small: 3, xs: 10 (cpu limited in fair share calculation)
				// Redistribution: remaining capacity goes to highest priority (xxl)
				"xxl":     3, // Gets 1 extra from redistribution
				"xl":      3,
				"default": 3,
				"small":   3,
				"xs":      10,
			},
		},
		{
			name: "configured max caps prevent overallocation",
			runnerSets: []*RunnerSetResources{
				{Name: "high", CPUMillis: 1000, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 400, ConfiguredMax: 2},
				{Name: "low", CPUMillis: 1000, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 100, ConfiguredMax: 20},
			},
			availableCPUMillis:   10000,                   // 10 CPUs
			availableMemoryBytes: 20 * 1024 * 1024 * 1024, // 20Gi
			want: map[string]int{
				// Total weight = 400 + 100 = 500
				// high: 400/500 = 80% -> 8 CPUs -> 8 runners BUT capped at 2
				// low: 100/500 = 20% -> 2 CPUs -> 2 runners
				// Redistribution: 6 CPUs remaining -> goes to low (by priority)
				// low gets 2 + 6 = 8 additional runners
				"high": 2,
				"low":  8, // 2 from fair share + 6 from redistribution
			},
		},
		{
			name: "equal priorities distribute equally",
			runnerSets: []*RunnerSetResources{
				{Name: "a", CPUMillis: 1000, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 100, ConfiguredMax: 10},
				{Name: "b", CPUMillis: 1000, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 100, ConfiguredMax: 10},
				{Name: "c", CPUMillis: 1000, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 100, ConfiguredMax: 10},
			},
			availableCPUMillis:   9000,                    // 9 CPUs
			availableMemoryBytes: 18 * 1024 * 1024 * 1024, // 18Gi
			want: map[string]int{
				"a": 3, // Each gets 1/3 = 3 CPUs = 3 runners
				"b": 3,
				"c": 3,
			},
		},
		{
			name: "zero priority treated as 1",
			runnerSets: []*RunnerSetResources{
				{Name: "zero", CPUMillis: 1000, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 0, ConfiguredMax: 10},
				{Name: "one", CPUMillis: 1000, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 1, ConfiguredMax: 10},
			},
			availableCPUMillis:   6000,                    // 6 CPUs
			availableMemoryBytes: 12 * 1024 * 1024 * 1024, // 12Gi
			want: map[string]int{
				// Both treated as priority 1, so equal distribution
				"zero": 3,
				"one":  3,
			},
		},
		{
			name: "no capacity available",
			runnerSets: []*RunnerSetResources{
				{Name: "runner", CPUMillis: 1000, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 100, ConfiguredMax: 10},
			},
			availableCPUMillis:   0,
			availableMemoryBytes: 0,
			want: map[string]int{
				"runner": 0,
			},
		},
		{
			name:                 "empty runner sets",
			runnerSets:           []*RunnerSetResources{},
			availableCPUMillis:   10000,
			availableMemoryBytes: 20 * 1024 * 1024 * 1024,
			want:                 map[string]int{},
		},
		{
			name: "memory constrained fair share",
			runnerSets: []*RunnerSetResources{
				{Name: "high", CPUMillis: 1000, MemoryBytes: 8 * 1024 * 1024 * 1024, Priority: 400, ConfiguredMax: 20},
				{Name: "low", CPUMillis: 1000, MemoryBytes: 8 * 1024 * 1024 * 1024, Priority: 100, ConfiguredMax: 20},
			},
			availableCPUMillis:   50000,                   // 50 CPUs (plenty)
			availableMemoryBytes: 20 * 1024 * 1024 * 1024, // 20Gi (limited)
			want: map[string]int{
				// Total weight = 500
				// high: 400/500 = 80% -> 16Gi -> 2 runners (memory limited)
				// low: 100/500 = 20% -> 4Gi -> 0 runners (memory limited, rounds down)
				// Redistribution: ~4Gi remaining -> goes to low
				"high": 2,
				"low":  0, // Gets 0 from fair share, but 4Gi isn't enough for 1 runner (needs 8Gi)
			},
		},
		{
			name: "minimum runners enforced",
			runnerSets: []*RunnerSetResources{
				{Name: "high", CPUMillis: 2000, MemoryBytes: 4 * 1024 * 1024 * 1024, Priority: 400, MinRunners: 1, ConfiguredMax: 20},
				{Name: "low", CPUMillis: 2000, MemoryBytes: 4 * 1024 * 1024 * 1024, Priority: 100, MinRunners: 2, ConfiguredMax: 20},
			},
			availableCPUMillis:   10000,                   // 10 CPUs
			availableMemoryBytes: 20 * 1024 * 1024 * 1024, // 20Gi
			want: map[string]int{
				// Total weight = 500
				// high: 400/500 = 80% -> 8 CPUs -> 4 runners
				// low: 100/500 = 20% -> 2 CPUs -> 1 runner, but MinRunners=2 enforced
				// After minimums: high=4, low=2 -> uses 12 CPUs (exceeds available!)
				// With minimums enforced: low gets 2 (minimum), high gets 4 from fair share
				"high": 4,
				"low":  2, // Enforced minimum
			},
		},
		{
			name: "minimum runners with tight capacity",
			runnerSets: []*RunnerSetResources{
				{Name: "a", CPUMillis: 1000, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 300, MinRunners: 3, ConfiguredMax: 20},
				{Name: "b", CPUMillis: 1000, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 200, MinRunners: 2, ConfiguredMax: 20},
				{Name: "c", CPUMillis: 1000, MemoryBytes: 2 * 1024 * 1024 * 1024, Priority: 100, MinRunners: 1, ConfiguredMax: 20},
			},
			availableCPUMillis:   8000,                    // 8 CPUs
			availableMemoryBytes: 16 * 1024 * 1024 * 1024, // 16Gi
			want: map[string]int{
				// Total weight = 600
				// a: 300/600 = 50% -> 4 CPUs -> 4 runners, MinRunners=3 (satisfied)
				// b: 200/600 = 33% -> 2.66 CPUs -> 2 runners, MinRunners=2 (satisfied)
				// c: 100/600 = 17% -> 1.33 CPUs -> 1 runner, MinRunners=1 (satisfied)
				// Total: 4+2+1=7 runners = 7 CPUs, 14Gi
				// Remaining: 1 CPU, 2Gi -> goes to highest priority (a) -> a gets 1 more
				"a": 5, // 4 from fair share + 1 from redistribution
				"b": 2,
				"c": 1,
			},
		},
		{
			name: "minimum runners with very tight capacity",
			runnerSets: []*RunnerSetResources{
				{Name: "a", CPUMillis: 2000, MemoryBytes: 4 * 1024 * 1024 * 1024, Priority: 300, MinRunners: 2, ConfiguredMax: 20},
				{Name: "b", CPUMillis: 2000, MemoryBytes: 4 * 1024 * 1024 * 1024, Priority: 200, MinRunners: 2, ConfiguredMax: 20},
			},
			availableCPUMillis:   6000,                    // 6 CPUs
			availableMemoryBytes: 12 * 1024 * 1024 * 1024, // 12Gi
			want: map[string]int{
				// Total weight = 500
				// a: 300/500 = 60% -> 3.6 CPUs -> 1 runner, but MinRunners=2 enforced
				// b: 200/500 = 40% -> 2.4 CPUs -> 1 runner, but MinRunners=2 enforced
				// After minimums: both get 2, uses 8 CPUs (exceeds 6 available!)
				// This shows minimums can exceed capacity - real deployment needs sufficient capacity
				"a": 2, // Minimum enforced (uses 4 CPU, 8Gi)
				"b": 2, // Minimum enforced (uses 4 CPU, 8Gi)
				// Total: 8 CPUs allocated (exceeds 6 available)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
			allocator := NewAllocator(logger)

			allocations, err := allocator.AllocateFairShare(tt.runnerSets, tt.availableCPUMillis, tt.availableMemoryBytes)
			if err != nil {
				t.Fatalf("AllocateFairShare() error = %v", err)
			}

			// Convert allocations to map for easier comparison
			got := make(map[string]int)
			for _, alloc := range allocations {
				got[alloc.Name] = alloc.MaxRunners
			}

			// Check all expected allocations
			for name, wantMax := range tt.want {
				gotMax, ok := got[name]
				if !ok {
					t.Errorf("missing allocation for %s", name)
					continue
				}

				if gotMax != wantMax {
					t.Errorf("allocation for %s = %v, want %v", name, gotMax, wantMax)
				}
			}

			// Check for unexpected allocations
			for name := range got {
				if _, ok := tt.want[name]; !ok {
					t.Errorf("unexpected allocation for %s", name)
				}
			}
		})
	}
}

func TestAllocator_calculateMaxRunners(t *testing.T) {
	tests := []struct {
		name                 string
		rs                   *RunnerSetResources
		availableCPUMillis   int64
		availableMemoryBytes int64
		want                 int
	}{
		{
			name: "CPU constrained",
			rs: &RunnerSetResources{
				Name:        "test",
				CPUMillis:   1000,
				MemoryBytes: 1 * 1024 * 1024 * 1024, // 1Gi
				Priority:    5,
			},
			availableCPUMillis:   5000,                     // Can fit 5 runners by CPU
			availableMemoryBytes: 100 * 1024 * 1024 * 1024, // Can fit 100 runners by memory
			want:                 5,
		},
		{
			name: "memory constrained",
			rs: &RunnerSetResources{
				Name:        "test",
				CPUMillis:   1000,
				MemoryBytes: 4 * 1024 * 1024 * 1024, // 4Gi
				Priority:    5,
			},
			availableCPUMillis:   100000,                 // Can fit 100 runners by CPU
			availableMemoryBytes: 8 * 1024 * 1024 * 1024, // Can fit 2 runners by memory
			want:                 2,
		},
		{
			name: "exactly fits",
			rs: &RunnerSetResources{
				Name:        "test",
				CPUMillis:   2000,
				MemoryBytes: 4 * 1024 * 1024 * 1024,
				Priority:    5,
			},
			availableCPUMillis:   10000,
			availableMemoryBytes: 20 * 1024 * 1024 * 1024,
			want:                 5,
		},
		{
			name: "no capacity",
			rs: &RunnerSetResources{
				Name:        "test",
				CPUMillis:   1000,
				MemoryBytes: 1 * 1024 * 1024 * 1024,
				Priority:    5,
			},
			availableCPUMillis:   0,
			availableMemoryBytes: 0,
			want:                 0,
		},
		{
			name: "invalid spec - zero CPU",
			rs: &RunnerSetResources{
				Name:        "test",
				CPUMillis:   0,
				MemoryBytes: 1 * 1024 * 1024 * 1024,
				Priority:    5,
			},
			availableCPUMillis:   10000,
			availableMemoryBytes: 10 * 1024 * 1024 * 1024,
			want:                 0,
		},
		{
			name: "invalid spec - zero memory",
			rs: &RunnerSetResources{
				Name:        "test",
				CPUMillis:   1000,
				MemoryBytes: 0,
				Priority:    5,
			},
			availableCPUMillis:   10000,
			availableMemoryBytes: 10 * 1024 * 1024 * 1024,
			want:                 0,
		},
		{
			name: "negative CPU should be treated as zero",
			rs: &RunnerSetResources{
				Name:        "test",
				CPUMillis:   1000,
				MemoryBytes: 1 * 1024 * 1024 * 1024,
				Priority:    5,
			},
			availableCPUMillis:   -1000,
			availableMemoryBytes: 10 * 1024 * 1024 * 1024,
			want:                 0,
		},
		{
			name: "negative memory should be treated as zero",
			rs: &RunnerSetResources{
				Name:        "test",
				CPUMillis:   1000,
				MemoryBytes: 1 * 1024 * 1024 * 1024,
				Priority:    5,
			},
			availableCPUMillis:   10000,
			availableMemoryBytes: -1024,
			want:                 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
			allocator := NewAllocator(logger)

			got := allocator.calculateMaxRunners(tt.rs, tt.availableCPUMillis, tt.availableMemoryBytes)
			if got != tt.want {
				t.Errorf("calculateMaxRunners() = %v, want %v", got, tt.want)
			}
		})
	}
}
