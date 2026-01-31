package generators

import (
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/operator-framework/operator-controller/internal/operator-controller/config"
)

func Test_applyCustomConfigToDeployment(t *testing.T) {
	t.Run("nil config does nothing", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "test"},
						},
					},
				},
			},
		}
		original := dep.DeepCopy()
		applyCustomConfigToDeployment(dep, nil)
		require.Equal(t, original, dep)
	})

	t.Run("empty config does nothing", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "test"},
						},
					},
				},
			},
		}
		original := dep.DeepCopy()
		applyCustomConfigToDeployment(dep, &config.DeploymentConfig{})
		require.Equal(t, original, dep)
	})

	t.Run("applies all config fields", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "test",
								Env: []corev1.EnvVar{
									{Name: "EXISTING", Value: "value"},
								},
							},
						},
					},
				},
			},
		}

		config := &config.DeploymentConfig{
			Env: []corev1.EnvVar{
				{Name: "NEW_ENV", Value: "new_value"},
			},
			Tolerations: []corev1.Toleration{
				{Key: "key1", Operator: corev1.TolerationOpEqual, Value: "value1"},
			},
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("100m"),
				},
			},
			NodeSelector: map[string]string{
				"disk": "ssd",
			},
		}

		applyCustomConfigToDeployment(dep, config)

		// Verify env was applied
		require.Len(t, dep.Spec.Template.Spec.Containers[0].Env, 2)
		require.Contains(t, dep.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "NEW_ENV", Value: "new_value"})

		// Verify tolerations were applied
		require.Len(t, dep.Spec.Template.Spec.Tolerations, 1)

		// Verify resources were applied
		require.NotNil(t, dep.Spec.Template.Spec.Containers[0].Resources.Requests)

		// Verify node selector was applied
		require.Equal(t, map[string]string{"disk": "ssd"}, dep.Spec.Template.Spec.NodeSelector)
	})
}

func Test_applyEnvironmentConfig(t *testing.T) {
	t.Run("empty env config does nothing", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "test", Env: []corev1.EnvVar{{Name: "EXISTING", Value: "value"}}},
						},
					},
				},
			},
		}
		original := dep.DeepCopy()
		applyEnvironmentConfig(dep, &config.DeploymentConfig{})
		require.Equal(t, original, dep)
	})

	t.Run("appends new env vars", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "test", Env: []corev1.EnvVar{{Name: "EXISTING", Value: "value"}}},
						},
					},
				},
			},
		}

		config := &config.DeploymentConfig{
			Env: []corev1.EnvVar{
				{Name: "NEW_VAR", Value: "new_value"},
			},
		}

		applyEnvironmentConfig(dep, config)

		require.Len(t, dep.Spec.Template.Spec.Containers[0].Env, 2)
		require.Contains(t, dep.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "EXISTING", Value: "value"})
		require.Contains(t, dep.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "NEW_VAR", Value: "new_value"})
	})

	t.Run("overrides existing env vars with same name", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "test",
								Env: []corev1.EnvVar{
									{Name: "VAR1", Value: "old_value"},
									{Name: "VAR2", Value: "keep_value"},
								},
							},
						},
					},
				},
			},
		}

		config := &config.DeploymentConfig{
			Env: []corev1.EnvVar{
				{Name: "VAR1", Value: "new_value"},
			},
		}

		applyEnvironmentConfig(dep, config)

		require.Len(t, dep.Spec.Template.Spec.Containers[0].Env, 2)
		require.Equal(t, "new_value", dep.Spec.Template.Spec.Containers[0].Env[0].Value)
		require.Equal(t, "keep_value", dep.Spec.Template.Spec.Containers[0].Env[1].Value)
	})

	t.Run("applies to all containers", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "container1"},
							{Name: "container2"},
						},
					},
				},
			},
		}

		config := &config.DeploymentConfig{
			Env: []corev1.EnvVar{
				{Name: "SHARED_VAR", Value: "value"},
			},
		}

		applyEnvironmentConfig(dep, config)

		require.Len(t, dep.Spec.Template.Spec.Containers[0].Env, 1)
		require.Len(t, dep.Spec.Template.Spec.Containers[1].Env, 1)
		require.Equal(t, "SHARED_VAR", dep.Spec.Template.Spec.Containers[0].Env[0].Name)
		require.Equal(t, "SHARED_VAR", dep.Spec.Template.Spec.Containers[1].Env[0].Name)
	})
}

