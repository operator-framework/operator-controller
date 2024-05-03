package handler

import (
	"context"
	"io/fs"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

type Handler interface {
	Handle(context.Context, fs.FS, *ocv1alpha1.ClusterExtension) (*chart.Chart, chartutil.Values, error)
}

type HandlerFunc func(context.Context, fs.FS, *ocv1alpha1.ClusterExtension) (*chart.Chart, chartutil.Values, error)

func (f HandlerFunc) Handle(ctx context.Context, fsys fs.FS, bd *ocv1alpha1.ClusterExtension) (*chart.Chart, chartutil.Values, error) {
	return f(ctx, fsys, bd)
}
