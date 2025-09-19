package validators

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
)

// CheckDeploymentSpecUniqueness checks that each strategy deployment spec in the csv has a unique name.
// Errors are sorted by deployment name.
func CheckDeploymentSpecUniqueness(rv1 *bundle.RegistryV1) []error {
	deploymentNameSet := sets.Set[string]{}
	duplicateDeploymentNames := sets.Set[string]{}
	for _, dep := range rv1.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
		if deploymentNameSet.Has(dep.Name) {
			duplicateDeploymentNames.Insert(dep.Name)
		}
		deploymentNameSet.Insert(dep.Name)
	}

	errs := make([]error, 0, len(duplicateDeploymentNames))
	for _, d := range slices.Sorted(slices.Values(duplicateDeploymentNames.UnsortedList())) {
		errs = append(errs, fmt.Errorf("cluster service version contains duplicate strategy deployment spec '%s'", d))
	}
	return errs
}

// CheckDeploymentNameIsDNS1123SubDomain checks each deployment strategy spec name complies with the Kubernetes
// resource naming conversions
func CheckDeploymentNameIsDNS1123SubDomain(rv1 *bundle.RegistryV1) []error {
	deploymentNameErrMap := map[string][]string{}
	for _, dep := range rv1.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
		errs := validation.IsDNS1123Subdomain(dep.Name)
		if len(errs) > 0 {
			slices.Sort(errs)
			deploymentNameErrMap[dep.Name] = errs
		}
	}

	errs := make([]error, 0, len(deploymentNameErrMap))
	for _, dep := range slices.Sorted(maps.Keys(deploymentNameErrMap)) {
		errs = append(errs, fmt.Errorf("invalid cluster service version strategy deployment name '%s': %s", dep, strings.Join(deploymentNameErrMap[dep], ", ")))
	}
	return errs
}

// CheckOwnedCRDExistence checks bundle owned custom resource definitions declared in the csv exist in the bundle
func CheckOwnedCRDExistence(rv1 *bundle.RegistryV1) []error {
	crdsNames := sets.Set[string]{}
	for _, crd := range rv1.CRDs {
		crdsNames.Insert(crd.Name)
	}

	missingCRDNames := sets.Set[string]{}
	for _, crd := range rv1.CSV.Spec.CustomResourceDefinitions.Owned {
		if !crdsNames.Has(crd.Name) {
			missingCRDNames.Insert(crd.Name)
		}
	}

	errs := make([]error, 0, len(missingCRDNames))
	for _, crdName := range slices.Sorted(slices.Values(missingCRDNames.UnsortedList())) {
		errs = append(errs, fmt.Errorf("cluster service definition references owned custom resource definition '%s' not found in bundle", crdName))
	}
	return errs
}

// CheckCRDResourceUniqueness checks that the bundle CRD names are unique
func CheckCRDResourceUniqueness(rv1 *bundle.RegistryV1) []error {
	crdsNames := sets.Set[string]{}
	duplicateCRDNames := sets.Set[string]{}
	for _, crd := range rv1.CRDs {
		if crdsNames.Has(crd.Name) {
			duplicateCRDNames.Insert(crd.Name)
		}
		crdsNames.Insert(crd.Name)
	}

	errs := make([]error, 0, len(duplicateCRDNames))
	for _, crdName := range slices.Sorted(slices.Values(duplicateCRDNames.UnsortedList())) {
		errs = append(errs, fmt.Errorf("bundle contains duplicate custom resource definition '%s'", crdName))
	}
	return errs
}

// CheckPackageNameNotEmpty checks that PackageName is not empty
func CheckPackageNameNotEmpty(rv1 *bundle.RegistryV1) []error {
	if rv1.PackageName == "" {
		return []error{errors.New("package name is empty")}
	}
	return nil
}

// CheckConversionWebhookSupport checks that if the bundle cluster service version declares conversion webhook definitions,
// that the bundle also only supports AllNamespaces install mode. This keeps parity with OLMv0 behavior for conversion webhooks,
// https://github.com/operator-framework/operator-lifecycle-manager/blob/dfd0b2bea85038d3c0d65348bc812d297f16b8d2/pkg/controller/install/webhook.go#L193
func CheckConversionWebhookSupport(rv1 *bundle.RegistryV1) []error {
	var conversionWebhookNames []string
	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		if wh.Type == v1alpha1.ConversionWebhook {
			conversionWebhookNames = append(conversionWebhookNames, wh.GenerateName)
		}
	}

	if len(conversionWebhookNames) > 0 {
		supportedInstallModes := sets.Set[v1alpha1.InstallModeType]{}
		for _, mode := range rv1.CSV.Spec.InstallModes {
			if mode.Supported {
				supportedInstallModes.Insert(mode.Type)
			}
		}

		if len(supportedInstallModes) != 1 || !supportedInstallModes.Has(v1alpha1.InstallModeTypeAllNamespaces) {
			sortedModes := slices.Sorted(slices.Values(supportedInstallModes.UnsortedList()))
			errs := make([]error, len(conversionWebhookNames))
			for i, webhookName := range conversionWebhookNames {
				errs[i] = fmt.Errorf("bundle contains conversion webhook %q and supports install modes %v - conversion webhooks are only supported for bundles that only support AllNamespaces install mode", webhookName, sortedModes)
			}
			return errs
		}
	}

	return nil
}