func Test_applyEnvironmentFromConfig(t *testing.T) {
	t.Run("empty envFrom config does nothing", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "test"},
						},
					},
				},
			},
		}
		original := dep.DeepCopy()
		applyEnvironmentFromConfig(dep, &config.DeploymentConfig{})
		require.Equal(t, original, dep)
	})

	t.Run("appends new envFrom sources", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "test"},
						},
					},
				},
			},
		}

		config := &config.DeploymentConfig{
			EnvFrom: []corev1.EnvFromSource{
				{
					ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "config1"},
					},
				},
			},
		}

		applyEnvironmentFromConfig(dep, config)

		require.Len(t, dep.Spec.Template.Spec.Containers[0].EnvFrom, 1)
		require.Equal(t, "config1", dep.Spec.Template.Spec.Containers[0].EnvFrom[0].ConfigMapRef.Name)
	})

	t.Run("does not add duplicate envFrom sources", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "test",
								EnvFrom: []corev1.EnvFromSource{
									{
										ConfigMapRef: &corev1.ConfigMapEnvSource{
											LocalObjectReference: corev1.LocalObjectReference{Name: "config1"},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		config := &config.DeploymentConfig{
			EnvFrom: []corev1.EnvFromSource{
				{
					ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "config1"},
					},
				},
			},
		}

		applyEnvironmentFromConfig(dep, config)

		require.Len(t, dep.Spec.Template.Spec.Containers[0].EnvFrom, 1)
	})

	t.Run("appends different envFrom sources", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "test",
								EnvFrom: []corev1.EnvFromSource{
									{
										ConfigMapRef: &corev1.ConfigMapEnvSource{
											LocalObjectReference: corev1.LocalObjectReference{Name: "config1"},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		config := &config.DeploymentConfig{
			EnvFrom: []corev1.EnvFromSource{
				{
					SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "secret1"},
					},
				},
			},
		}

		applyEnvironmentFromConfig(dep, config)

		require.Len(t, dep.Spec.Template.Spec.Containers[0].EnvFrom, 2)
	})
}

func Test_envFromEquals(t *testing.T) {
	t.Run("equal ConfigMapRef sources", func(t *testing.T) {
		a := corev1.EnvFromSource{
			ConfigMapRef: &corev1.ConfigMapEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "config1"},
			},
		}
		b := corev1.EnvFromSource{
			ConfigMapRef: &corev1.ConfigMapEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "config1"},
			},
		}
		require.True(t, envFromEquals(a, b))
	})

	t.Run("different ConfigMapRef names", func(t *testing.T) {
		a := corev1.EnvFromSource{
			ConfigMapRef: &corev1.ConfigMapEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "config1"},
			},
		}
		b := corev1.EnvFromSource{
			ConfigMapRef: &corev1.ConfigMapEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "config2"},
			},
		}
		require.False(t, envFromEquals(a, b))
	})

	t.Run("different source types", func(t *testing.T) {
		a := corev1.EnvFromSource{
			ConfigMapRef: &corev1.ConfigMapEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "config1"},
			},
		}
		b := corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "secret1"},
			},
		}
		require.False(t, envFromEquals(a, b))
	})

	t.Run("equal SecretRef sources", func(t *testing.T) {
		a := corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "secret1"},
			},
		}
		b := corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "secret1"},
			},
		}
		require.True(t, envFromEquals(a, b))
	})

	t.Run("different prefixes", func(t *testing.T) {
		a := corev1.EnvFromSource{
			Prefix: "prefix1",
			ConfigMapRef: &corev1.ConfigMapEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "config1"},
			},
		}
		b := corev1.EnvFromSource{
			Prefix: "prefix2",
			ConfigMapRef: &corev1.ConfigMapEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "config1"},
			},
		}
		require.False(t, envFromEquals(a, b))
	})
}

func Test_applyVolumeConfig(t *testing.T) {
	t.Run("empty volume config does nothing", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{},
				},
			},
		}
		original := dep.DeepCopy()
		applyVolumeConfig(dep, &config.DeploymentConfig{})
		require.Equal(t, original, dep)
	})

	t.Run("appends volumes", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{
							{Name: "existing-vol"},
						},
					},
				},
			},
		}

		config := &config.DeploymentConfig{
			Volumes: []corev1.Volume{
				{Name: "new-vol"},
			},
		}

		applyVolumeConfig(dep, config)

		require.Len(t, dep.Spec.Template.Spec.Volumes, 2)
		require.Equal(t, "existing-vol", dep.Spec.Template.Spec.Volumes[0].Name)
		require.Equal(t, "new-vol", dep.Spec.Template.Spec.Volumes[1].Name)
	})
}

