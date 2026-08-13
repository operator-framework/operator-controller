package applier

import (
	"context"
	"fmt"
	"io/fs"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

type OrbOperator struct {
	Client     client.Client
	Scheme     *runtime.Scheme
	Generator  CODGenerator
	Preflights []Preflight
	FieldOwner string
}

func (o *OrbOperator) Apply(ctx context.Context, contentFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (bool, string, error) {
	l := log.FromContext(ctx)

	cod, err := o.Generator.GenerateCOD(ctx, contentFS, ext, objectLabels, revisionAnnotations)
	if err != nil {
		return false, "", fmt.Errorf("generating COD: %w", err)
	}

	codYAML, err := yaml.Marshal(cod)
	if err != nil {
		return false, "", fmt.Errorf("marshaling COD to YAML: %w", err)
	}
	l.Info("generated ClusterObjectDeployment", "cod", string(codYAML))
	return false, "", nil
}
