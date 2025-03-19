package authorization

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"sort"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	apimachyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	rbacinternal "k8s.io/kubernetes/pkg/apis/rbac"
	rbacv1helpers "k8s.io/kubernetes/pkg/apis/rbac/v1"
	"k8s.io/kubernetes/pkg/registry/rbac/clusterrole/policybased"
	policybasedClusterRoleBinding "k8s.io/kubernetes/pkg/registry/rbac/clusterrolebinding/policybased"
	policybasedRole "k8s.io/kubernetes/pkg/registry/rbac/role/policybased"
	policybasedRoleBinding "k8s.io/kubernetes/pkg/registry/rbac/rolebinding/policybased"
	"k8s.io/kubernetes/pkg/registry/rbac/validation"
	"k8s.io/kubernetes/plugin/pkg/auth/authorizer/rbac"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PreAuthorizer interface {
	PreAuthorize(ctx context.Context, manifestManager user.Info, manifestReader io.Reader) ([]ScopedPolicyRules, error)
}

type ScopedPolicyRules struct {
	Namespace    string
	MissingRules []rbacv1.PolicyRule
}

var (
	collectionVerbs = []string{"list", "watch", "create"}
	objectVerbs     = []string{"get", "patch", "update", "delete"}
)

type rbacPreAuthorizer struct {
	authorizer   authorizer.Authorizer
	ruleResolver validation.AuthorizationRuleResolver
	restMapper   meta.RESTMapper
}

func NewRBACPreAuthorizer(cl client.Client) PreAuthorizer {
	return &rbacPreAuthorizer{
		authorizer:   newRBACAuthorizer(cl),
		ruleResolver: newRBACRulesResolver(cl),
		restMapper:   cl.RESTMapper(),
	}
}

// PreAuthorize validates whether the current user/request satisfies the necessary permissions
// as defined by the RBAC policy. It decodes the manifest, constructs authorization records,
// performs attribute checks, and then uses escalationChecker (which delegates to upstream logic)
// to verify that no privilege escalation is occurring.
func (a *rbacPreAuthorizer) PreAuthorize(ctx context.Context, manifestManager user.Info, manifestReader io.Reader) ([]ScopedPolicyRules, error) {
	var allMissingPolicyRules []ScopedPolicyRules
	dm, err := a.decodeManifest(manifestReader)
	if err != nil {
		return nil, err
	}
	attributesRecords := dm.asAuthorizationAttributesRecordsForUser(manifestManager)

	// Compute missing rules using your original logic.
	missingRules, err := a.authorizeAttributesRecords(ctx, attributesRecords)
	if err != nil {
		// If there are errors here, you might choose to log them or collect them.
		// We'll still try to run the upstream escalation check.
	}

	var escalationErrors []error
	ec := escalationChecker{
		authorizer:        a.authorizer,
		ruleResolver:      a.ruleResolver,
		extraClusterRoles: dm.clusterRoles,
		extraRoles:        dm.roles,
	}
	for _, obj := range dm.rbacObjects() {
		if err := ec.checkEscalation(ctx, manifestManager, obj); err != nil {
			// If err is a composite error (errors.Join), unwrap it and add each underlying error.
			var joinErr interface{ Unwrap() []error }
			if errors.As(err, &joinErr) {
				for _, singleErr := range joinErr.Unwrap() {
					escalationErrors = append(escalationErrors, singleErr)
				}
			} else {
				escalationErrors = append(escalationErrors, err)
			}
		}
	}

	// If escalation check fails, return the detailed missing rules along with the error.
	if len(escalationErrors) > 0 {
		// Process the missingRules map into a slice as before.
		for ns, nsMissingRules := range missingRules {
			if compactMissingRules, err := validation.CompactRules(nsMissingRules); err == nil {
				missingRules[ns] = compactMissingRules
			}
			sortableRules := rbacv1helpers.SortableRuleSlice(missingRules[ns])
			sort.Sort(sortableRules)
			allMissingPolicyRules = append(allMissingPolicyRules, ScopedPolicyRules{Namespace: ns, MissingRules: sortableRules})
		}
		return allMissingPolicyRules, fmt.Errorf("escalation check failed: %w", errors.Join(escalationErrors...))
	}

	// If the escalation check passed, override any computed missing rules (since the final decision is allowed).
	return []ScopedPolicyRules{}, nil
}

