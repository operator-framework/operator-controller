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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authorization"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/preflights/crdupgradesafety"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
)

const (
	StateNeedsInstall     string = "NeedsInstall"
	StateNeedsUpgrade     string = "NeedsUpgrade"
	StateUnchanged        string = "Unchanged"
	StateError            string = "Error"
	maxHelmReleaseHistory        = 10
)

// Preflight is a check that should be run before making any changes to the cluster
type Preflight interface {
	// Install runs checks that should be successful prior
	// to installing the Helm release. It is provided
	// a Helm release and returns an error if the
	// check is unsuccessful
	Install(context.Context, *release.Release) error

	// Upgrade runs checks that should be successful prior
	// to upgrading the Helm release. It is provided
	// a Helm release and returns an error if the
	// check is unsuccessful
	Upgrade(context.Context, *release.Release) error
}

type BundleToHelmChartConverter interface {
	ToHelmChart(bundle source.BundleSource, installNamespace string, watchNamespace string) (*chart.Chart, error)
}

type Helm struct {
	ActionClientGetter         helmclient.ActionClientGetter
	Preflights                 []Preflight
	PreAuthorizer              authorization.PreAuthorizer
	BundleToHelmChartConverter BundleToHelmChartConverter
}

// shouldSkipPreflight is a helper to determine if the preflight check is CRDUpgradeSafety AND
// if it is set to enforcement None.
func shouldSkipPreflight(ctx context.Context, preflight Preflight, ext *ocv1.ClusterExtension, state string) bool {
	l := log.FromContext(ctx)
	hasCRDUpgradeSafety := ext.Spec.Install != nil && ext.Spec.Install.Preflight != nil && ext.Spec.Install.Preflight.CRDUpgradeSafety != nil
	_, isCRDUpgradeSafetyInstance := preflight.(*crdupgradesafety.Preflight)

	if hasCRDUpgradeSafety && isCRDUpgradeSafetyInstance {
		if state == StateNeedsInstall || state == StateNeedsUpgrade {
			l.Info("crdUpgradeSafety ", "policy", ext.Spec.Install.Preflight.CRDUpgradeSafety.Enforcement)
		}
		if ext.Spec.Install.Preflight.CRDUpgradeSafety.Enforcement == ocv1.CRDUpgradeSafetyEnforcementNone {
			// Skip this preflight check because it is of type *crdupgradesafety.Preflight and the CRD Upgrade Safety
			// policy is set to None
			return true
		}
	}
	return false
}

