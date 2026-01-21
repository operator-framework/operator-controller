package applier

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/postrender"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apimachyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authorization"
	"github.com/operator-framework/operator-controller/internal/operator-controller/contentmanager"
	"github.com/operator-framework/operator-controller/internal/operator-controller/contentmanager/cache"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
	imageutil "github.com/operator-framework/operator-controller/internal/shared/util/image"
)

// HelmChartProvider provides helm charts from bundle sources and cluster extensions
type HelmChartProvider interface {
	Get(bundle fs.FS, clusterExtension *ocv1.ClusterExtension) (*chart.Chart, error)
}

type HelmReleaseToObjectsConverter struct {
}

type HelmReleaseToObjectsConverterInterface interface {
	GetObjectsFromRelease(rel *release.Release) ([]client.Object, error)
}

func (h HelmReleaseToObjectsConverter) GetObjectsFromRelease(rel *release.Release) ([]client.Object, error) {
	if rel == nil {
		return nil, nil
	}

	relObjects, err := util.ManifestObjects(strings.NewReader(rel.Manifest), fmt.Sprintf("%s-release-manifest", rel.Name))
	if err != nil {
		return nil, fmt.Errorf("parsing release %q objects: %w", rel.Name, err)
	}
	return relObjects, nil
}

type Helm struct {
	ActionClientGetter            helmclient.ActionClientGetter
	Preflights                    []Preflight
	PreAuthorizer                 authorization.PreAuthorizer
	HelmChartProvider             HelmChartProvider
	HelmReleaseToObjectsConverter HelmReleaseToObjectsConverterInterface

	Manager contentmanager.Manager
	Watcher cache.Watcher
}

// runPreAuthorizationChecks performs pre-authorization checks for a Helm release
// it renders a client-only release, checks permissions using the PreAuthorizer
// and returns an error if authorization fails or required permissions are missing
func (h *Helm) runPreAuthorizationChecks(ctx context.Context, ext *ocv1.ClusterExtension, chart *chart.Chart, values chartutil.Values, post postrender.PostRenderer) error {
	tmplRel, err := h.renderClientOnlyRelease(ctx, ext, chart, values, post)
	if err != nil {
		return fmt.Errorf("error rendering content for pre-authorization checks: %w", err)
	}

	manifestManager := getUserInfo(ext)
	return formatPreAuthorizerOutput(h.PreAuthorizer.PreAuthorize(ctx, manifestManager, strings.NewReader(tmplRel.Manifest), extManagementPerms(ext)))
}

