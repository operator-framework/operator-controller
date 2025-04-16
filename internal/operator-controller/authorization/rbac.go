package authorization

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
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

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

type PreAuthorizer interface {
	PreAuthorize(ctx context.Context, ext *ocv1.ClusterExtension, manifestReader io.Reader) ([]ScopedPolicyRules, error)
}

type ScopedPolicyRules struct {
	Namespace    string
	MissingRules []rbacv1.PolicyRule
}

var objectVerbs = []string{"get", "patch", "update", "delete"}

// Here we are splitting collection verbs based on required scope
// NB: this split is tightly coupled to the requirements of the contentmanager, specifically
// its need for cluster-scoped list/watch permissions.
// TODO: We are accepting this coupling for now, but plan to decouple
// TODO: link for above https://github.com/operator-framework/operator-controller/issues/1911
var namespacedCollectionVerbs = []string{"create"}
var clusterCollectionVerbs = []string{"list", "watch"}

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
//   - non-nil error: indicates that an error occurred during the permission evaluation process
//     (for example, a failure decoding the manifest or other internal issues). If the evaluation
//     completes successfully but identifies missing rules, then a nil error is returned along with
//     the list (or slice) of missing rules. Note that in some cases the error may encapsulate multiple
//     evaluation failures
func (a *rbacPreAuthorizer) PreAuthorize(ctx context.Context, ext *ocv1.ClusterExtension, manifestReader io.Reader) ([]ScopedPolicyRules, error) {
	dm, err := a.decodeManifest(manifestReader)
	if err != nil {
		return nil, err
	}
	manifestManager := &user.DefaultInfo{Name: fmt.Sprintf("system:serviceaccount:%s:%s", ext.Spec.Namespace, ext.Spec.ServiceAccount.Name)}
	attributesRecords := dm.asAuthorizationAttributesRecordsForUser(manifestManager, ext)

	var preAuthEvaluationErrors []error
	missingRules, err := a.authorizeAttributesRecords(ctx, attributesRecords)
	if err != nil {
		preAuthEvaluationErrors = append(preAuthEvaluationErrors, err)
	}

	ec := a.escalationCheckerFor(dm)

	var parseErrors []error
	for _, obj := range dm.rbacObjects() {
		if err := ec.checkEscalation(ctx, manifestManager, obj); err != nil {
			result, err := parseEscalationErrorForMissingRules(err)
			missingRules[obj.GetNamespace()] = append(missingRules[obj.GetNamespace()], result.MissingRules...)
			preAuthEvaluationErrors = append(preAuthEvaluationErrors, result.ResolutionErrors)
			parseErrors = append(parseErrors, err)
		}
	}
	allMissingPolicyRules := make([]ScopedPolicyRules, 0, len(missingRules))

	for ns, nsMissingRules := range missingRules {
		// NOTE: Although CompactRules is defined to return an error, its current implementation
		// never produces a non-nil error. This is because all operations within the function are
		// designed to succeed under current conditions. In the future, if more complex rule validations
		// are introduced, this behavior may change and proper error handling will be required.
		if compactMissingRules, err := validation.CompactRules(nsMissingRules); err == nil {
			missingRules[ns] = compactMissingRules
		}

		missingRulesWithDeduplicatedVerbs := make([]rbacv1.PolicyRule, 0, len(missingRules[ns]))
		for _, rule := range missingRules[ns] {
			verbSet := sets.New[string](rule.Verbs...)
			if verbSet.Has("*") {
				rule.Verbs = []string{"*"}
			} else {
				rule.Verbs = sets.List(verbSet)
			}
			missingRulesWithDeduplicatedVerbs = append(missingRulesWithDeduplicatedVerbs, rule)
		}

		sortableRules := rbacv1helpers.SortableRuleSlice(missingRulesWithDeduplicatedVerbs)

		sort.Sort(sortableRules)
		allMissingPolicyRules = append(allMissingPolicyRules, ScopedPolicyRules{Namespace: ns, MissingRules: sortableRules})
	}

	// sort allMissingPolicyRules alphabetically by namespace
	slices.SortFunc(allMissingPolicyRules, func(a, b ScopedPolicyRules) int {
		return strings.Compare(a.Namespace, b.Namespace)
	})

	var errs []error
	if parseErr := errors.Join(parseErrors...); parseErr != nil {
		errs = append(errs, fmt.Errorf("failed to parse escalation check error strings: %v", parseErr))
	}
	if len(preAuthEvaluationErrors) > 0 {
		errs = append(errs, fmt.Errorf("failed to resolve or evaluate permissions: %v", errors.Join(preAuthEvaluationErrors...)))
	}
	if len(errs) > 0 {
		return allMissingPolicyRules, fmt.Errorf("missing rules may be incomplete: %w", errors.Join(errs...))
	}
	return allMissingPolicyRules, nil
}