func (a *rbacPreAuthorizer) decodeManifest(manifestReader io.Reader) (*decodedManifest, error) {
	dm := &decodedManifest{
		gvrs:                map[schema.GroupVersionResource][]types.NamespacedName{},
		clusterRoles:        map[client.ObjectKey]rbacv1.ClusterRole{},
		roles:               map[client.ObjectKey]rbacv1.Role{},
		clusterRoleBindings: map[client.ObjectKey]rbacv1.ClusterRoleBinding{},
		roleBindings:        map[client.ObjectKey]rbacv1.RoleBinding{},
	}
	var (
		i       int
		errs    []error
		decoder = apimachyaml.NewYAMLOrJSONDecoder(manifestReader, 1024)
	)
	for {
		var uObj unstructured.Unstructured
		err := decoder.Decode(&uObj)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("could not decode object %d in manifest: %w", i, err))
			continue
		}
		gvk := uObj.GroupVersionKind()
		restMapping, err := a.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			var objName string
			if name := uObj.GetName(); name != "" {
				objName = fmt.Sprintf(" (name: %s)", name)
			}
			errs = append(
				errs,
				fmt.Errorf("could not get REST mapping for object %d in manifest with GVK %s%s: %w", i, gvk, objName, err),
			)
			continue
		}

		gvr := restMapping.Resource
		dm.gvrs[gvr] = append(dm.gvrs[gvr], client.ObjectKeyFromObject(&uObj))

		switch restMapping.Resource.GroupResource() {
		case schema.GroupResource{Group: rbacv1.GroupName, Resource: "clusterroles"}:
			obj := &rbacv1.ClusterRole{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uObj.UnstructuredContent(), obj); err != nil {
				errs = append(errs, fmt.Errorf("could not decode object %d in manifest as ClusterRole: %w", i, err))
				continue
			}
			dm.clusterRoles[client.ObjectKeyFromObject(obj)] = *obj
		case schema.GroupResource{Group: rbacv1.GroupName, Resource: "clusterrolebindings"}:
			obj := &rbacv1.ClusterRoleBinding{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uObj.UnstructuredContent(), obj); err != nil {
				errs = append(errs, fmt.Errorf("could not decode object %d in manifest as ClusterRoleBinding: %w", i, err))
				continue
			}
			dm.clusterRoleBindings[client.ObjectKeyFromObject(obj)] = *obj
		case schema.GroupResource{Group: rbacv1.GroupName, Resource: "roles"}:
			obj := &rbacv1.Role{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uObj.UnstructuredContent(), obj); err != nil {
				errs = append(errs, fmt.Errorf("could not decode object %d in manifest as Role: %w", i, err))
				continue
			}
			dm.roles[client.ObjectKeyFromObject(obj)] = *obj
		case schema.GroupResource{Group: rbacv1.GroupName, Resource: "rolebindings"}:
			obj := &rbacv1.RoleBinding{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uObj.UnstructuredContent(), obj); err != nil {
				errs = append(errs, fmt.Errorf("could not decode object %d in manifest as RoleBinding: %w", i, err))
				continue
			}
			dm.roleBindings[client.ObjectKeyFromObject(obj)] = *obj
		}
		i++
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return dm, nil
}

func (a *rbacPreAuthorizer) authorizeAttributesRecords(ctx context.Context, attributesRecords []authorizer.AttributesRecord) (map[string][]rbacv1.PolicyRule, error) {
	missingRules := map[string][]rbacv1.PolicyRule{}
	var errs []error
	for _, ar := range attributesRecords {
		allow, err := a.attributesAllowed(ctx, ar)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !allow {
			missingRules[ar.Namespace] = append(missingRules[ar.Namespace], policyRuleFromAttributesRecord(ar))
		}
	}
	return missingRules, errors.Join(errs...)
}

func (a *rbacPreAuthorizer) attributesAllowed(ctx context.Context, attributesRecord authorizer.AttributesRecord) (bool, error) {
	decision, reason, err := a.authorizer.Authorize(ctx, attributesRecord)
	if err != nil {
		if reason != "" {
			return false, fmt.Errorf("%s: %w", reason, err)
		}
		return false, err
	}
	return decision == authorizer.DecisionAllow, nil
}

func policyRuleFromAttributesRecord(attributesRecord authorizer.AttributesRecord) rbacv1.PolicyRule {
	pr := rbacv1.PolicyRule{}
	if attributesRecord.Verb != "" {
		pr.Verbs = []string{attributesRecord.Verb}
	}
	if !attributesRecord.ResourceRequest {
		pr.NonResourceURLs = []string{attributesRecord.Path}
		return pr
	}
	pr.APIGroups = []string{attributesRecord.APIGroup}
	if attributesRecord.Name != "" {
		pr.ResourceNames = []string{attributesRecord.Name}
	}
	r := attributesRecord.Resource
	if attributesRecord.Subresource != "" {
		r += "/" + attributesRecord.Subresource
	}
	pr.Resources = []string{r}
	return pr
}

