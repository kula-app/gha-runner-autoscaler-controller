package controller

import (
	"context"
	"log/slog"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCapacityCalculator_Calculate(t *testing.T) {
	tests := []struct {
		name                     string
		nodes                    []corev1.Node
		pods                     []corev1.Pod
		cpuBufferPercent         int
		memBufferPercent         int
		wantAvailableCPUMillis   int64
		wantAvailableMemoryBytes int64
	}{
		{
			name: "single node, no pods, 10% buffer",
			nodes: []corev1.Node{
				makeNode("node1", "10000m", "20Gi", corev1.ConditionTrue),
			},
			pods:                     []corev1.Pod{},
			cpuBufferPercent:         10,
			memBufferPercent:         10,
			wantAvailableCPUMillis:   9000,        // 10000 * 0.9
			wantAvailableMemoryBytes: 19327352832, // 20Gi * 0.9
		},
		{
			name: "single node, one pod, 10% buffer",
			nodes: []corev1.Node{
				makeNode("node1", "10000m", "20Gi", corev1.ConditionTrue),
			},
			pods: []corev1.Pod{
				makePod("pod1", "node1", "2000m", "4Gi", corev1.PodRunning),
			},
			cpuBufferPercent:         10,
			memBufferPercent:         10,
			wantAvailableCPUMillis:   7200,        // (10000 - 2000) * 0.9
			wantAvailableMemoryBytes: 15461882265, // (20Gi - 4Gi) * 0.9
		},
		{
			name: "multiple nodes, multiple pods",
			nodes: []corev1.Node{
				makeNode("node1", "10000m", "20Gi", corev1.ConditionTrue),
				makeNode("node2", "10000m", "20Gi", corev1.ConditionTrue),
			},
			pods: []corev1.Pod{
				makePod("pod1", "node1", "2000m", "4Gi", corev1.PodRunning),
				makePod("pod2", "node2", "3000m", "8Gi", corev1.PodRunning),
			},
			cpuBufferPercent:         10,
			memBufferPercent:         10,
			wantAvailableCPUMillis:   13500,       // (20000 - 5000) * 0.9
			wantAvailableMemoryBytes: 27058293964, // (40Gi - 12Gi) * 0.9 = 28Gi * 0.9 = 25.2Gi
		},
		{
			name: "node not ready should be excluded",
			nodes: []corev1.Node{
				makeNode("node1", "10000m", "20Gi", corev1.ConditionTrue),
				makeNode("node2", "10000m", "20Gi", corev1.ConditionFalse), // Not ready
			},
			pods:                     []corev1.Pod{},
			cpuBufferPercent:         10,
			memBufferPercent:         10,
			wantAvailableCPUMillis:   9000,        // Only node1: 10000 * 0.9
			wantAvailableMemoryBytes: 19327352832, // Only node1: 20Gi * 0.9
		},
		{
			name: "terminated pods should be excluded",
			nodes: []corev1.Node{
				makeNode("node1", "10000m", "20Gi", corev1.ConditionTrue),
			},
			pods: []corev1.Pod{
				makePod("pod1", "node1", "2000m", "4Gi", corev1.PodRunning),
				makePod("pod2", "node1", "1000m", "2Gi", corev1.PodSucceeded), // Terminated
			},
			cpuBufferPercent:         10,
			memBufferPercent:         10,
			wantAvailableCPUMillis:   7200,        // (10000 - 2000) * 0.9, pod2 excluded
			wantAvailableMemoryBytes: 15461882265, // (20Gi - 4Gi) * 0.9, pod2 excluded
		},
		{
			name: "over-committed cluster (negative available)",
			nodes: []corev1.Node{
				makeNode("node1", "10000m", "20Gi", corev1.ConditionTrue),
			},
			pods: []corev1.Pod{
				makePod("pod1", "node1", "12000m", "25Gi", corev1.PodRunning), // Over-requested
			},
			cpuBufferPercent:         10,
			memBufferPercent:         10,
			wantAvailableCPUMillis:   0, // Negative becomes 0
			wantAvailableMemoryBytes: 0, // Negative becomes 0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)

			// Build initial objects
			objs := make([]runtime.Object, 0)
			for i := range tt.nodes {
				objs = append(objs, &tt.nodes[i])
			}
			for i := range tt.pods {
				objs = append(objs, &tt.pods[i])
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objs...).
				Build()

			calculator := NewCapacityCalculator(fakeClient, slog.Default(), tt.cpuBufferPercent, tt.memBufferPercent)

			capacity, err := calculator.Calculate(context.Background())
			if err != nil {
				t.Fatalf("Calculate() error = %v", err)
			}

			if capacity.AvailableCPUMillis != tt.wantAvailableCPUMillis {
				t.Errorf("AvailableCPUMillis = %v, want %v", capacity.AvailableCPUMillis, tt.wantAvailableCPUMillis)
			}

			if capacity.AvailableMemoryBytes != tt.wantAvailableMemoryBytes {
				t.Errorf("AvailableMemoryBytes = %v, want %v", capacity.AvailableMemoryBytes, tt.wantAvailableMemoryBytes)
			}
		})
	}
}