func Test_applyVolumeMountConfig(t *testing.T) {
	t.Run("empty volumeMount config does nothing", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "test"},
						},
					},
				},
			},
		}
		original := dep.DeepCopy()
		applyVolumeMountConfig(dep, &config.DeploymentConfig{})
		require.Equal(t, original, dep)
	})

	t.Run("appends volume mounts to all containers", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "container1",
								VolumeMounts: []corev1.VolumeMount{
									{Name: "existing", MountPath: "/existing"},
								},
							},
							{Name: "container2"},
						},
					},
				},
			},
		}

		config := &config.DeploymentConfig{
			VolumeMounts: []corev1.VolumeMount{
				{Name: "new-mount", MountPath: "/new"},
			},
		}

		applyVolumeMountConfig(dep, config)

		require.Len(t, dep.Spec.Template.Spec.Containers[0].VolumeMounts, 2)
		require.Len(t, dep.Spec.Template.Spec.Containers[1].VolumeMounts, 1)
		require.Equal(t, "new-mount", dep.Spec.Template.Spec.Containers[0].VolumeMounts[1].Name)
		require.Equal(t, "new-mount", dep.Spec.Template.Spec.Containers[1].VolumeMounts[0].Name)
	})
}

func Test_applyTolerationsConfig(t *testing.T) {
	t.Run("empty tolerations config does nothing", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{},
				},
			},
		}
		original := dep.DeepCopy()
		applyTolerationsConfig(dep, &config.DeploymentConfig{})
		require.Equal(t, original, dep)
	})

	t.Run("appends new tolerations", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{},
				},
			},
		}

		config := &config.DeploymentConfig{
			Tolerations: []corev1.Toleration{
				{Key: "key1", Operator: corev1.TolerationOpEqual, Value: "value1"},
			},
		}

		applyTolerationsConfig(dep, config)

		require.Len(t, dep.Spec.Template.Spec.Tolerations, 1)
		require.Equal(t, "key1", dep.Spec.Template.Spec.Tolerations[0].Key)
	})

	t.Run("does not add duplicate tolerations", func(t *testing.T) {
		toleration := corev1.Toleration{
			Key:      "key1",
			Operator: corev1.TolerationOpEqual,
			Value:    "value1",
			Effect:   corev1.TaintEffectNoSchedule,
		}

		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Tolerations: []corev1.Toleration{toleration},
					},
				},
			},
		}

		config := &config.DeploymentConfig{
			Tolerations: []corev1.Toleration{toleration},
		}

		applyTolerationsConfig(dep, config)

		require.Len(t, dep.Spec.Template.Spec.Tolerations, 1)
	})

	t.Run("appends different tolerations", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Tolerations: []corev1.Toleration{
							{Key: "key1", Operator: corev1.TolerationOpEqual, Value: "value1"},
						},
					},
				},
			},
		}

		config := &config.DeploymentConfig{
			Tolerations: []corev1.Toleration{
				{Key: "key2", Operator: corev1.TolerationOpEqual, Value: "value2"},
			},
		}

		applyTolerationsConfig(dep, config)

		require.Len(t, dep.Spec.Template.Spec.Tolerations, 2)
	})
}