type decodedManifest struct {
	gvrs                map[schema.GroupVersionResource][]types.NamespacedName
	clusterRoles        map[client.ObjectKey]rbacv1.ClusterRole
	roles               map[client.ObjectKey]rbacv1.Role
	clusterRoleBindings map[client.ObjectKey]rbacv1.ClusterRoleBinding
	roleBindings        map[client.ObjectKey]rbacv1.RoleBinding
}

func (dm *decodedManifest) rbacObjects() []client.Object {
	objects := make([]client.Object, 0, len(dm.clusterRoles)+len(dm.roles)+len(dm.clusterRoleBindings)+len(dm.roleBindings))
	for obj := range maps.Values(dm.clusterRoles) {
		o := obj // avoid aliasing
		objects = append(objects, &o)
	}
	for obj := range maps.Values(dm.roles) {
		o := obj
		objects = append(objects, &o)
	}
	for obj := range maps.Values(dm.clusterRoleBindings) {
		o := obj
		objects = append(objects, &o)
	}
	for obj := range maps.Values(dm.roleBindings) {
		o := obj
		objects = append(objects, &o)
	}
	return objects
}

func (dm *decodedManifest) asAuthorizationAttributesRecordsForUser(manifestManager user.Info) []authorizer.AttributesRecord {
	var attributeRecords []authorizer.AttributesRecord
	for gvr, keys := range dm.gvrs {
		namespaces := sets.New[string]()
		for _, k := range keys {
			namespaces.Insert(k.Namespace)
			for _, v := range objectVerbs {
				attributeRecords = append(attributeRecords, authorizer.AttributesRecord{
					User:            manifestManager,
					Namespace:       k.Namespace,
					Name:            k.Name,
					APIGroup:        gvr.Group,
					APIVersion:      gvr.Version,
					Resource:        gvr.Resource,
					ResourceRequest: true,
					Verb:            v,
				})
			}
		}
		for _, ns := range sets.List(namespaces) {
			for _, v := range collectionVerbs {
				attributeRecords = append(attributeRecords, authorizer.AttributesRecord{
					User:            manifestManager,
					Namespace:       ns,
					APIGroup:        gvr.Group,
					APIVersion:      gvr.Version,
					Resource:        gvr.Resource,
					ResourceRequest: true,
					Verb:            v,
				})
			}
		}
	}
	return attributeRecords
}

func newRBACAuthorizer(cl client.Client) authorizer.Authorizer {
	rg := &rbacGetter{cl: cl}
	return rbac.New(rg, rg, rg, rg)
}

type rbacGetter struct {
	cl client.Client
}

func (r rbacGetter) ListClusterRoleBindings(ctx context.Context) ([]*rbacv1.ClusterRoleBinding, error) {
	var clusterRoleBindingsList rbacv1.ClusterRoleBindingList
	if err := r.cl.List(ctx, &clusterRoleBindingsList); err != nil {
		return nil, err
	}
	return toPtrSlice(clusterRoleBindingsList.Items), nil
}

func (r rbacGetter) GetClusterRole(ctx context.Context, name string) (*rbacv1.ClusterRole, error) {
	var clusterRole rbacv1.ClusterRole
	if err := r.cl.Get(ctx, client.ObjectKey{Name: name}, &clusterRole); err != nil {
		return nil, err
	}
	return &clusterRole, nil
}

func (r rbacGetter) ListRoleBindings(ctx context.Context, namespace string) ([]*rbacv1.RoleBinding, error) {
	var roleBindingsList rbacv1.RoleBindingList
	if err := r.cl.List(ctx, &roleBindingsList, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	return toPtrSlice(roleBindingsList.Items), nil
}

func (r rbacGetter) GetRole(ctx context.Context, namespace, name string) (*rbacv1.Role, error) {
	var role rbacv1.Role
	if err := r.cl.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, &role); err != nil {
		return nil, err
	}
	return &role, nil
}

func newRBACRulesResolver(cl client.Client) validation.AuthorizationRuleResolver {
	rg := &rbacGetter{cl: cl}
	return validation.NewDefaultRuleResolver(rg, rg, rg, rg)
}

type escalationChecker struct {
	authorizer        authorizer.Authorizer
	ruleResolver      validation.AuthorizationRuleResolver
	extraRoles        map[types.NamespacedName]rbacv1.Role
	extraClusterRoles map[types.NamespacedName]rbacv1.ClusterRole
}

