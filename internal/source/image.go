package source

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/nlepage/go-tarfs"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	applyconfigurationcorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	v1 "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	catalogdv1alpha1 "github.com/operator-framework/catalogd/api/core/v1alpha1"
)

type Image struct {
	Client       client.Client
	KubeClient   kubernetes.Interface
	PodNamespace string
	UnpackImage  string
}

const imageCatalogUnpackContainerName = "catalog"

func (i *Image) Unpack(ctx context.Context, catalog *catalogdv1alpha1.Catalog) (*Result, error) {
	if catalog.Spec.Source.Type != catalogdv1alpha1.SourceTypeImage {
		panic(fmt.Sprintf("source type %q is unable to handle specified catalog source type %q", catalogdv1alpha1.SourceTypeImage, catalog.Spec.Source.Type))
	}
	if catalog.Spec.Source.Image == nil {
		return nil, fmt.Errorf("catalog source image configuration is unset")
	}

	pod := &corev1.Pod{}
	op, err := i.ensureUnpackPod(ctx, catalog, pod)
	if err != nil {
		return nil, err
	} else if op == controllerutil.OperationResultCreated || op == controllerutil.OperationResultUpdated || pod.DeletionTimestamp != nil {
		return &Result{State: StatePending}, nil
	}

	switch phase := pod.Status.Phase; phase {
	case corev1.PodPending:
		return pendingImagePodResult(pod), nil
	case corev1.PodRunning:
		return &Result{State: StateUnpacking}, nil
	case corev1.PodFailed:
		return nil, i.failedPodResult(ctx, pod)
	case corev1.PodSucceeded:
		return i.succeededPodResult(ctx, pod)
	default:
		return nil, i.handleUnexpectedPod(ctx, pod)
	}
}

func (i *Image) ensureUnpackPod(ctx context.Context, catalog *catalogdv1alpha1.Catalog, pod *corev1.Pod) (controllerutil.OperationResult, error) {
	existingPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: i.PodNamespace, Name: catalog.Name}}
	if err := i.Client.Get(ctx, client.ObjectKeyFromObject(existingPod), existingPod); client.IgnoreNotFound(err) != nil {
		return controllerutil.OperationResultNone, err
	}

	podApplyConfig := i.getDesiredPodApplyConfig(catalog)
	updatedPod, err := i.KubeClient.CoreV1().Pods(i.PodNamespace).Apply(ctx, podApplyConfig, metav1.ApplyOptions{Force: true, FieldManager: "catalogd-core"})
	if err != nil {
		if !apierrors.IsInvalid(err) {
			return controllerutil.OperationResultNone, err
		}
		if err := i.Client.Delete(ctx, existingPod); err != nil {
			return controllerutil.OperationResultNone, err
		}
		updatedPod, err = i.KubeClient.CoreV1().Pods(i.PodNamespace).Apply(ctx, podApplyConfig, metav1.ApplyOptions{Force: true, FieldManager: "catalogd-core"})
		if err != nil {
			return controllerutil.OperationResultNone, err
		}
	}

	// make sure the passed in pod value is updated with the latest
	// version of the pod
	*pod = *updatedPod

	// compare existingPod to newPod and return an appropriate
	// OperatorResult value.
	newPod := updatedPod.DeepCopy()
	unsetNonComparedPodFields(existingPod, newPod)
	if equality.Semantic.DeepEqual(existingPod, newPod) {
		return controllerutil.OperationResultNone, nil
	}
	return controllerutil.OperationResultUpdated, nil
}

