package applier

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/postrender"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apimachyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/rukpak/convert"
	"github.com/operator-framework/operator-controller/internal/rukpak/preflights/crdupgradesafety"
	"github.com/operator-framework/operator-controller/internal/rukpak/util"
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

type Helm struct {
	ActionClientGetter helmclient.ActionClientGetter
	Preflights         []Preflight
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

func (h *Helm) Apply(ctx context.Context, contentFS fs.FS, ext *ocv1.ClusterExtension, objectLabels map[string]string, storageLabels map[string]string) ([]client.Object, string, error) {
	chrt, err := convert.RegistryV1ToHelmChart(ctx, contentFS, ext.Spec.Namespace, []string{corev1.NamespaceAll})
	if err != nil {
		return nil, "", err
	}
	values := chartutil.Values{}

	ac, err := h.ActionClientGetter.ActionClientFor(ctx, ext)
	if err != nil {
		return nil, "", err
	}

	post := &postrenderer{
		labels: objectLabels,
	}

	rel, desiredRel, state, err := h.getReleaseState(ac, ext, chrt, values, post)
	if err != nil {
		return nil, "", err
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
