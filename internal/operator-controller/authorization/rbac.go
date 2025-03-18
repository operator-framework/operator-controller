package authorization

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"sort"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	apimachyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/endpoints/request"
	rbacinternal "k8s.io/kubernetes/pkg/apis/rbac"
	rbacv1helpers "k8s.io/kubernetes/pkg/apis/rbac/v1"
	rbacregistry "k8s.io/kubernetes/pkg/registry/rbac"
	"k8s.io/kubernetes/pkg/registry/rbac/validation"
	rbac "k8s.io/kubernetes/plugin/pkg/auth/authorizer/rbac"
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
// as defined by the RBAC policy. It examines the user’s roles, resource identifiers, and
// the intended action to determine if the operation is allowed.
//
// Return Value:
//   - nil: indicates that the authorization check passed and the operation is permitted.
//   - non-nil error: indicates that the authorization failed (either due to insufficient permissions
//     or an error encountered during the check), the error provides a slice of several failures at once.
func (a *rbacPreAuthorizer) PreAuthorize(ctx context.Context, manifestManager user.Info, manifestReader io.Reader) ([]ScopedPolicyRules, error) {
	var allMissingPolicyRules = []ScopedPolicyRules{}
	dm, err := a.decodeManifest(manifestReader)
	if err != nil {
		return nil, err
	}
	attributesRecords := dm.asAuthorizationAttributesRecordsForUser(manifestManager)

	var preAuthEvaluationErrors []error
	missingRules, err := a.authorizeAttributesRecords(ctx, attributesRecords)
	if err != nil {
		preAuthEvaluationErrors = append(preAuthEvaluationErrors, err)
	}

	ec := escalationChecker{
		authorizer:        a.authorizer,
		ruleResolver:      a.ruleResolver,
		extraClusterRoles: dm.clusterRoles,
		extraRoles:        dm.roles,
	}

	for _, obj := range dm.rbacObjects() {
		if err := ec.checkEscalation(ctx, manifestManager, obj); err != nil {
			// In Kubernetes 1.32.2 the specialized PrivilegeEscalationError is gone.
			// Instead, we simply collect the error.
			preAuthEvaluationErrors = append(preAuthEvaluationErrors, err)
		}
	}

	for ns, nsMissingRules := range missingRules {
		// NOTE: Although CompactRules is defined to return an error, its current implementation
		// never produces a non-nil error. This is because all operations within the function are
		// designed to succeed under current conditions. In the future, if more complex rule validations
		// are introduced, this behavior may change and proper error handling will be required.
		if compactMissingRules, err := validation.CompactRules(nsMissingRules); err == nil {
			missingRules[ns] = compactMissingRules
		}
		sortableRules := rbacv1helpers.SortableRuleSlice(missingRules[ns])
		sort.Sort(sortableRules)
		allMissingPolicyRules = append(allMissingPolicyRules, ScopedPolicyRules{Namespace: ns, MissingRules: sortableRules})
	}

	if len(preAuthEvaluationErrors) > 0 {
		return allMissingPolicyRules, fmt.Errorf("authorization evaluation errors: %w", errors.Join(preAuthEvaluationErrors...))
	}
	return allMissingPolicyRules, nil
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
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return dm, nil
}