func Test_tolerationEquals(t *testing.T) {
	t.Run("equal tolerations", func(t *testing.T) {
		a := corev1.Toleration{
			Key:      "key1",
			Operator: corev1.TolerationOpEqual,
			Value:    "value1",
			Effect:   corev1.TaintEffectNoSchedule,
		}
		b := corev1.Toleration{
			Key:      "key1",
			Operator: corev1.TolerationOpEqual,
			Value:    "value1",
			Effect:   corev1.TaintEffectNoSchedule,
		}
		require.True(t, tolerationEquals(a, b))
	})

	t.Run("different keys", func(t *testing.T) {
		a := corev1.Toleration{Key: "key1"}
		b := corev1.Toleration{Key: "key2"}
		require.False(t, tolerationEquals(a, b))
	})

	t.Run("different operators", func(t *testing.T) {
		a := corev1.Toleration{Operator: corev1.TolerationOpEqual}
		b := corev1.Toleration{Operator: corev1.TolerationOpExists}
		require.False(t, tolerationEquals(a, b))
	})

	t.Run("different values", func(t *testing.T) {
		a := corev1.Toleration{Value: "value1"}
		b := corev1.Toleration{Value: "value2"}
		require.False(t, tolerationEquals(a, b))
	})

	t.Run("different effects", func(t *testing.T) {
		a := corev1.Toleration{Effect: corev1.TaintEffectNoSchedule}
		b := corev1.Toleration{Effect: corev1.TaintEffectPreferNoSchedule}
		require.False(t, tolerationEquals(a, b))
	})

	t.Run("equal with TolerationSeconds", func(t *testing.T) {
		a := corev1.Toleration{
			Key:               "key1",
			TolerationSeconds: ptr.To(int64(300)),
		}
		b := corev1.Toleration{
			Key:               "key1",
			TolerationSeconds: ptr.To(int64(300)),
		}
		require.True(t, tolerationEquals(a, b))
	})

	t.Run("different TolerationSeconds", func(t *testing.T) {
		a := corev1.Toleration{
			Key:               "key1",
			TolerationSeconds: ptr.To(int64(300)),
		}
		b := corev1.Toleration{
			Key:               "key1",
			TolerationSeconds: ptr.To(int64(600)),
		}
		require.False(t, tolerationEquals(a, b))
	})

	t.Run("one nil TolerationSeconds", func(t *testing.T) {
		a := corev1.Toleration{
			Key:               "key1",
			TolerationSeconds: ptr.To(int64(300)),
		}
		b := corev1.Toleration{
			Key: "key1",
		}
		require.False(t, tolerationEquals(a, b))
	})
}

func Test_applyResourcesConfig(t *testing.T) {
	t.Run("nil resources config does nothing", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "test"},
						},
					},
				},
			},
		}
		original := dep.DeepCopy()
		applyResourcesConfig(dep, &config.DeploymentConfig{})
		require.Equal(t, original, dep)
	})

	t.Run("replaces resources for all containers", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "container1",
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU: resource.MustParse("100m"),
									},
								},
							},
							{
								Name: "container2",
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU: resource.MustParse("200m"),
									},
								},
							},
						},
					},
				},
			},
		}

		newResources := &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1000m"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
		}

		config := &config.DeploymentConfig{
			Resources: newResources,
		}

		applyResourcesConfig(dep, config)

		// Verify both containers have the new resources
		require.Equal(t, *newResources, dep.Spec.Template.Spec.Containers[0].Resources)
		require.Equal(t, *newResources, dep.Spec.Template.Spec.Containers[1].Resources)
	})
}

func Test_applyNodeSelectorConfig(t *testing.T) {
	t.Run("nil nodeSelector config does nothing", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{},
				},
			},
		}
		original := dep.DeepCopy()
		applyNodeSelectorConfig(dep, &config.DeploymentConfig{})
		require.Equal(t, original, dep)
	})

	t.Run("replaces nodeSelector", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						NodeSelector: map[string]string{
							"disk": "hdd",
							"zone": "us-east-1a",
						},
					},
				},
			},
		}

		config := &config.DeploymentConfig{
			NodeSelector: map[string]string{
				"disk": "ssd",
			},
		}

		applyNodeSelectorConfig(dep, config)

		require.Equal(t, map[string]string{"disk": "ssd"}, dep.Spec.Template.Spec.NodeSelector)
	})
}

