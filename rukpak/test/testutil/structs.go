package testutil

import (
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// NewTestingCRD takes a name, group and versions to be set for a new CRD. If name is set to ""
// then it will run testutil.GenName() on it with the DefaultCrdName.
func NewTestingCRD(name, group string, versions []apiextensionsv1.CustomResourceDefinitionVersion) *apiextensionsv1.CustomResourceDefinition {
	if name == "" {
		name = GenName(DefaultCrdName)
	}
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%v.%v", name, group),
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Scope:    apiextensionsv1.ClusterScoped,
			Group:    group,
			Versions: versions,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   name,
				Singular: name,
				Kind:     name,
				ListKind: name + "List",
			},
		},
	}
}

// NewTestingCR takes a name, group, version and king in order to create a
// new CR of a resource correctly.
func NewTestingCR(name, group, version, kind string) *unstructured.Unstructured {
	newTestingCr := &unstructured.Unstructured{}
	newTestingCr.SetKind(kind)
	newTestingCr.SetAPIVersion(group + "/" + version)
	newTestingCr.SetGenerateName(name)
	return newTestingCr
}

// CrdReady takes a CRD's status and determines if it is ready to accept new CRs
func CrdReady(status *apiextensionsv1.CustomResourceDefinitionStatus) bool {
	if status == nil {
		return false
	}
	established, namesAccepted := false, false
	for _, cdt := range status.Conditions {
		switch cdt.Type {
		case apiextensionsv1.Established:
			if cdt.Status == apiextensionsv1.ConditionTrue {
				established = true
			}
		case apiextensionsv1.NamesAccepted:
			if cdt.Status == apiextensionsv1.ConditionTrue {
				namesAccepted = true
			}
		}
	}
	return established && namesAccepted
}