func (i *Image) getDesiredPodApplyConfig(catalog *catalogdv1alpha1.Catalog) *applyconfigurationcorev1.PodApplyConfiguration {
	// TODO: Address unpacker pod allowing root users for image sources
	//
	// In our current implementation, we are creating a pod that uses the image
	// provided by an image source. This pod is not always guaranteed to run as a
	// non-root user and thus will fail to initialize if running as root in a PSA
	// restricted namespace due to violations. As it currently stands, our compliance
	// with PSA is baseline which allows for pods to run as root users. However,
	// all RukPak processes and resources, except this unpacker pod for image sources,
	// are runnable in a PSA restricted environment. We should consider ways to make
	// this PSA definition either configurable or workable in a restricted namespace.
	//
	// See https://github.com/operator-framework/rukpak/pull/539 for more detail.
	containerSecurityContext := applyconfigurationcorev1.SecurityContext().
		WithAllowPrivilegeEscalation(false).
		WithCapabilities(applyconfigurationcorev1.Capabilities().
			WithDrop("ALL"),
		)

	podApply := applyconfigurationcorev1.Pod(catalog.Name, i.PodNamespace).
		WithLabels(map[string]string{
			"catalogd.operatorframework.io/owner-kind": catalog.Kind,
			"catalogd.operatorframework.io/owner-name": catalog.Name,
		}).
		WithOwnerReferences(v1.OwnerReference().
			WithName(catalog.Name).
			WithKind(catalog.Kind).
			WithAPIVersion(catalog.APIVersion).
			WithUID(catalog.UID).
			WithController(true).
			WithBlockOwnerDeletion(true),
		).
		WithSpec(applyconfigurationcorev1.PodSpec().
			WithAutomountServiceAccountToken(false).
			WithRestartPolicy(corev1.RestartPolicyNever).
			WithInitContainers(applyconfigurationcorev1.Container().
				WithName("install-unpack").
				WithImage(i.UnpackImage).
				WithImagePullPolicy(corev1.PullIfNotPresent).
				WithCommand("cp", "-Rv", "/unpack", "/util/bin/unpack").
				WithVolumeMounts(applyconfigurationcorev1.VolumeMount().
					WithName("util").
					WithMountPath("/util/bin"),
				).
				WithSecurityContext(containerSecurityContext),
			).
			WithContainers(applyconfigurationcorev1.Container().
				WithName(imageCatalogUnpackContainerName).
				WithImage(catalog.Spec.Source.Image.Ref).
				WithCommand("/util/bin/unpack", "--bundle-dir", "/configs").
				WithVolumeMounts(applyconfigurationcorev1.VolumeMount().
					WithName("util").
					WithMountPath("/util/bin"),
				).
				WithSecurityContext(containerSecurityContext),
			).
			WithVolumes(applyconfigurationcorev1.Volume().
				WithName("util").
				WithEmptyDir(applyconfigurationcorev1.EmptyDirVolumeSource()),
			).
			WithSecurityContext(applyconfigurationcorev1.PodSecurityContext().
				WithRunAsNonRoot(false).
				WithSeccompProfile(applyconfigurationcorev1.SeccompProfile().
					WithType(corev1.SeccompProfileTypeRuntimeDefault),
				),
			),
		)

	if catalog.Spec.Source.Image.PullSecret != "" {
		podApply.Spec = podApply.Spec.WithImagePullSecrets(
			applyconfigurationcorev1.LocalObjectReference().WithName(catalog.Spec.Source.Image.PullSecret),
		)
	}
	return podApply
}

func unsetNonComparedPodFields(pods ...*corev1.Pod) {
	for _, p := range pods {
		p.APIVersion = ""
		p.Kind = ""
		p.Status = corev1.PodStatus{}
	}
}

func (i *Image) failedPodResult(ctx context.Context, pod *corev1.Pod) error {
	logs, err := i.getPodLogs(ctx, pod)
	if err != nil {
		return fmt.Errorf("unpack failed: failed to retrieve failed pod logs: %v", err)
	}
	_ = i.Client.Delete(ctx, pod)
	return fmt.Errorf("unpack failed: %v", string(logs))
}

func (i *Image) succeededPodResult(ctx context.Context, pod *corev1.Pod) (*Result, error) {
	catalogFS, err := i.getCatalogContents(ctx, pod)
	if err != nil {
		return nil, fmt.Errorf("get catalog contents: %v", err)
	}

	digest, err := i.getCatalogImageDigest(pod)
	if err != nil {
		return nil, fmt.Errorf("get catalog image digest: %v", err)
	}

	resolvedSource := &catalogdv1alpha1.CatalogSource{
		Type:  catalogdv1alpha1.SourceTypeImage,
		Image: &catalogdv1alpha1.ImageSource{Ref: digest},
	}

	message := fmt.Sprintf("successfully unpacked the catalog image %q", digest)

	return &Result{FS: catalogFS, ResolvedSource: resolvedSource, State: StateUnpacked, Message: message}, nil
}

func (i *Image) getCatalogContents(ctx context.Context, pod *corev1.Pod) (fs.FS, error) {
	catalogData, err := i.getPodLogs(ctx, pod)
	if err != nil {
		return nil, fmt.Errorf("get catalog contents: %v", err)
	}
	bd := struct {
		Content []byte `json:"content"`
	}{}

	if err := json.Unmarshal(catalogData, &bd); err != nil {
		return nil, fmt.Errorf("parse catalog data: %v", err)
	}

	gzr, err := gzip.NewReader(bytes.NewReader(bd.Content))
	if err != nil {
		return nil, fmt.Errorf("read catalog content gzip: %v", err)
	}
	return tarfs.New(gzr)
}

func (i *Image) getCatalogImageDigest(pod *corev1.Pod) (string, error) {
	for _, ps := range pod.Status.ContainerStatuses {
		if ps.Name == imageCatalogUnpackContainerName && ps.ImageID != "" {
			return ps.ImageID, nil
		}
	}
	return "", fmt.Errorf("catalog image digest not found")
}

func (i *Image) getPodLogs(ctx context.Context, pod *corev1.Pod) ([]byte, error) {
	logReader, err := i.KubeClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{}).Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("get pod logs: %v", err)
	}
	defer logReader.Close()
	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, logReader); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (i *Image) handleUnexpectedPod(ctx context.Context, pod *corev1.Pod) error {
	_ = i.Client.Delete(ctx, pod)
	return fmt.Errorf("unexpected pod phase: %v", pod.Status.Phase)
}

func pendingImagePodResult(pod *corev1.Pod) *Result {
	var messages []string
	for _, cStatus := range append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...) {
		if waiting := cStatus.State.Waiting; waiting != nil {
			if waiting.Reason == "ErrImagePull" || waiting.Reason == "ImagePullBackOff" {
				messages = append(messages, waiting.Message)
			}
		}
	}
	return &Result{State: StatePending, Message: strings.Join(messages, "; ")}
}
