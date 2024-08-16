/*
Copyright 2020 The Operator-SDK Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package testutil

import (
	"fmt"
	"strings"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func BuildTestCRD(gvk schema.GroupVersionKind) apiextensionsv1.CustomResourceDefinition {
	trueVal := true
	singular := strings.ToLower(gvk.Kind)
	plural := fmt.Sprintf("%ss", singular)
	return apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s.%s", plural, gvk.Group),
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: gvk.Group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     gvk.Kind,
				ListKind: fmt.Sprintf("%sList", gvk.Kind),
				Singular: singular,
				Plural:   plural,
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name: "v1",
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type:                   "object",
							XPreserveUnknownFields: &trueVal,
						},
					},
					Subresources: &apiextensionsv1.CustomResourceSubresources{
						Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
					},
					Served:  true,
					Storage: true,
				},
			},
		},
	}
}

func MustLoadChart(path string) chart.Chart {
	chrt, err := loader.Load(path)
	if err != nil {
		panic(err)
	}
	return *chrt
}

func BuildTestCR(gvk schema.GroupVersionKind) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"spec": map[string]interface{}{"replicas": 2},
	}}
	obj.SetName("test")
	obj.SetNamespace("default")
	obj.SetGroupVersionKind(gvk)
	obj.SetUID("test-uid")
	obj.SetAnnotations(map[string]string{
		"helm.sdk.operatorframework.io/install-description":   "test install description",
		"helm.sdk.operatorframework.io/upgrade-description":   "test upgrade description",
		"helm.sdk.operatorframework.io/uninstall-description": "test uninstall description",
	})
	return obj
}