func TestParseCPU(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{
			name:  "millicores",
			input: "250m",
			want:  250,
		},
		{
			name:  "whole cores",
			input: "2",
			want:  2000,
		},
		{
			name:  "large millicores",
			input: "7000m",
			want:  7000,
		},
		{
			name:    "invalid format",
			input:   "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCPU(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseCPU() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseCPU() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseMemory(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{
			name:  "mebibytes",
			input: "512Mi",
			want:  536870912, // 512 * 1024 * 1024
		},
		{
			name:  "gibibytes",
			input: "4Gi",
			want:  4294967296, // 4 * 1024 * 1024 * 1024
		},
		{
			name:  "large gibibytes",
			input: "18Gi",
			want:  19327352832, // 18 * 1024 * 1024 * 1024
		},
		{
			name:    "invalid format",
			input:   "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMemory(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseMemory() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseMemory() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Helper functions

func makeNode(name, cpu, memory string, readyStatus corev1.ConditionStatus) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpu),
				corev1.ResourceMemory: resource.MustParse(memory),
			},
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: readyStatus,
				},
			},
		},
	}
}

func makePod(name, nodeName, cpu, memory string, phase corev1.PodPhase) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{
				{
					Name: "container",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse(cpu),
							corev1.ResourceMemory: resource.MustParse(memory),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: phase,
		},
	}
}

func makePodWithLabels(name, nodeName, cpu, memory string, phase corev1.PodPhase, labels map[string]string) corev1.Pod {
	pod := makePod(name, nodeName, cpu, memory, phase)
	pod.Labels = labels
	return pod
}