func (a *rbacPreAuthorizer) escalationCheckerFor(dm *decodedManifest) escalationChecker {
	ec := escalationChecker{
		authorizer:        a.authorizer,
		ruleResolver:      a.ruleResolver,
		extraClusterRoles: dm.clusterRoles,
		extraRoles:        dm.roles,
	}
	return ec
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

func (dm *decodedManifest) asAuthorizationAttributesRecordsForUser(manifestManager user.Info, ext *ocv1.ClusterExtension) []authorizer.AttributesRecord {
	var attributeRecords []authorizer.AttributesRecord

	for gvr, keys := range dm.gvrs {
		namespaces := sets.New[string]()
		for _, k := range keys {
			namespaces.Insert(k.Namespace)
			// generate records for object-specific verbs (get, update, patch, delete) in their respective namespaces
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
		// generate records for namespaced collection verbs (create) for each relevant namespace
		for _, ns := range sets.List(namespaces) {
			for _, v := range namespacedCollectionVerbs {
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
		// generate records for cluster-scoped collection verbs (list, watch) required by contentmanager
		for _, v := range clusterCollectionVerbs {
			attributeRecords = append(attributeRecords, authorizer.AttributesRecord{
				User:            manifestManager,
				Namespace:       corev1.NamespaceAll, // check cluster scope
				APIGroup:        gvr.Group,
				APIVersion:      gvr.Version,
				Resource:        gvr.Resource,
				ResourceRequest: true,
				Verb:            v,
			})
		}

		for _, verb := range []string{"update"} {
			attributeRecords = append(attributeRecords, authorizer.AttributesRecord{
				User:            manifestManager,
				Name:            ext.Name,
				APIGroup:        ext.GroupVersionKind().Group,
				APIVersion:      ext.GroupVersionKind().Version,
				Resource:        "clusterextensions/finalizers",
				ResourceRequest: true,
				Verb:            verb,
			})
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

var (
	errRegex  = regexp.MustCompile(`(?s)^user ".*" \(groups=.*\) is attempting to grant RBAC permissions not currently held:\n([^;]+)(?:; resolution errors: (.*))?$`)
	ruleRegex = regexp.MustCompile(`{([^}]*)}`)
	itemRegex = regexp.MustCompile(`"[^"]*"`)
)

type parseResult struct {
	MissingRules     []rbacv1.PolicyRule
	ResolutionErrors error
}

// TODO: Investigate replacing this regex parsing with structured error handling once there are
//
//	structured RBAC errors introduced by https://github.com/kubernetes/kubernetes/pull/130955.
//
// parseEscalationErrorForMissingRules attempts to extract specific RBAC permissions
// that were denied due to escalation prevention from a given error's text.
// It returns the list of extracted PolicyRules and an error detailing the escalation attempt
// and any resolution errors found.
// Note: If parsing is successful, the returned error is derived from the *input* error's
// message, not an error encountered during the parsing process itself. If parsing fails due to an unexpected
// error format, a distinct parsing error is returned.
func parseEscalationErrorForMissingRules(ecError error) (*parseResult, error) {
	var (
		result      = &parseResult{}
		parseErrors []error
	)

	// errRegex captures the missing permissions and optionally resolution errors from an escalation error message
	// Group 1: The list of missing permissions
	// Group 2: Optional resolution errors
	errString := ecError.Error()
	errMatches := errRegex.FindStringSubmatch(errString) // Use FindStringSubmatch for single match expected

	// Check if the main error message pattern was matched and captured the required groups
	// We expect at least 3 elements: full match, missing permissions, resolution errors (can be empty)
	if len(errMatches) != 3 {
		// The error format doesn't match the expected pattern for escalation errors
		return &parseResult{}, fmt.Errorf("unexpected format of escalation check error string: %q", errString)
	}
	missingPermissionsStr := errMatches[1]
	if resolutionErrorsStr := errMatches[2]; resolutionErrorsStr != "" {
		result.ResolutionErrors = errors.New(resolutionErrorsStr)
	}

	// Extract permissions using permRegex from the captured permissions string (Group 1)
	for _, rule := range ruleRegex.FindAllString(missingPermissionsStr, -1) {
		pr, err := parseCompactRuleString(rule)
		if err != nil {
			parseErrors = append(parseErrors, err)
			continue
		}
		result.MissingRules = append(result.MissingRules, *pr)
	}
	// Return the extracted permissions and the constructed error message
	return result, errors.Join(parseErrors...)
}

func parseCompactRuleString(rule string) (*rbacv1.PolicyRule, error) {
	var fields []string
	if ruleText := rule[1 : len(rule)-1]; ruleText != "" {
		fields = mapSlice(strings.Split(ruleText, ","), func(in string) string {
			return strings.TrimSpace(in)
		})
	}
	var pr rbacv1.PolicyRule
	for _, item := range fields {
		field, valuesStr, ok := strings.Cut(item, ":")
		if !ok {
			return nil, fmt.Errorf("unexpected item %q: expected <Type>:[<values>...]", item)
		}
		values := mapSlice(itemRegex.FindAllString(valuesStr, -1), func(in string) string {
			return strings.Trim(in, `"`)
		})
		switch field {
		case "APIGroups":
			pr.APIGroups = values
		case "Resources":
			pr.Resources = values
		case "ResourceNames":
			pr.ResourceNames = values
		case "NonResourceURLs":
			pr.NonResourceURLs = values
		case "Verbs":
			pr.Verbs = values
		default:
			return nil, fmt.Errorf("unexpected item %q: unknown field: %q", item, field)
		}
	}
	return &pr, nil
}

func hasAggregationRule(clusterRole *rbacv1.ClusterRole) bool {
	// Currently, an aggregation rule is considered present only if it has one or more selectors.
	// An empty slice of ClusterRoleSelectors means no selectors were provided,
	// which does NOT imply "match all."
	return clusterRole.AggregationRule != nil && len(clusterRole.AggregationRule.ClusterRoleSelectors) > 0
}

func mapSlice[I, O any](in []I, f func(I) O) []O {
	out := make([]O, len(in))
	for i := range in {
		out[i] = f(in[i])
	}
	return out
}

func toPtrSlice[V any](in []V) []*V {
	out := make([]*V, len(in))
	for i := range in {
		out[i] = &in[i]
	}
	return out
}