// CheckWebhookDeploymentReferentialIntegrity checks that each webhook definition in the csv
// references an existing strategy deployment spec. Errors are sorted by strategy deployment spec name,
// webhook type, and webhook name.
func CheckWebhookDeploymentReferentialIntegrity(rv1 *bundle.RegistryV1) []error {
	webhooksByDeployment := map[string][]v1alpha1.WebhookDescription{}
	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		webhooksByDeployment[wh.DeploymentName] = append(webhooksByDeployment[wh.DeploymentName], wh)
	}

	for _, depl := range rv1.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
		delete(webhooksByDeployment, depl.Name)
	}

	var errs []error
	// Loop through sorted keys to keep error messages ordered by deployment name
	for _, deploymentName := range slices.Sorted(maps.Keys(webhooksByDeployment)) {
		webhookDefns := webhooksByDeployment[deploymentName]
		slices.SortFunc(webhookDefns, func(a, b v1alpha1.WebhookDescription) int {
			return cmp.Or(cmp.Compare(a.Type, b.Type), cmp.Compare(a.GenerateName, b.GenerateName))
		})
		for _, webhookDef := range webhookDefns {
			errs = append(errs, fmt.Errorf("webhook of type '%s' with name '%s' references non-existent deployment '%s'", webhookDef.Type, webhookDef.GenerateName, webhookDef.DeploymentName))
		}
	}
	return errs
}

// CheckWebhookNameUniqueness checks that each webhook definition of each type (validating, mutating, or conversion)
// has a unique name. Webhooks of different types can have the same name. Errors are sorted by webhook type
// and name.
func CheckWebhookNameUniqueness(rv1 *bundle.RegistryV1) []error {
	webhookNameSetByType := map[v1alpha1.WebhookAdmissionType]sets.Set[string]{}
	duplicateWebhooksByType := map[v1alpha1.WebhookAdmissionType]sets.Set[string]{}
	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		if _, ok := webhookNameSetByType[wh.Type]; !ok {
			webhookNameSetByType[wh.Type] = sets.Set[string]{}
		}
		if webhookNameSetByType[wh.Type].Has(wh.GenerateName) {
			if _, ok := duplicateWebhooksByType[wh.Type]; !ok {
				duplicateWebhooksByType[wh.Type] = sets.Set[string]{}
			}
			duplicateWebhooksByType[wh.Type].Insert(wh.GenerateName)
		}
		webhookNameSetByType[wh.Type].Insert(wh.GenerateName)
	}

	var errs []error
	for _, whType := range slices.Sorted(maps.Keys(duplicateWebhooksByType)) {
		for _, webhookName := range slices.Sorted(slices.Values(duplicateWebhooksByType[whType].UnsortedList())) {
			errs = append(errs, fmt.Errorf("duplicate webhook '%s' of type '%s'", webhookName, whType))
		}
	}
	return errs
}

// CheckConversionWebhooksReferenceOwnedCRDs checks defined conversion webhooks reference bundle owned CRDs.
// Errors are sorted by webhook name and CRD name.
func CheckConversionWebhooksReferenceOwnedCRDs(rv1 *bundle.RegistryV1) []error {
	//nolint:prealloc
	var conversionWebhooks []v1alpha1.WebhookDescription
	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		if wh.Type != v1alpha1.ConversionWebhook {
			continue
		}
		conversionWebhooks = append(conversionWebhooks, wh)
	}

	if len(conversionWebhooks) == 0 {
		return nil
	}

	ownedCRDNames := sets.Set[string]{}
	for _, crd := range rv1.CSV.Spec.CustomResourceDefinitions.Owned {
		ownedCRDNames.Insert(crd.Name)
	}

	slices.SortFunc(conversionWebhooks, func(a, b v1alpha1.WebhookDescription) int {
		return cmp.Compare(a.GenerateName, b.GenerateName)
	})

	var errs []error
	for _, webhook := range conversionWebhooks {
		webhookCRDs := webhook.ConversionCRDs
		slices.Sort(webhookCRDs)
		for _, crd := range webhookCRDs {
			if !ownedCRDNames.Has(crd) {
				errs = append(errs, fmt.Errorf("conversion webhook '%s' references custom resource definition '%s' not owned bundle", webhook.GenerateName, crd))
			}
		}
	}
	return errs
}

