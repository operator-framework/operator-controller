package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"

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
	"k8s.io/client-go/kubernetes"
	controllerClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

// e2e resolution from a catalogsource backed entitysource without olm
//var _ = Describe("Deppy", func() {
//	var kubeClient *kubernetes.Clientset
//	var err error
//	var testNamespace string
//	cleanup := func() {}
//	var ctx = context.TODO()
//	BeforeEach(func() {
//		kubeClient, err = kubernetes.NewForConfig(config.GetConfigOrDie())
//		Expect(err).To(BeNil())
//		testNamespace = createTestNamespace(ctx, kubeClient, "registry-grpc-")
//		cleanup = applyCRDifNotPresent(ctx)
//	})
//	AfterEach(func() {
//		deleteTestNamespace(ctx, kubeClient, testNamespace)
//		cleanup()
//	})
//	It("can install an operator from a catalogSource", func() {
//		testPrefix := "registry-grpc-"
//
//		serviceAccountName := createTestServiceAccount(ctx, kubeClient, testNamespace, testPrefix)
//		createTestRegistryPod(ctx, kubeClient, testNamespace, testPrefix, serviceAccountName)
//		serviceName := createTestRegistryService(ctx, kubeClient, testNamespace, testPrefix)
//		createTestCatalogSource(ctx, kubeClient, testNamespace, "prometheus-index", serviceName)
//
//		scheme := runtime.NewScheme()
//		// Add catalogSources
//		err = v1alpha1.AddToScheme(scheme)
//		Expect(err).To(BeNil())
//
//		c := config.GetConfigOrDie()
//		ctrlCli, err := controllerClient.NewWithWatch(c, controllerClient.Options{
//			Scheme: scheme,
//		})
//		Expect(err).To(BeNil())
//
//		logger := zap.New()
//		cacheCli := catalogsource.NewCachedRegistryQuerier(ctrlCli, catalogsource.NewRegistryGRPCClient(0), &logger)
//
//		go cacheCli.Start(ctx)
//
//		defer cacheCli.Stop()
//
//		Eventually(func(g Gomega) {
//			// wait till cache is populated
//			var entityIDs []deppy.Identifier
//			err := cacheCli.Iterate(ctx, func(entity *input.Entity) error {
//				entityIDs = append(entityIDs, entity.Identifier())
//				return nil
//			})
//			g.Expect(err).To(BeNil())
//			g.Expect(entityIDs).To(ConsistOf([]deppy.Identifier{
//				deppy.IdentifierFromString(testNamespace + "/prometheus-index/prometheus/beta/0.14.0"),
//				deppy.IdentifierFromString(testNamespace + "/prometheus-index/prometheus/beta/0.15.0"),
//				deppy.IdentifierFromString(testNamespace + "/prometheus-index/prometheus/beta/0.22.2"),
//				deppy.IdentifierFromString(testNamespace + "/prometheus-index/prometheus/beta/0.27.0"),
//				deppy.IdentifierFromString(testNamespace + "/prometheus-index/prometheus/beta/0.32.0"),
//				deppy.IdentifierFromString(testNamespace + "/prometheus-index/prometheus/beta/0.37.0"),
//			}))
//		}).Should(Succeed())
//
//		s, err := solver.NewDeppySolver(cacheCli, olm.NewOLMVariableSource("prometheus"))
//
//		//catalogsource.WithDependencies([]*input.Entity{{ID: deppy.IdentifierFromString("with package prometheus"), Properties: map[string]string{
//		//	property.TypePackageRequired: `[{"packageName":"prometheus","version":">=0.37.0"}]`,
//		//}}})
//		Expect(err).To(BeNil())
//		solutionSet, err := s.Solve(ctx, solver.AddAllVariablesToSolution())
//		Expect(err).To(BeNil())
//
//		Expect(solutionSet.Error()).To(BeNil())
//		Expect(solutionSet.SelectedVariables()).To(HaveKey(deppy.IdentifierFromString(testNamespace + "/prometheus-index/prometheus/beta/0.37.0")))
//		Expect(solutionSet)
//
//	})
//
//})