func Test_applyAffinityConfig(t *testing.T) {
	t.Run("nil affinity config does nothing", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{},
				},
			},
		}
		original := dep.DeepCopy()
		applyAffinityConfig(dep, &config.DeploymentConfig{})
		require.Equal(t, original, dep)
	})

	t.Run("sets affinity when deployment has none", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{},
				},
			},
		}

		nodeAffinity := &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{Key: "key1", Operator: corev1.NodeSelectorOpIn, Values: []string{"value1"}},
						},
					},
				},
			},
		}

		config := &config.DeploymentConfig{
			Affinity: &corev1.Affinity{
				NodeAffinity: nodeAffinity,
			},
		}

		applyAffinityConfig(dep, config)

		require.NotNil(t, dep.Spec.Template.Spec.Affinity)
		require.Equal(t, nodeAffinity, dep.Spec.Template.Spec.Affinity.NodeAffinity)
	})

	t.Run("selectively overrides affinity sub-attributes", func(t *testing.T) {
		existingNodeAffinity := &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{Key: "existing", Operator: corev1.NodeSelectorOpIn, Values: []string{"value"}},
						},
					},
				},
			},
		}

		existingPodAffinity := &corev1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "existing"},
					},
				},
			},
		}

		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Affinity: &corev1.Affinity{
							NodeAffinity: existingNodeAffinity,
							PodAffinity:  existingPodAffinity,
						},
					},
				},
			},
		}

		newNodeAffinity := &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{Key: "new", Operator: corev1.NodeSelectorOpIn, Values: []string{"value"}},
						},
					},
				},
			},
		}

		newPodAntiAffinity := &corev1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "new"},
					},
				},
			},
		}

		config := &config.DeploymentConfig{
			Affinity: &corev1.Affinity{
				NodeAffinity:    newNodeAffinity,
				PodAntiAffinity: newPodAntiAffinity,
			},
		}

		applyAffinityConfig(dep, config)

		// NodeAffinity should be replaced
		require.Equal(t, newNodeAffinity, dep.Spec.Template.Spec.Affinity.NodeAffinity)
		// PodAffinity should remain unchanged
		require.Equal(t, existingPodAffinity, dep.Spec.Template.Spec.Affinity.PodAffinity)
		// PodAntiAffinity should be set
		require.Equal(t, newPodAntiAffinity, dep.Spec.Template.Spec.Affinity.PodAntiAffinity)
	})

	t.Run("does not override with nil sub-attributes", func(t *testing.T) {
		existingNodeAffinity := &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{Key: "existing", Operator: corev1.NodeSelectorOpIn, Values: []string{"value"}},
						},
					},
				},
			},
		}

		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Affinity: &corev1.Affinity{
							NodeAffinity: existingNodeAffinity,
						},
					},
				},
			},
		}

		config := &config.DeploymentConfig{
			Affinity: &corev1.Affinity{
				// NodeAffinity is nil, should not override
				PodAffinity: &corev1.PodAffinity{}, // Non-nil, should be set
			},
		}

		applyAffinityConfig(dep, config)

		// NodeAffinity should remain unchanged
		require.Equal(t, existingNodeAffinity, dep.Spec.Template.Spec.Affinity.NodeAffinity)
		// PodAffinity should be set
		require.NotNil(t, dep.Spec.Template.Spec.Affinity.PodAffinity)
	})
}

func Test_applyAnnotationsConfig(t *testing.T) {
	t.Run("empty annotations config does nothing", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{},
				},
			},
		}
		original := dep.DeepCopy()
		applyAnnotationsConfig(dep, &config.DeploymentConfig{})
		require.Equal(t, original, dep)
	})

	t.Run("adds new annotations to deployment and pod", func(t *testing.T) {
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{},
				},
			},
		}

		config := &config.DeploymentConfig{
			Annotations: map[string]string{
				"new-annotation": "value",
			},
		}

		applyAnnotationsConfig(dep, config)

		require.Equal(t, "value", dep.Annotations["new-annotation"])
		require.Equal(t, "value", dep.Spec.Template.Annotations["new-annotation"])
	})

	t.Run("existing annotations take precedence", func(t *testing.T) {
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"existing-key": "existing-value",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"pod-existing-key": "pod-existing-value",
						},
					},
					Spec: corev1.PodSpec{},
				},
			},
		}

		config := &config.DeploymentConfig{
			Annotations: map[string]string{
				"existing-key":     "config-value",
				"pod-existing-key": "config-value",
				"new-key":          "new-value",
			},
		}

		applyAnnotationsConfig(dep, config)

		// Existing deployment annotation should not be overridden
		require.Equal(t, "existing-value", dep.Annotations["existing-key"])
		// New deployment annotation should be added
		require.Equal(t, "new-value", dep.Annotations["new-key"])
		// Existing pod annotation should not be overridden
		require.Equal(t, "pod-existing-value", dep.Spec.Template.Annotations["pod-existing-key"])
		// New pod annotation should be added
		require.Equal(t, "new-value", dep.Spec.Template.Annotations["new-key"])
	})
}
