package controller

import (
	"testing"

	actionsv1alpha1 "github.com/actions/actions-runner-controller/apis/actions.github.com/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kula-app/gha-runner-autoscaler-controller/internal/config"
)

func TestExtractRunnerSetResources(t *testing.T) {
	tests := []struct {
		name        string
		runnerSet   *actionsv1alpha1.AutoscalingRunnerSet
		want        *RunnerSetResources
		wantErr     bool
		errContains string
	}{
		{
			name: "autoscaling not enabled",
			runnerSet: &actionsv1alpha1.AutoscalingRunnerSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-runner",
					Annotations: map[string]string{
						// Missing or false enabled annotation
					},
				},
			},
			wantErr:     true,
			errContains: "autoscaling not enabled",
		},
		{
			name: "resources from annotations",
			runnerSet: &actionsv1alpha1.AutoscalingRunnerSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-runner",
					Annotations: map[string]string{
						config.AnnotationEnabled:  "true",
						config.AnnotationCPU:      "2000m",
						config.AnnotationMemory:   "4Gi",
						config.AnnotationPriority: "10",
					},
				},
				Spec: actionsv1alpha1.AutoscalingRunnerSetSpec{
					MaxRunners: intPtr(5),
				},
			},
			want: &RunnerSetResources{
				Name:          "test-runner",
				CPUMillis:     2000,
				MemoryBytes:   4 * 1024 * 1024 * 1024,
				Priority:      10,
				CurrentMax:    5,
				ConfiguredMax: 5,
			},
		},
		{
			name: "resources from annotations with raw integers",
			runnerSet: &actionsv1alpha1.AutoscalingRunnerSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-runner",
					Annotations: map[string]string{
						config.AnnotationEnabled:  "true",
						config.AnnotationCPU:      "2000",
						config.AnnotationMemory:   "4294967296",
						config.AnnotationPriority: "5",
					},
				},
				Spec: actionsv1alpha1.AutoscalingRunnerSetSpec{
					MaxRunners: intPtr(10),
				},
			},
			want: &RunnerSetResources{
				Name:          "test-runner",
				CPUMillis:     2000,
				MemoryBytes:   4294967296,
				Priority:      5,
				CurrentMax:    10,
				ConfiguredMax: 10,
			},
		},
		{
			name: "resources from pod template spec",
			runnerSet: &actionsv1alpha1.AutoscalingRunnerSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-runner",
					Annotations: map[string]string{
						config.AnnotationEnabled: "true",
					},
				},
				Spec: actionsv1alpha1.AutoscalingRunnerSetSpec{
					MaxRunners: intPtr(8),
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "runner",
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("1000m"),
											corev1.ResourceMemory: resource.MustParse("2Gi"),
										},
									},
								},
							},
						},
					},
				},
			},
			want: &RunnerSetResources{
				Name:          "test-runner",
				CPUMillis:     1000,
				MemoryBytes:   2 * 1024 * 1024 * 1024,
				Priority:      0, // Default
				CurrentMax:    8,
				ConfiguredMax: 8,
			},
		},
		{
			name: "default priority when not specified",
			runnerSet: &actionsv1alpha1.AutoscalingRunnerSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-runner",
					Annotations: map[string]string{
						config.AnnotationEnabled: "true",
						config.AnnotationCPU:     "1000m",
						config.AnnotationMemory:  "2Gi",
					},
				},
				Spec: actionsv1alpha1.AutoscalingRunnerSetSpec{
					MaxRunners: intPtr(3),
				},
			},
			want: &RunnerSetResources{
				Name:          "test-runner",
				CPUMillis:     1000,
				MemoryBytes:   2 * 1024 * 1024 * 1024,
				Priority:      0,
				CurrentMax:    3,
				ConfiguredMax: 3,
			},
		},
		{
			name: "invalid priority annotation",
			runnerSet: &actionsv1alpha1.AutoscalingRunnerSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-runner",
					Annotations: map[string]string{
						config.AnnotationEnabled:  "true",
						config.AnnotationCPU:      "1000m",
						config.AnnotationMemory:   "2Gi",
						config.AnnotationPriority: "invalid",
					},
				},
			},
			wantErr:     true,
			errContains: "invalid priority annotation",
		},
		{
			name: "invalid CPU annotation",
			runnerSet: &actionsv1alpha1.AutoscalingRunnerSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-runner",
					Annotations: map[string]string{
						config.AnnotationEnabled: "true",
						config.AnnotationCPU:     "invalid",
						config.AnnotationMemory:  "2Gi",
					},
				},
			},
			wantErr:     true,
			errContains: "invalid CPU annotation",
		},
		{
			name: "invalid memory annotation",
			runnerSet: &actionsv1alpha1.AutoscalingRunnerSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-runner",
					Annotations: map[string]string{
						config.AnnotationEnabled: "true",
						config.AnnotationCPU:     "1000m",
						config.AnnotationMemory:  "invalid",
					},
				},
			},
			wantErr:     true,
			errContains: "invalid memory annotation",
		},
		{
			name: "missing CPU in annotation and pod spec",
			runnerSet: &actionsv1alpha1.AutoscalingRunnerSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-runner",
					Annotations: map[string]string{
						config.AnnotationEnabled: "true",
						config.AnnotationMemory:  "2Gi",
					},
				},
				Spec: actionsv1alpha1.AutoscalingRunnerSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "other-container",
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{},
									},
								},
							},
						},
					},
				},
			},
			wantErr:     true,
			errContains: "CPU not specified",
		},
		{
			name: "missing memory in annotation and pod spec",
			runnerSet: &actionsv1alpha1.AutoscalingRunnerSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-runner",
					Annotations: map[string]string{
						config.AnnotationEnabled: "true",
						config.AnnotationCPU:     "1000m",
					},
				},
				Spec: actionsv1alpha1.AutoscalingRunnerSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "runner",
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU: resource.MustParse("1000m"),
										},
									},
								},
							},
						},
					},
				},
			},
			wantErr:     true,
			errContains: "memory not specified",
		},
		{
			name: "nil maxRunners",
			runnerSet: &actionsv1alpha1.AutoscalingRunnerSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-runner",
					Annotations: map[string]string{
						config.AnnotationEnabled: "true",
						config.AnnotationCPU:     "1000m",
						config.AnnotationMemory:  "2Gi",
					},
				},
				Spec: actionsv1alpha1.AutoscalingRunnerSetSpec{
					MaxRunners: nil,
				},
			},
			want: &RunnerSetResources{
				Name:          "test-runner",
				CPUMillis:     1000,
				MemoryBytes:   2 * 1024 * 1024 * 1024,
				Priority:      0,
				CurrentMax:    0,
				ConfiguredMax: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractRunnerSetResources(tt.runnerSet)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ExtractRunnerSetResources() expected error containing %q, got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("ExtractRunnerSetResources() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("ExtractRunnerSetResources() unexpected error = %v", err)
				return
			}

			if got.Name != tt.want.Name {
				t.Errorf("Name = %v, want %v", got.Name, tt.want.Name)
			}
			if got.CPUMillis != tt.want.CPUMillis {
				t.Errorf("CPUMillis = %v, want %v", got.CPUMillis, tt.want.CPUMillis)
			}
			if got.MemoryBytes != tt.want.MemoryBytes {
				t.Errorf("MemoryBytes = %v, want %v", got.MemoryBytes, tt.want.MemoryBytes)
			}
			if got.Priority != tt.want.Priority {
				t.Errorf("Priority = %v, want %v", got.Priority, tt.want.Priority)
			}
			if got.CurrentMax != tt.want.CurrentMax {
				t.Errorf("CurrentMax = %v, want %v", got.CurrentMax, tt.want.CurrentMax)
			}
			if got.ConfiguredMax != tt.want.ConfiguredMax {
				t.Errorf("ConfiguredMax = %v, want %v", got.ConfiguredMax, tt.want.ConfiguredMax)
			}
		})
	}
}