func (a *rbacPreAuthorizer) authorizeAttributesRecords(ctx context.Context, attributesRecords []authorizer.AttributesRecord) (map[string][]rbacv1.PolicyRule, error) {
	var (
		missingRules = map[string][]rbacv1.PolicyRule{}
		errs         []error
	)
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
		objects = append(objects, &obj)
	}
	for obj := range maps.Values(dm.roles) {
		objects = append(objects, &obj)
	}
	for obj := range maps.Values(dm.clusterRoleBindings) {
		objects = append(objects, &obj)
	}
	for obj := range maps.Values(dm.roleBindings) {
		objects = append(objects, &obj)
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

func (ec *escalationChecker) checkEscalation(ctx context.Context, manifestManager user.Info, obj client.Object) error {
	ctx = request.WithUser(request.WithNamespace(ctx, obj.GetNamespace()), manifestManager)
	switch v := obj.(type) {
	case *rbacv1.Role:
		ctx = request.WithRequestInfo(ctx, &request.RequestInfo{APIGroup: rbacv1.GroupName, Resource: "roles", IsResourceRequest: true})
		return ec.checkRoleEscalation(ctx, v)
	case *rbacv1.RoleBinding:
		ctx = request.WithRequestInfo(ctx, &request.RequestInfo{APIGroup: rbacv1.GroupName, Resource: "rolebindings", IsResourceRequest: true})
		return ec.checkRoleBindingEscalation(ctx, v)
	case *rbacv1.ClusterRole:
		ctx = request.WithRequestInfo(ctx, &request.RequestInfo{APIGroup: rbacv1.GroupName, Resource: "clusterroles", IsResourceRequest: true})
		return ec.checkClusterRoleEscalation(ctx, v)
	case *rbacv1.ClusterRoleBinding:
		ctx = request.WithRequestInfo(ctx, &request.RequestInfo{APIGroup: rbacv1.GroupName, Resource: "clusterrolebindings", IsResourceRequest: true})
		return ec.checkClusterRoleBindingEscalation(ctx, v)
	default:
		return fmt.Errorf("unknown object type %T", v)
	}
}

func (ec *escalationChecker) checkClusterRoleEscalation(ctx context.Context, clusterRole *rbacv1.ClusterRole) error {
	if rbacregistry.EscalationAllowed(ctx) || rbacregistry.RoleEscalationAuthorized(ctx, ec.authorizer) {
		return nil
	}

	// to set the aggregation rule, since it can gather anything, requires * on *.*
	if hasAggregationRule(clusterRole) {
		if err := validation.ConfirmNoEscalation(ctx, ec.ruleResolver, fullAuthority); err != nil {
			return fmt.Errorf("must have cluster-admin privileges to use an aggregationRule: %w", err)
		}
	}

	if err := validation.ConfirmNoEscalation(ctx, ec.ruleResolver, clusterRole.Rules); err != nil {
		return err
	}
	return nil
}

func (ec *escalationChecker) checkClusterRoleBindingEscalation(ctx context.Context, clusterRoleBinding *rbacv1.ClusterRoleBinding) error {
	if rbacregistry.EscalationAllowed(ctx) {
		return nil
	}

	roleRef := rbacinternal.RoleRef{}
	err := rbacv1helpers.Convert_v1_RoleRef_To_rbac_RoleRef(&clusterRoleBinding.RoleRef, &roleRef, nil)
	if err != nil {
		return err
	}

	if rbacregistry.BindingAuthorized(ctx, roleRef, metav1.NamespaceNone, ec.authorizer) {
		return nil
	}

	rules, err := ec.ruleResolver.GetRoleReferenceRules(ctx, clusterRoleBinding.RoleRef, metav1.NamespaceNone)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	if clusterRoleBinding.RoleRef.Kind == "ClusterRole" {
		if manifestClusterRole, ok := ec.extraClusterRoles[types.NamespacedName{Name: clusterRoleBinding.RoleRef.Name}]; ok {
			rules = append(rules, manifestClusterRole.Rules...)
		}
	}

	if err := validation.ConfirmNoEscalation(ctx, ec.ruleResolver, rules); err != nil {
		return err
	}
	return nil
}

func (ec *escalationChecker) checkRoleEscalation(ctx context.Context, role *rbacv1.Role) error {
	if rbacregistry.EscalationAllowed(ctx) || rbacregistry.RoleEscalationAuthorized(ctx, ec.authorizer) {
		return nil
	}

	rules := role.Rules
	if err := validation.ConfirmNoEscalation(ctx, ec.ruleResolver, rules); err != nil {
		return err
	}
	return nil
}

func (ec *escalationChecker) checkRoleBindingEscalation(ctx context.Context, roleBinding *rbacv1.RoleBinding) error {
	if rbacregistry.EscalationAllowed(ctx) {
		return nil
	}

	roleRef := rbacinternal.RoleRef{}
	err := rbacv1helpers.Convert_v1_RoleRef_To_rbac_RoleRef(&roleBinding.RoleRef, &roleRef, nil)
	if err != nil {
		return err
	}
	if rbacregistry.BindingAuthorized(ctx, roleRef, roleBinding.Namespace, ec.authorizer) {
		return nil
	}

	rules, err := ec.ruleResolver.GetRoleReferenceRules(ctx, roleBinding.RoleRef, roleBinding.Namespace)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	switch roleRef.Kind {
	case "ClusterRole":
		if manifestClusterRole, ok := ec.extraClusterRoles[types.NamespacedName{Name: roleBinding.RoleRef.Name}]; ok {
			rules = append(rules, manifestClusterRole.Rules...)
		}
	case "Role":
		if manifestRole, ok := ec.extraRoles[types.NamespacedName{Namespace: roleBinding.Namespace, Name: roleBinding.RoleRef.Name}]; ok {
			rules = append(rules, manifestRole.Rules...)
		}
	}

	if err := validation.ConfirmNoEscalation(ctx, ec.ruleResolver, rules); err != nil {
		return err
	}
	return nil
}

var fullAuthority = []rbacv1.PolicyRule{
	{Verbs: []string{"*"}, APIGroups: []string{"*"}, Resources: []string{"*"}},
	{Verbs: []string{"*"}, NonResourceURLs: []string{"*"}},
}

func hasAggregationRule(clusterRole *rbacv1.ClusterRole) bool {
	// Currently, an aggregation rule is considered present only if it has one or more selectors.
	// An empty slice of ClusterRoleSelectors means no selectors were provided,
	// which does NOT imply "match all."
	return clusterRole.AggregationRule != nil && len(clusterRole.AggregationRule.ClusterRoleSelectors) > 0
}

func toPtrSlice[V any](in []V) []*V {
	out := make([]*V, len(in))
	for i := range in {
		out[i] = &in[i]
	}
	return out
}