// ListEntities
func createTestNamespace(ctx context.Context, c *kubernetes.Clientset, prefix string) string {
	ns, err := c.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: prefix,
		},
	}, metav1.CreateOptions{})
	Expect(err).To(BeNil())
	return ns.Name
}

func deleteTestNamespace(ctx context.Context, c *kubernetes.Clientset, name string) {
	err := c.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
	Expect(err).To(BeNil())
}

func createTestServiceAccount(ctx context.Context, cli *kubernetes.Clientset, namespace, prefix string) string {
	sa, err := cli.CoreV1().ServiceAccounts(namespace).Create(ctx, &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: prefix,
			Namespace:    namespace,
		},
	}, metav1.CreateOptions{})
	Expect(err).To(BeNil())
	return sa.Name
}

func createTestRegistryPod(ctx context.Context, cli *kubernetes.Clientset, namespace, prefix, serviceAccount string) string {
	pod, err := cli.CoreV1().Pods(namespace).Create(ctx, &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: prefix,
			Labels:       map[string]string{"catalogsource": "prometheus-index"},
			Annotations:  nil,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "registry",
				// TODO: switch to using locally built and loaded images to avoid flakes
				Image: "quay.io/ankitathomas/index:prometheus-index-v0.37.0",
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
		},
	}, metav1.CreateOptions{})
	Expect(err).To(BeNil())

	Eventually(func() (bool, error) {
		currentPod, err := cli.CoreV1().Pods(namespace).Get(ctx, pod.Name, metav1.GetOptions{})
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

func createTestRegistryService(ctx context.Context, cli *kubernetes.Clientset, namespace, prefix string) string {
	svc, err := cli.CoreV1().Services(namespace).Create(ctx, &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: prefix,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "grpc",
					Port:       50051,
					TargetPort: intstr.FromInt(50051),
				},
			},
			Type:     corev1.ServiceTypeNodePort,
			Selector: map[string]string{"catalogsource": "prometheus-index"},
		},
	}, metav1.CreateOptions{})
	Expect(err).To(BeNil())

	conn, err := grpc.Dial(getServiceAddress(ctx, cli, namespace, svc.Name), []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}...)
	defer conn.Close()
	oldState := conn.GetState()
	Eventually(func(g Gomega) {
		state := conn.GetState()
		if state != connectivity.Ready {
			if conn.WaitForStateChange(ctx, conn.GetState()) {
				state = conn.GetState()
				if oldState != state {
					oldState = state
					if state == connectivity.Idle {
						conn.Connect()
					}
				}
			}
		}
		g.Expect(conn.GetState()).To(Equal(connectivity.Ready))
	}).WithTimeout(2 * time.Minute).Should(Succeed())
	return svc.Name
}

func getServiceAddress(ctx context.Context, cli *kubernetes.Clientset, namespace, name string) string {
	svc, err := cli.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	Expect(err).To(BeNil())
	c := config.GetConfigOrDie()
	parts := strings.Split(c.Host, ":")
	address := parts[0]
	if len(parts) == 3 {
		address = strings.TrimLeft(parts[1], "/")
	}
	// TODO: not required on-cluster, fix Dockerfile and use fmt.Sprintf("%s.%s.svc:%d", svc.Name, svc.Namespace, svc.Spec.Ports[0].Port)
	return fmt.Sprintf("%s:%d", address, svc.Spec.Ports[0].NodePort)
}

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
		crdContents, err := os.ReadFile("../testdata/operators.coreos.com_catalogsources.crd.yaml")
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

func createTestCatalogSource(ctx context.Context, cli *kubernetes.Clientset, namespace, name, serviceName string) {
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
			Address: getServiceAddress(ctx, cli, namespace, serviceName),
		},
	})
	Expect(err).To(BeNil())
}
