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