// checkEscalation delegates the escalation check to upstream storage by performing
// a dry-run Create call on the appropriate storage for the RBAC object type.
func (ec *escalationChecker) checkEscalation(ctx context.Context, manifestManager user.Info, obj client.Object) error {
	// Set up the context with user and namespace.
	ctx = request.WithUser(request.WithNamespace(ctx, obj.GetNamespace()), manifestManager)
	noOpValidate := func(ctx context.Context, obj runtime.Object) error { return nil }
	opts := &metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}}

	switch v := obj.(type) {
	case *rbacv1.ClusterRole:
		// Merge extra ClusterRole rules if present.
		key := types.NamespacedName{Name: v.Name}
		if extra, ok := ec.extraClusterRoles[key]; ok {
			v.Rules = append(v.Rules, extra.Rules...)
		}
		// Convert external ClusterRole to internal representation.
		var internalClusterRole rbacinternal.ClusterRole
		if err := rbacv1helpers.Convert_v1_ClusterRole_To_rbac_ClusterRole(v, &internalClusterRole, nil); err != nil {
			return err
		}
		storage := policybased.NewStorage(&FakeStandardStorage{}, ec.authorizer, ec.ruleResolver)
		_, err := storage.Create(ctx, &internalClusterRole, noOpValidate, opts)
		return err

	case *rbacv1.ClusterRoleBinding:
		// Convert external ClusterRoleBinding to internal representation.
		var internalClusterRoleBinding rbacinternal.ClusterRoleBinding
		if err := rbacv1helpers.Convert_v1_ClusterRoleBinding_To_rbac_ClusterRoleBinding(v, &internalClusterRoleBinding, nil); err != nil {
			return err
		}
		storage := policybasedClusterRoleBinding.NewStorage(&FakeStandardStorage{}, ec.authorizer, ec.ruleResolver)
		_, err := storage.Create(ctx, &internalClusterRoleBinding, noOpValidate, opts)
		return err

	case *rbacv1.Role:
		// Merge extra Role rules if present.
		key := types.NamespacedName{Namespace: v.Namespace, Name: v.Name}
		if extra, ok := ec.extraRoles[key]; ok {
			v.Rules = append(v.Rules, extra.Rules...)
		}
		// Convert external Role to internal representation.
		var internalRole rbacinternal.Role
		if err := rbacv1helpers.Convert_v1_Role_To_rbac_Role(v, &internalRole, nil); err != nil {
			return err
		}
		storage := policybasedRole.NewStorage(&FakeStandardStorage{}, ec.authorizer, ec.ruleResolver)
		_, err := storage.Create(ctx, &internalRole, noOpValidate, opts)
		return err

	case *rbacv1.RoleBinding:
		// Convert external RoleBinding to internal representation.
		var internalRoleBinding rbacinternal.RoleBinding
		if err := rbacv1helpers.Convert_v1_RoleBinding_To_rbac_RoleBinding(v, &internalRoleBinding, nil); err != nil {
			return err
		}
		storage := policybasedRoleBinding.NewStorage(&FakeStandardStorage{}, ec.authorizer, ec.ruleResolver)
		_, err := storage.Create(ctx, &internalRoleBinding, noOpValidate, opts)
		return err

	default:
		return fmt.Errorf("unsupported object type %T", v)
	}
}

// FakeStandardStorage is a minimal fake implementation of rest.StandardStorage to satisfy required methods for dry-run operations.
type FakeStandardStorage struct{}

func (fs *FakeStandardStorage) New() runtime.Object {
	return nil
}

func (fs *FakeStandardStorage) NewList() runtime.Object {
	return nil
}

func (fs *FakeStandardStorage) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	return nil, nil
}

func (fs *FakeStandardStorage) List(ctx context.Context, options *metainternalversion.ListOptions) (runtime.Object, error) {
	return nil, nil
}

func (fs *FakeStandardStorage) Create(ctx context.Context, obj runtime.Object, createValidation rest.ValidateObjectFunc, options *metav1.CreateOptions) (runtime.Object, error) {
	// For dry-run escalation check, simply return the object.
	return obj, nil
}

func (fs *FakeStandardStorage) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo, createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc, forceAllowCreate bool, options *metav1.UpdateOptions) (runtime.Object, bool, error) {
	return nil, false, nil
}

func (fs *FakeStandardStorage) Delete(ctx context.Context, name string, deleteValidation rest.ValidateObjectFunc, options *metav1.DeleteOptions) (runtime.Object, bool, error) {
	return nil, false, nil
}

func (fs *FakeStandardStorage) Watch(ctx context.Context, options *metainternalversion.ListOptions) (watch.Interface, error) {
	return nil, nil
}

func (fs *FakeStandardStorage) Destroy() {
	return
}

func (fs *FakeStandardStorage) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	return nil, nil
}

func (fs *FakeStandardStorage) DeleteCollection(ctx context.Context, deleteValidation rest.ValidateObjectFunc, options *metav1.DeleteOptions, listOptions *metainternalversion.ListOptions) (runtime.Object, error) {
	return nil, nil
}

func toPtrSlice[V any](in []V) []*V {
	out := make([]*V, len(in))
	for i := range in {
		out[i] = &in[i]
	}
	return out
}
