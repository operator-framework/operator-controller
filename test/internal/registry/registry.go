// Package registry provides functions to deploy an OCI image registry
// in a Kubernetes cluster for e2e testing.
package registry

import (
	"context"
	"fmt"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmapplyconfig "github.com/cert-manager/cert-manager/pkg/client/applyconfigurations/certmanager/v1"
	cmmetaapplyconfig "github.com/cert-manager/cert-manager/pkg/client/applyconfigurations/meta/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultNamespace = "operator-controller-e2e"
	DefaultName      = "docker-registry"
	NodePort         = int32(30000)
)

// Deploy ensures the image registry namespace, TLS certificate, deployment,
// and service exist in the cluster. It is idempotent — if the resources
// already exist, they are updated in place via server-side apply.
func Deploy(ctx context.Context, cfg *rest.Config, namespace, name string) error {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add core/v1 to scheme: %w", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add apps/v1 to scheme: %w", err)
	}
	if err := certmanagerv1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add cert-manager/v1 to scheme: %w", err)
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	secretName := fmt.Sprintf("%s-registry", namespace)
	fieldOwner := client.FieldOwner("e2e-test")

	// Apply namespace
	if err := c.Apply(ctx, corev1ac.Namespace(namespace), fieldOwner, client.ForceOwnership); err != nil {
		return fmt.Errorf("failed to apply namespace: %w", err)
	}

	// Apply TLS certificate
	cert := cmapplyconfig.Certificate(secretName, namespace).
		WithSpec(cmapplyconfig.CertificateSpec().
			WithSecretName(secretName).
			WithIsCA(true).
			WithDNSNames(
				fmt.Sprintf("%s.%s.svc", name, namespace),
				fmt.Sprintf("%s.%s.svc.cluster.local", name, namespace),
			).
			WithPrivateKey(cmapplyconfig.CertificatePrivateKey().
				WithRotationPolicy(certmanagerv1.RotationPolicyAlways).
				WithAlgorithm(certmanagerv1.ECDSAKeyAlgorithm).
				WithSize(256),
			).
			WithIssuerRef(cmmetaapplyconfig.IssuerReference().
				WithName("olmv1-ca").
				WithKind("ClusterIssuer").
				WithGroup("cert-manager.io"),
			),
		)
	if err := c.Apply(ctx, cert, fieldOwner, client.ForceOwnership); err != nil {
		return fmt.Errorf("failed to apply certificate: %w", err)
	}

	// Apply deployment
	podLabels := map[string]string{"app": "registry", "app.kubernetes.io/name": name}
	deploy := appsv1ac.Deployment(name, namespace).
		WithLabels(podLabels).
		WithSpec(appsv1ac.DeploymentSpec().
			WithReplicas(1).
			WithSelector(metav1ac.LabelSelector().
				WithMatchLabels(podLabels),
			).
			WithTemplate(corev1ac.PodTemplateSpec().
				WithLabels(podLabels).
				WithSpec(corev1ac.PodSpec().
					WithContainers(corev1ac.Container().
						WithName("registry").
						WithImage("registry:3").
						WithImagePullPolicy(corev1.PullIfNotPresent).
						WithVolumeMounts(corev1ac.VolumeMount().
							WithName("certs-vol").
							WithMountPath("/certs"),
						).
						WithEnv(
							corev1ac.EnvVar().WithName("REGISTRY_HTTP_TLS_CERTIFICATE").WithValue("/certs/tls.crt"),
							corev1ac.EnvVar().WithName("REGISTRY_HTTP_TLS_KEY").WithValue("/certs/tls.key"),
						),
					).
					WithVolumes(corev1ac.Volume().
						WithName("certs-vol").
						WithSecret(corev1ac.SecretVolumeSource().
							WithSecretName(secretName),
						),
					),
				),
			),
		)
	if err := c.Apply(ctx, deploy, fieldOwner, client.ForceOwnership); err != nil {
		return fmt.Errorf("failed to apply deployment: %w", err)
	}

	// Apply service
	svc := corev1ac.Service(name, namespace).
		WithSpec(corev1ac.ServiceSpec().
			WithSelector(podLabels).
			WithType(corev1.ServiceTypeNodePort).
			WithPorts(corev1ac.ServicePort().
				WithName("http").
				WithPort(5000).
				WithTargetPort(intstr.FromInt32(5000)).
				WithNodePort(NodePort),
			),
		)
	if err := c.Apply(ctx, svc, fieldOwner, client.ForceOwnership); err != nil {
		return fmt.Errorf("failed to apply service: %w", err)
	}

	// Wait for the deployment to be available
	d := &appsv1.Deployment{}
	deployKey := client.ObjectKey{Namespace: namespace, Name: name}
	if err := wait.PollUntilContextTimeout(ctx, time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		if err := c.Get(ctx, deployKey, d); err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		for _, cond := range d.Status.Conditions {
			if cond.Type == appsv1.DeploymentAvailable && cond.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("timed out waiting for registry deployment to become available: %w", err)
	}

	return nil
}
