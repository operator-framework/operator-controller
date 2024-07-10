package handler

import (
	"context"
	"io/fs"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"

	bd "github.com/operator-framework/operator-controller/internal/rukpak/bundledeployment"
)

type Handler interface {
	Handle(context.Context, fs.FS, *bd.BundleDeployment) (*chart.Chart, chartutil.Values, error)
}

type HandlerFunc func(context.Context, fs.FS, *bd.BundleDeployment) (*chart.Chart, chartutil.Values, error)

func (f HandlerFunc) Handle(ctx context.Context, fsys fs.FS, bd *bd.BundleDeployment) (*chart.Chart, chartutil.Values, error) {
	return f(ctx, fsys, bd)
}
