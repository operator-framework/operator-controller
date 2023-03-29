package e2e

import (
	"context"
	"fmt"
	"os"

	. "github.com/onsi/gomega"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsscheme "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	yaml2 "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controllerClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

// ListEntities
func createTestNamespace(ctx context.Context, c client.Client, prefix string) string {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: prefix,
		},
	}
	err := c.Create(ctx, ns)
	Expect(err).To(BeNil())
	return ns.Name
}

func deleteTestNamespace(ctx context.Context, c client.Client, name string) {
	err := c.Delete(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	})
	Expect(err).To(BeNil())
}

func createTestServiceAccount(ctx context.Context, c client.Client, namespace, prefix string) string {
	sa := &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: prefix,
			Namespace:    namespace,
		},
	}
	err := c.Create(ctx, sa)
	Expect(err).To(BeNil())
	return sa.Name
}

func createTestRegistryPod(ctx context.Context, cli client.Client, namespace, prefix, serviceAccount string) string {
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: prefix,
			Labels:       map[string]string{"catalogsource": "prometheus-index"},
			Annotations:  nil,
			Namespace:    namespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "registry",
				// TODO: switch to using locally built and loaded images to avoid flakes
				Image: "quay.io/ankitathomas/index:quay",
				Ports: []corev1.ContainerPort{
					{
						Name:          "grpc",
						ContainerPort: 50051,
					},
				},
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						Exec: &corev1.ExecAction{
							Command: []string{"grpc_health_probe", "-addr=:50051"},
						},
					},
					InitialDelaySeconds: 5,
					TimeoutSeconds:      5,
				},
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						Exec: &corev1.ExecAction{
							Command: []string{"grpc_health_probe", "-addr=:50051"},
						},
					},
					InitialDelaySeconds: 10,
					TimeoutSeconds:      5,
				},
				StartupProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						Exec: &corev1.ExecAction{
							Command: []string{"grpc_health_probe", "-addr=:50051"},
						},
					},
					FailureThreshold: 15,
					PeriodSeconds:    10,
				},
				ImagePullPolicy:          corev1.PullAlways,
				TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
			}},
			ServiceAccountName: serviceAccount,
		}}
	err := cli.Create(ctx, pod)
	Expect(err).To(BeNil())

	currentPod := corev1.Pod{}
	Eventually(func() (bool, error) {
		err := cli.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: namespace}, &currentPod)
		if err != nil {
			return false, err
		}
		if len(currentPod.Status.ContainerStatuses) == 0 {
			return false, fmt.Errorf("pod not ready")
		}
		return currentPod.Status.ContainerStatuses[0].Ready, nil
	}, "5m", "1s", ctx).Should(BeTrue())
	return pod.Name
}

func createTestRegistryService(ctx context.Context, cli client.Client, namespace, prefix string) string {
	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: prefix,
			Namespace:    namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "grpc",
					Port:       50051,
					TargetPort: intstr.FromInt(50051),
				},
			},
			Selector: map[string]string{"catalogsource": "prometheus-index"},
		},
	}
	err := c.Create(ctx, svc)
	Expect(err).To(BeNil())

	return svc.Name
}

func getServiceAddress(ctx context.Context, cli client.Client, namespace, name string) string {
	svc := corev1.Service{}
	err := cli.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &svc)
	Expect(err).To(BeNil())
	return fmt.Sprintf("%s.%s.svc:%d", svc.Name, svc.Namespace, svc.Spec.Ports[0].Port)
}

// TODO: ensure CRD support in test environment setup in Makefile
func applyCRDifNotPresent(ctx context.Context) func() {
	cleanup := func() {}
	scheme := runtime.NewScheme()
	//Add CRDs
	err := apiextensionsscheme.AddToScheme(scheme)
	Expect(err).To(BeNil())

	c := config.GetConfigOrDie()
	ctrlCli, err := controllerClient.New(c, controllerClient.Options{
		Scheme: scheme,
	})
	Expect(err).To(BeNil())

	catalogSourceCRD := apiextensionsv1.CustomResourceDefinition{}
	err = ctrlCli.Get(ctx, types.NamespacedName{Name: "catalogsources.operators.coreos.com"}, &catalogSourceCRD)
	if err != nil {
		Expect(errors.IsNotFound(err)).To(BeTrue())
		crdContents, err := os.ReadFile("../../testdata/crds/operators.coreos.com_catalogsources.yaml")
		Expect(err).To(BeNil())

		err = yaml2.Unmarshal(crdContents, &catalogSourceCRD)
		Expect(err).To(BeNil())

		err = ctrlCli.Create(ctx, &catalogSourceCRD)
		if !errors.IsAlreadyExists(err) {
			Expect(err).To(BeNil())
			// cleanup catalogsource crd only if it didn't already exist on-cluster
			cleanup = func() {
				err = ctrlCli.Delete(ctx, &apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: "catalogsources.operators.coreos.com"}})
				Expect(err).To(BeNil())
			}
		}
	}
	return cleanup
}

func createTestCatalogSource(ctx context.Context, cli client.Client, namespace, name, serviceName string) {
	scheme := runtime.NewScheme()
	// Add catalogSources
	err := v1alpha1.AddToScheme(scheme)
	Expect(err).To(BeNil())

	c := config.GetConfigOrDie()
	ctrlCli, err := controllerClient.New(c, controllerClient.Options{
		Scheme: scheme,
	})
	Expect(err).To(BeNil())

	err = ctrlCli.Create(ctx, &v1alpha1.CatalogSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.CatalogSourceSpec{
			Address:    getServiceAddress(ctx, cli, namespace, serviceName),
			SourceType: v1alpha1.SourceTypeGrpc,
		},
	})
	Expect(err).To(BeNil())
}