// CheckConversionWebhookCRDReferenceUniqueness checks no two (or more) conversion webhooks reference the same CRD.
func CheckConversionWebhookCRDReferenceUniqueness(rv1 *bundle.RegistryV1) []error {
	// collect webhooks by crd
	crdToWh := map[string][]string{}
	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		if wh.Type != v1alpha1.ConversionWebhook {
			continue
		}
		for _, crd := range wh.ConversionCRDs {
			crdToWh[crd] = append(crdToWh[crd], wh.GenerateName)
		}
	}

	// remove crds with single webhook
	maps.DeleteFunc(crdToWh, func(crd string, whs []string) bool {
		return len(whs) == 1
	})

	errs := make([]error, 0, len(crdToWh))
	orderedCRDs := slices.Sorted(maps.Keys(crdToWh))
	for _, crd := range orderedCRDs {
		orderedWhs := strings.Join(slices.Sorted(slices.Values(crdToWh[crd])), ",")
		errs = append(errs, fmt.Errorf("conversion webhooks [%s] reference same custom resource definition '%s'", orderedWhs, crd))
	}
	return errs
}

// CheckWebhookNameIsDNS1123SubDomain checks each webhook configuration name complies with the Kubernetes resource naming conversions
func CheckWebhookNameIsDNS1123SubDomain(rv1 *bundle.RegistryV1) []error {
	invalidWebhooksByType := map[v1alpha1.WebhookAdmissionType]map[string][]string{}
	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		if _, ok := invalidWebhooksByType[wh.Type]; !ok {
			invalidWebhooksByType[wh.Type] = map[string][]string{}
		}
		errs := validation.IsDNS1123Subdomain(wh.GenerateName)
		if len(errs) > 0 {
			slices.Sort(errs)
			invalidWebhooksByType[wh.Type][wh.GenerateName] = errs
		}
	}

	var errs []error
	for _, whType := range slices.Sorted(maps.Keys(invalidWebhooksByType)) {
		for _, webhookName := range slices.Sorted(maps.Keys(invalidWebhooksByType[whType])) {
			errs = append(errs, fmt.Errorf("webhook of type '%s' has invalid name '%s': %s", whType, webhookName, strings.Join(invalidWebhooksByType[whType][webhookName], ",")))
		}
	}
	return errs
}

// forbiddenWebhookRuleAPIGroups contain the API groups that are forbidden for webhook configuration rules in OLMv1
var forbiddenWebhookRuleAPIGroups = sets.New("olm.operatorframework.io", "*")

// forbiddenAdmissionRegistrationResources contain the resources that are forbidden for webhook configuration rules
// for the admissionregistration.k8s.io api group
var forbiddenAdmissionRegistrationResources = sets.New(
	"*",
	"mutatingwebhookconfiguration",
	"mutatingwebhookconfigurations",
	"validatingwebhookconfiguration",
	"validatingwebhookconfigurations",
)

// CheckWebhookRules ensures webhook rules do not reference forbidden API groups or resources in line with OLMv0 behavior
// The following are forbidden, rules targeting:
//   - all API groups (i.e. '*')
//   - OLMv1 API group (i.e. 'olm.operatorframework.io')
//   - all resources under the 'admissionregistration.k8s.io' API group
//   - the 'ValidatingWebhookConfiguration' resource under the 'admissionregistration.k8s.io' API group
//   - the 'MutatingWebhookConfiguration' resource under the 'admissionregistration.k8s.io' API group
//
// These boundaries attempt to reduce the blast radius of faulty webhooks and avoid deadlocks preventing the user
// from deleting OLMv1 resources installing and managing the faulty webhook, or deleting faulty admission webhook
// configurations.
// See https://github.com/operator-framework/operator-lifecycle-manager/blob/ccf0c4c91f1e7673e87f3a18947f9a1f88d48438/pkg/controller/install/webhook.go#L19
// for more details
func CheckWebhookRules(rv1 *bundle.RegistryV1) []error {
	var errs []error
	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		// Rules are not used for conversion webhooks
		if wh.Type == v1alpha1.ConversionWebhook {
			continue
		}
		webhookName := wh.GenerateName
		for _, rule := range wh.Rules {
			for _, apiGroup := range rule.APIGroups {
				if forbiddenWebhookRuleAPIGroups.Has(apiGroup) {
					errs = append(errs, fmt.Errorf("webhook %q contains forbidden rule: admission webhook rules cannot reference API group %q", webhookName, apiGroup))
				}
				if apiGroup == "admissionregistration.k8s.io" {
					for _, resource := range rule.Resources {
						if forbiddenAdmissionRegistrationResources.Has(strings.ToLower(resource)) {
							errs = append(errs, fmt.Errorf("webhook %q contains forbidden rule: admission webhook rules cannot reference resource %q for API group %q", webhookName, resource, apiGroup))
						}
					}
				}
			}
		}
	}
	return errs
}