func TestParseResourceQuantityOrInt(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		isCPU   bool
		want    int64
		wantErr bool
	}{
		{
			name:  "CPU raw integer",
			value: "2000",
			isCPU: true,
			want:  2000,
		},
		{
			name:  "CPU with millicores",
			value: "2000m",
			isCPU: true,
			want:  2000,
		},
		{
			name:  "CPU with cores - treated as raw int",
			value: "2",
			isCPU: true,
			want:  2, // "2" is parsed as raw integer first, not as resource quantity
		},
		{
			name:  "Memory raw integer bytes",
			value: "4294967296",
			isCPU: false,
			want:  4294967296,
		},
		{
			name:  "Memory with Gi",
			value: "4Gi",
			isCPU: false,
			want:  4 * 1024 * 1024 * 1024,
		},
		{
			name:  "Memory with Mi",
			value: "512Mi",
			isCPU: false,
			want:  512 * 1024 * 1024,
		},
		{
			name:    "invalid value",
			value:   "invalid",
			isCPU:   true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseResourceQuantityOrInt(tt.value, tt.isCPU)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseResourceQuantityOrInt() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("parseResourceQuantityOrInt() unexpected error = %v", err)
				return
			}

			if got != tt.want {
				t.Errorf("parseResourceQuantityOrInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseCPUFromResource(t *testing.T) {
	tests := []struct {
		name string
		q    resource.Quantity
		want int64
	}{
		{
			name: "millicores",
			q:    resource.MustParse("250m"),
			want: 250,
		},
		{
			name: "whole cores",
			q:    resource.MustParse("2"),
			want: 2000,
		},
		{
			name: "large millicores",
			q:    resource.MustParse("7000m"),
			want: 7000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCPU(tt.q)
			if err != nil {
				t.Errorf("parseCPU() unexpected error = %v", err)
				return
			}

			if got != tt.want {
				t.Errorf("parseCPU() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseMemoryFromResource(t *testing.T) {
	tests := []struct {
		name string
		q    resource.Quantity
		want int64
	}{
		{
			name: "mebibytes",
			q:    resource.MustParse("512Mi"),
			want: 536870912,
		},
		{
			name: "gibibytes",
			q:    resource.MustParse("4Gi"),
			want: 4294967296,
		},
		{
			name: "large gibibytes",
			q:    resource.MustParse("18Gi"),
			want: 19327352832,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMemory(tt.q)
			if err != nil {
				t.Errorf("parseMemory() unexpected error = %v", err)
				return
			}

			if got != tt.want {
				t.Errorf("parseMemory() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Helper functions

func intPtr(i int) *int {
	return &i
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