func (h *Helm) Apply(ctx context.Context, contentFS fs.FS, ext *ocv1.ClusterExtension, objectLabels map[string]string, storageLabels map[string]string) (bool, string, error) {
	// If contentFS is nil, we're maintaining the current state without catalog access.
	// In this case, reconcile the existing Helm release if it exists.
	if contentFS == nil {
		ac, err := h.ActionClientGetter.ActionClientFor(ctx, ext)
		if err != nil {
			return false, "", err
		}
		return h.reconcileExistingRelease(ctx, ac, ext)
	}

	chrt, err := h.buildHelmChart(contentFS, ext)
	if err != nil {
		return false, "", err
	}
	values := chartutil.Values{}

	post := &postrenderer{
		labels: objectLabels,
	}

	if h.PreAuthorizer != nil {
		err := h.runPreAuthorizationChecks(ctx, ext, chrt, values, post)
		if err != nil {
			// Return the pre-authorization error directly
			return false, "", err
		}
	}

	ac, err := h.ActionClientGetter.ActionClientFor(ctx, ext)
	if err != nil {
		return false, "", err
	}

	rel, desiredRel, state, err := h.getReleaseState(ac, ext, chrt, values, post)
	if err != nil {
		return false, "", fmt.Errorf("failed to get release state using server-side dry-run: %w", err)
	}
	objs, err := h.HelmReleaseToObjectsConverter.GetObjectsFromRelease(desiredRel)
	if err != nil {
		return false, "", err
	}

	for _, preflight := range h.Preflights {
		if shouldSkipPreflight(ctx, preflight, ext, state) {
			continue
		}
		switch state {
		case StateNeedsInstall:
			err := preflight.Install(ctx, objs)
			if err != nil {
				return false, "", err
			}
		case StateNeedsUpgrade:
			err := preflight.Upgrade(ctx, objs)
			if err != nil {
				return false, "", err
			}
		}
	}

	switch state {
	case StateNeedsInstall:
		rel, err = ac.Install(ext.GetName(), ext.Spec.Namespace, chrt, values, func(install *action.Install) error {
			install.CreateNamespace = false
			install.Labels = storageLabels
			return nil
		}, helmclient.AppendInstallPostRenderer(post))
		if err != nil {
			return false, "", err
		}
	case StateNeedsUpgrade:
		rel, err = ac.Upgrade(ext.GetName(), ext.Spec.Namespace, chrt, values, func(upgrade *action.Upgrade) error {
			upgrade.MaxHistory = maxHelmReleaseHistory
			upgrade.Labels = storageLabels
			return nil
		}, helmclient.AppendUpgradePostRenderer(post))
		if err != nil {
			return false, "", err
		}
	case StateUnchanged:
		if err := ac.Reconcile(rel); err != nil {
			return false, "", err
		}
	default:
		return false, "", fmt.Errorf("unexpected release state %q", state)
	}

	relObjects, err := util.ManifestObjects(strings.NewReader(rel.Manifest), fmt.Sprintf("%s-release-manifest", rel.Name))
	if err != nil {
		return true, "", err
	}
	klog.FromContext(ctx).Info("watching managed objects")
	cache, err := h.Manager.Get(ctx, ext)
	if err != nil {
		return true, "", err
	}

	if err := cache.Watch(ctx, h.Watcher, relObjects...); err != nil {
		return true, "", err
	}

	return true, "", nil
}

// reconcileExistingRelease reconciles an existing Helm release without catalog access.
// This is used when the catalog is unavailable but we need to maintain the current installation.
// It reconciles the release to actively maintain resources, and sets up watchers for monitoring/observability.
func (h *Helm) reconcileExistingRelease(ctx context.Context, ac helmclient.ActionInterface, ext *ocv1.ClusterExtension) (bool, string, error) {
	rel, err := ac.Get(ext.GetName())
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return false, "", fmt.Errorf("catalog content unavailable and no release installed")
	}
	if err != nil {
		return false, "", fmt.Errorf("failed to get current release: %w", err)
	}

	// Reconcile the existing release to ensure resources are maintained
	if err := ac.Reconcile(rel); err != nil {
		// Reconcile failed - resources NOT maintained
		// Return false (rollout failed) with error
		return false, "", err
	}

	// At this point: Reconcile succeeded - resources ARE maintained (applied to cluster via Server-Side Apply)
	// The operations below are for setting up watches to detect drift (i.e., if someone manually modifies the
	// resources). If watch setup fails, the resources are still successfully maintained, but we won't detect
	// and auto-correct manual modifications. We return true (rollout succeeded) and log watch errors.
	logger := klog.FromContext(ctx)

	relObjects, err := util.ManifestObjects(strings.NewReader(rel.Manifest), fmt.Sprintf("%s-release-manifest", rel.Name))
	if err != nil {
		logger.Error(err, "failed to parse manifest objects, cannot set up drift detection watches (resources are applied but drift detection disabled)")
		return true, "", nil
	}

	logger.V(1).Info("setting up drift detection watches on managed objects")

	// Defensive nil checks to prevent panics if Manager or Watcher not properly initialized
	if h.Manager == nil {
		logger.Error(fmt.Errorf("manager is nil"), "Manager not initialized, cannot set up drift detection watches (resources are applied but drift detection disabled)")
		return true, "", nil
	}
	cache, err := h.Manager.Get(ctx, ext)
	if err != nil {
		logger.Error(err, "failed to get managed content cache, cannot set up drift detection watches (resources are applied but drift detection disabled)")
		return true, "", nil
	}

	if h.Watcher == nil {
		logger.Error(fmt.Errorf("watcher is nil"), "Watcher not initialized, cannot set up drift detection watches (resources are applied but drift detection disabled)")
		return true, "", nil
	}
	if err := cache.Watch(ctx, h.Watcher, relObjects...); err != nil {
		logger.Error(err, "failed to set up drift detection watches (resources are applied but drift detection disabled)")
		return true, "", nil
	}

	return true, "", nil
}