func TestCapacityCalculator_CalculateWithRunnerPods(t *testing.T) {
	tests := []struct {
		name                     string
		nodes                    []corev1.Node
		pods                     []corev1.Pod
		cpuBufferPercent         int
		memBufferPercent         int
		wantAvailableCPUMillis   int64
		wantAvailableMemoryBytes int64
	}{
		{
			name: "runner pods should be excluded from usage",
			nodes: []corev1.Node{
				makeNode("node1", "10000m", "20Gi", corev1.ConditionTrue),
			},
			pods: []corev1.Pod{
				makePod("pod1", "node1", "2000m", "4Gi", corev1.PodRunning),
				makePodWithLabels("runner1", "node1", "1000m", "2Gi", corev1.PodRunning, map[string]string{
					"actions.github.com/scale-set-name": "my-runner-set",
				}),
			},
			cpuBufferPercent:         10,
			memBufferPercent:         10,
			wantAvailableCPUMillis:   7200,        // (10000 - 2000) * 0.9, runner excluded
			wantAvailableMemoryBytes: 15461882265, // (20Gi - 4Gi) * 0.9, runner excluded
		},
		{
			name: "runner pods with component label should be excluded",
			nodes: []corev1.Node{
				makeNode("node1", "10000m", "20Gi", corev1.ConditionTrue),
			},
			pods: []corev1.Pod{
				makePod("pod1", "node1", "2000m", "4Gi", corev1.PodRunning),
				makePodWithLabels("runner1", "node1", "1000m", "2Gi", corev1.PodRunning, map[string]string{
					"app.kubernetes.io/component": "runner",
				}),
			},
			cpuBufferPercent:         10,
			memBufferPercent:         10,
			wantAvailableCPUMillis:   7200,        // (10000 - 2000) * 0.9, runner excluded
			wantAvailableMemoryBytes: 15461882265, // (20Gi - 4Gi) * 0.9, runner excluded
		},
		{
			name: "only runner pods - all capacity available",
			nodes: []corev1.Node{
				makeNode("node1", "10000m", "20Gi", corev1.ConditionTrue),
			},
			pods: []corev1.Pod{
				makePodWithLabels("runner1", "node1", "2000m", "4Gi", corev1.PodRunning, map[string]string{
					"actions.github.com/scale-set-name": "my-runner-set",
				}),
			},
			cpuBufferPercent:         10,
			memBufferPercent:         10,
			wantAvailableCPUMillis:   9000,        // 10000 * 0.9, all runners excluded
			wantAvailableMemoryBytes: 19327352832, // 20Gi * 0.9, all runners excluded
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)

			// Build initial objects
			objs := make([]runtime.Object, 0)
			for i := range tt.nodes {
				objs = append(objs, &tt.nodes[i])
			}
			for i := range tt.pods {
				objs = append(objs, &tt.pods[i])
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objs...).
				Build()

			calculator := NewCapacityCalculator(fakeClient, slog.Default(), tt.cpuBufferPercent, tt.memBufferPercent)

			capacity, err := calculator.Calculate(context.Background())
			if err != nil {
				t.Fatalf("Calculate() error = %v", err)
			}

			if capacity.AvailableCPUMillis != tt.wantAvailableCPUMillis {
				t.Errorf("AvailableCPUMillis = %v, want %v", capacity.AvailableCPUMillis, tt.wantAvailableCPUMillis)
			}

			if capacity.AvailableMemoryBytes != tt.wantAvailableMemoryBytes {
				t.Errorf("AvailableMemoryBytes = %v, want %v", capacity.AvailableMemoryBytes, tt.wantAvailableMemoryBytes)
			}
		})
	}
}

func TestIsRunnerPod(t *testing.T) {
	tests := []struct {
		name string
		pod  corev1.Pod
		want bool
	}{
		{
			name: "pod with scale-set-name label is runner",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"actions.github.com/scale-set-name": "my-runner-set",
					},
				},
			},
			want: true,
		},
		{
			name: "pod with component=runner label is runner",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/component": "runner",
					},
				},
			},
			want: true,
		},
		{
			name: "pod with both labels is runner",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"actions.github.com/scale-set-name": "my-runner-set",
						"app.kubernetes.io/component":       "runner",
					},
				},
			},
			want: true,
		},
		{
			name: "pod with empty scale-set-name is not runner",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"actions.github.com/scale-set-name": "",
					},
				},
			},
			want: false,
		},
		{
			name: "pod with component=worker is not runner",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/component": "worker",
					},
				},
			},
			want: false,
		},
		{
			name: "pod without labels is not runner",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: nil,
				},
			},
			want: false,
		},
		{
			name: "pod with empty labels is not runner",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
				},
			},
			want: false,
		},
		{
			name: "pod with other labels is not runner",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "myapp",
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRunnerPod(tt.pod)
			if got != tt.want {
				t.Errorf("isRunnerPod() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsNodeReady(t *testing.T) {
	tests := []struct {
		name string
		node corev1.Node
		want bool
	}{
		{
			name: "node with Ready=True is ready",
			node: corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			want: true,
		},
		{
			name: "node with Ready=False is not ready",
			node: corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			want: false,
		},
		{
			name: "node with Ready=Unknown is not ready",
			node: corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionUnknown,
						},
					},
				},
			},
			want: false,
		},
		{
			name: "node without Ready condition is not ready",
			node: corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{},
				},
			},
			want: false,
		},
		{
			name: "node with multiple conditions including Ready=True",
			node: corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeMemoryPressure,
							Status: corev1.ConditionFalse,
						},
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
						{
							Type:   corev1.NodeDiskPressure,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNodeReady(tt.node)
			if got != tt.want {
				t.Errorf("isNodeReady() = %v, want %v", got, tt.want)
			}
		})
	}
}
