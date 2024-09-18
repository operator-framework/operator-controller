package permissions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	kappperms "carvel.dev/kapp/pkg/kapp/permissions"
	kappres "carvel.dev/kapp/pkg/kapp/resources"
	"helm.sh/helm/v3/pkg/release"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/rukpak/util"
)

type RestConfigMapper func(context.Context, client.Object, *rest.Config) (*rest.Config, error)

type Preflight struct {
	rcm     RestConfigMapper
	baseCfg *rest.Config
	mapper  meta.RESTMapper
}

func NewPreflight(rcm RestConfigMapper, baseCfg *rest.Config, mapper meta.RESTMapper) *Preflight {
	return &Preflight{
		rcm:     rcm,
		baseCfg: baseCfg,
		mapper:  mapper,
	}
}

func (p *Preflight) Install(ctx context.Context, rel *release.Release, ext *ocv1alpha1.ClusterExtension) error {
	return p.runPreflight(ctx, rel, ext)
}

func (p *Preflight) Upgrade(ctx context.Context, rel *release.Release, ext *ocv1alpha1.ClusterExtension) error {
	return p.runPreflight(ctx, rel, ext)
}

func (p *Preflight) runPreflight(ctx context.Context, rel *release.Release, ext *ocv1alpha1.ClusterExtension) error {
	if rel == nil {
		return nil
	}

	cfg, err := p.rcm(ctx, ext, p.baseCfg)
	if err != nil {
		return fmt.Errorf("getting config: %w", err)
	}

	kClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("getting kubernetes client: %w", err)
	}

	permissionValidator := kappperms.NewSelfSubjectRulesReviewValidator(kClient.AuthorizationV1().SelfSubjectRulesReviews())
	roleValidator := kappperms.NewRoleValidator(permissionValidator, p.mapper)
	bindingValidator := kappperms.NewBindingValidator(permissionValidator, kClient.RbacV1(), p.mapper)
	basicValidator := kappperms.NewBasicValidator(permissionValidator, p.mapper)

	validator := kappperms.NewCompositeValidator(basicValidator, map[schema.GroupVersionKind]kappperms.Validator{
		rbacv1.SchemeGroupVersion.WithKind("Role"):               roleValidator,
		rbacv1.SchemeGroupVersion.WithKind("ClusterRole"):        roleValidator,
		rbacv1.SchemeGroupVersion.WithKind("RoleBinding"):        bindingValidator,
		rbacv1.SchemeGroupVersion.WithKind("ClusterRoleBinding"): bindingValidator,
	})

	relObjects, err := util.ManifestObjects(strings.NewReader(rel.Manifest), fmt.Sprintf("%s-release-manifest", rel.Name))
	if err != nil {
		return fmt.Errorf("parsing release %q objects: %w", rel.Name, err)
	}

	verbsToCheck := []string{"create", "update", "patch", "delete", "get", "list", "watch"}
	errs := []error{}
	for _, obj := range relObjects {
		bytes, err := json.Marshal(obj)
		if err != nil {
			return fmt.Errorf("marshalling object %v: %w", obj, err)
		}
		resource, err := kappres.NewResourceFromBytes(bytes)
		if err != nil {
			return fmt.Errorf("converting bytes to resource: %w", err)
		}

		for _, verb := range verbsToCheck {
			err := validator.Validate(ctx, resource, verb)
			if err != nil {
				errs = append(errs, err)
			}
		}
	}

	if len(errs) > 0 {
		errs = append([]error{errors.New("validating permissions to install and manage resources")}, errs...)
	}

	return errors.Join(errs...)
}