func (h *Helm) buildHelmChart(bundleFS fs.FS, ext *ocv1.ClusterExtension) (*chart.Chart, error) {
	if h.HelmChartProvider == nil {
		return nil, errors.New("HelmChartProvider is nil")
	}
	if features.OperatorControllerFeatureGate.Enabled(features.HelmChartSupport) {
		meta := new(chart.Metadata)
		if ok, _ := imageutil.IsBundleSourceChart(bundleFS, meta); ok {
			return imageutil.LoadChartFSWithOptions(
				bundleFS,
				fmt.Sprintf("%s-%s.tgz", meta.Name, meta.Version),
				imageutil.WithInstallNamespace(ext.Spec.Namespace),
			)
		}
	}
	return h.HelmChartProvider.Get(bundleFS, ext)
}

func (h *Helm) renderClientOnlyRelease(ctx context.Context, ext *ocv1.ClusterExtension, chrt *chart.Chart, values chartutil.Values, post postrender.PostRenderer) (*release.Release, error) {
	// We need to get a separate action client because our work below
	// permanently modifies the underlying action.Configuration for ClientOnly mode.
	ac, err := h.ActionClientGetter.ActionClientFor(ctx, ext)
	if err != nil {
		return nil, err
	}

	isUpgrade := false
	currentRelease, err := ac.Get(ext.GetName())
	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
		return nil, err
	}
	if currentRelease != nil {
		isUpgrade = true
	}

	return ac.Install(ext.GetName(), ext.Spec.Namespace, chrt, values, func(i *action.Install) error {
		i.DryRun = true
		i.ReleaseName = ext.GetName()
		i.Replace = true
		i.ClientOnly = true
		i.IncludeCRDs = true
		i.IsUpgrade = isUpgrade
		return nil
	}, helmclient.AppendInstallPostRenderer(post))
}

func (h *Helm) getReleaseState(cl helmclient.ActionInterface, ext *ocv1.ClusterExtension, chrt *chart.Chart, values chartutil.Values, post postrender.PostRenderer) (*release.Release, *release.Release, string, error) {
	currentRelease, err := cl.Get(ext.GetName())
	if errors.Is(err, driver.ErrReleaseNotFound) {
		desiredRelease, err := cl.Install(ext.GetName(), ext.Spec.Namespace, chrt, values, func(i *action.Install) error {
			i.DryRun = true
			i.DryRunOption = "server"
			return nil
		}, helmclient.AppendInstallPostRenderer(post))
		if err != nil {
			return nil, nil, StateError, err
		}
		return nil, desiredRelease, StateNeedsInstall, nil
	}
	if err != nil {
		return nil, nil, StateError, err
	}

	desiredRelease, err := cl.Upgrade(ext.GetName(), ext.Spec.Namespace, chrt, values, func(upgrade *action.Upgrade) error {
		upgrade.MaxHistory = maxHelmReleaseHistory
		upgrade.DryRun = true
		upgrade.DryRunOption = "server"
		return nil
	}, helmclient.AppendUpgradePostRenderer(post))
	if err != nil {
		return currentRelease, nil, StateError, err
	}
	relState := StateUnchanged
	if desiredRelease.Manifest != currentRelease.Manifest ||
		currentRelease.Info.Status == release.StatusFailed ||
		currentRelease.Info.Status == release.StatusSuperseded {
		relState = StateNeedsUpgrade
	}
	return currentRelease, desiredRelease, relState, nil
}

