package registry

import (
	"context"
	"fmt"
	"io/fs"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/operator-framework/operator-controller/internal/rukpak/bundledeployment"
	"github.com/operator-framework/operator-controller/internal/rukpak/convert"
	"github.com/operator-framework/operator-controller/internal/rukpak/provisioner/plain"
)

const (
	// ProvisionerID is the unique registry provisioner ID
	ProvisionerID = "core-rukpak-io-registry"
)

func HandleBundleDeployment(ctx context.Context, fsys fs.FS, bd *bundledeployment.BundleDeployment) (*chart.Chart, chartutil.Values, error) {
	plainFS, err := convert.RegistryV1ToPlain(fsys, bd.Spec.InstallNamespace, []string{metav1.NamespaceAll})
	if err != nil {
		return nil, nil, fmt.Errorf("convert registry+v1 bundle to plain+v0 bundle: %v", err)
	}
	return plain.HandleBundleDeployment(ctx, plainFS, bd)
}
