package applier

import (
	"context"
	"io/fs"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

type OrbOperator struct {
	Client     client.Client
	Scheme     *runtime.Scheme
	Preflights []Preflight
	FieldOwner string
}

func (o *OrbOperator) Apply(ctx context.Context, _ fs.FS, _ *ocv1.ClusterExtension, _, _ map[string]string) (bool, string, error) {
	log.FromContext(ctx).Info("OrbOperatorRuntime applier not yet implemented")
	return false, "", nil
}