type postrenderer struct {
	labels  map[string]string
	cascade postrender.PostRenderer
}

func (p *postrenderer) Run(renderedManifests *bytes.Buffer) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	dec := apimachyaml.NewYAMLOrJSONDecoder(renderedManifests, 1024)
	for {
		obj := unstructured.Unstructured{}
		err := dec.Decode(&obj)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		obj.SetLabels(util.MergeMaps(obj.GetLabels(), p.labels))
		b, err := obj.MarshalJSON()
		if err != nil {
			return nil, err
		}
		buf.Write(b)
	}
	if p.cascade != nil {
		return p.cascade.Run(&buf)
	}
	return &buf, nil
}

func ruleDescription(ns string, rule rbacv1.PolicyRule) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Namespace:%q", ns))

	if len(rule.APIGroups) > 0 {
		sb.WriteString(fmt.Sprintf(" APIGroups:[%s]", strings.Join(slices.Sorted(slices.Values(rule.APIGroups)), ",")))
	}
	if len(rule.Resources) > 0 {
		sb.WriteString(fmt.Sprintf(" Resources:[%s]", strings.Join(slices.Sorted(slices.Values(rule.Resources)), ",")))
	}
	if len(rule.ResourceNames) > 0 {
		sb.WriteString(fmt.Sprintf(" ResourceNames:[%s]", strings.Join(slices.Sorted(slices.Values(rule.ResourceNames)), ",")))
	}
	if len(rule.Verbs) > 0 {
		sb.WriteString(fmt.Sprintf(" Verbs:[%s]", strings.Join(slices.Sorted(slices.Values(rule.Verbs)), ",")))
	}
	if len(rule.NonResourceURLs) > 0 {
		sb.WriteString(fmt.Sprintf(" NonResourceURLs:[%s]", strings.Join(slices.Sorted(slices.Values(rule.NonResourceURLs)), ",")))
	}
	return sb.String()
}

// formatPreAuthorizerOutput formats the output of PreAuthorizer.PreAuthorize calls into a consistent and deterministic error.
// If the call returns no missing rules, and no error, nil is returned.
func formatPreAuthorizerOutput(missingRules []authorization.ScopedPolicyRules, authErr error) error {
	var preAuthErrors []error
	if len(missingRules) > 0 {
		var missingRuleDescriptions []string
		for _, policyRules := range missingRules {
			for _, rule := range policyRules.MissingRules {
				missingRuleDescriptions = append(missingRuleDescriptions, ruleDescription(policyRules.Namespace, rule))
			}
		}
		slices.Sort(missingRuleDescriptions)
		// This phrase is explicitly checked by external testing
		preAuthErrors = append(preAuthErrors, fmt.Errorf("service account requires the following permissions to manage cluster extension:\n  %s", strings.Join(missingRuleDescriptions, "\n  ")))
	}
	if authErr != nil {
		preAuthErrors = append(preAuthErrors, fmt.Errorf("authorization evaluation error: %w", authErr))
	}
	if len(preAuthErrors) > 0 {
		// This phrase is explicitly checked by external testing
		return fmt.Errorf("pre-authorization failed: %v", errors.Join(preAuthErrors...))
	}
	return nil
}

func getUserInfo(ext *ocv1.ClusterExtension) user.Info {
	return &user.DefaultInfo{Name: fmt.Sprintf("system:serviceaccount:%s:%s", ext.Spec.Namespace, ext.Spec.ServiceAccount.Name)}
}

func extManagementPerms(ext *ocv1.ClusterExtension) func(user.Info) []authorizer.AttributesRecord {
	return func(user user.Info) []authorizer.AttributesRecord {
		return []authorizer.AttributesRecord{
			{
				User:            user,
				Name:            ext.Name,
				APIGroup:        ocv1.GroupVersion.Group,
				APIVersion:      ocv1.GroupVersion.Version,
				Resource:        "clusterextensions/finalizers",
				ResourceRequest: true,
				Verb:            "update",
			},
		}
	}
}