// runPreAuthorizationChecks performs pre-authorization checks for a Helm release
// it renders a client-only release, checks permissions using the PreAuthorizer
// and returns an error if authorization fails or required permissions are missing
func (h *Helm) runPreAuthorizationChecks(ctx context.Context, ext *ocv1.ClusterExtension, chart *chart.Chart, values chartutil.Values, post postrender.PostRenderer) error {
	tmplRel, err := h.renderClientOnlyRelease(ctx, ext, chart, values, post)
	if err != nil {
		return fmt.Errorf("failed to get release state using client-only dry-run: %w", err)
	}

	missingRules, authErr := h.PreAuthorizer.PreAuthorize(ctx, ext, strings.NewReader(tmplRel.Manifest))

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

func (h *Helm) Apply(ctx context.Context, contentFS fs.FS, ext *ocv1.ClusterExtension, objectLabels map[string]string, storageLabels map[string]string) ([]client.Object, string, error) {
	chrt, err := h.buildHelmChart(contentFS, ext)
	if err != nil {
		return nil, "", err
	}
	values := chartutil.Values{}

	post := &postrenderer{
		labels: objectLabels,
	}

	if h.PreAuthorizer != nil {
		err := h.runPreAuthorizationChecks(ctx, ext, chrt, values, post)
		if err != nil {
			// Return the pre-authorization error directly
			return nil, "", err
		}
	}

	ac, err := h.ActionClientGetter.ActionClientFor(ctx, ext)
	if err != nil {
		return nil, "", err
	}

	rel, desiredRel, state, err := h.getReleaseState(ctx, ac, ext, chrt, values, post)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get release state using server-side dry-run: %w", err)
	}

	for _, preflight := range h.Preflights {
		if shouldSkipPreflight(ctx, preflight, ext, state) {
			continue
		}
		switch state {
		case StateNeedsInstall:
			err := preflight.Install(ctx, desiredRel)
			if err != nil {
				return nil, state, err
			}
		case StateNeedsUpgrade:
			err := preflight.Upgrade(ctx, desiredRel)
			if err != nil {
				return nil, state, err
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
			return nil, state, err
		}
	case StateNeedsUpgrade:
		rel, err = ac.Upgrade(ext.GetName(), ext.Spec.Namespace, chrt, values, func(upgrade *action.Upgrade) error {
			upgrade.MaxHistory = maxHelmReleaseHistory
			upgrade.Labels = storageLabels
			return nil
		}, helmclient.AppendUpgradePostRenderer(post))
		if err != nil {
			return nil, state, err
		}
	case StateUnchanged:
		if err := ac.Reconcile(rel); err != nil {
			return nil, state, err
		}
	default:
		return nil, state, fmt.Errorf("unexpected release state %q", state)
	}

	relObjects, err := util.ManifestObjects(strings.NewReader(rel.Manifest), fmt.Sprintf("%s-release-manifest", rel.Name))
	if err != nil {
		return nil, state, err
	}

	return relObjects, state, nil
}

func (h *Helm) buildHelmChart(bundleFS fs.FS, ext *ocv1.ClusterExtension) (*chart.Chart, error) {
	if h.BundleToHelmChartConverter == nil {
		return nil, errors.New("BundleToHelmChartConverter is nil")
	}
	watchNamespace, err := GetWatchNamespace(ext)
	if err != nil {
		return nil, err
	}
	return h.BundleToHelmChartConverter.ToHelmChart(source.FromFS(bundleFS), ext.Spec.Namespace, watchNamespace)
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

func (h *Helm) getReleaseState(ctx context.Context, cl helmclient.ActionInterface, ext *ocv1.ClusterExtension, chrt *chart.Chart, values chartutil.Values, post postrender.PostRenderer) (*release.Release, *release.Release, string, error) {
	logger := log.FromContext(ctx)
	currentRelease, err := cl.Get(ext.GetName())

	// if a release is pending at this point, that means that a helm action
	// (installation/upgrade) we were attempting was likely interrupted in-flight.
	// Pending release would leave us in reconciliation error loop because helm
	// wouldn't be able to progress automatically from it.
	//
	// one of the workarounds is to try and remove helm metadata relating to
	// that pending release which should 'reset' its state communicated to helm
	// and the next reconciliation should be able to successfully pick up from here
	// for context see: https://github.com/helm/helm/issues/5595 and https://github.com/helm/helm/issues/7476
	// and the discussion in https://github.com/operator-framework/operator-controller/pull/1776
	if err == nil && currentRelease.Info.Status.IsPending() {
		if _, err = cl.Config().Releases.Delete(currentRelease.Name, currentRelease.Version); err != nil {
			return nil, nil, StateError, fmt.Errorf("failed removing interrupted release %q version %d metadata: %w", currentRelease.Name, currentRelease.Version, err)
		}
		// return error to try to detect proper state (installation/upgrade) at next reconciliation
		return nil, nil, StateError, fmt.Errorf("removed interrupted release %q version %d metadata", currentRelease.Name, currentRelease.Version)
	}

	if errors.Is(err, driver.ErrReleaseNotFound) {
		logger.V(4).Info("ClusterExtension dry-run install", "extension", ext.GetName())
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
		logger.V(4).Info("ClusterExtension dry-run upgrade", "extension", ext.GetName())
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
