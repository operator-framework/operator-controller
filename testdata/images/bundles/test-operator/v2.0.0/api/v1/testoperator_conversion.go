/*
Copyright 2025.

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

package v1

import (
	"log"

	"sigs.k8s.io/controller-runtime/pkg/conversion"

	testolmv2 "github.com/operator-framework/operator-controller/testdata/images/bundles/test-operator/v2.0.0/api/v2"
)

// ConvertTo converts this TestOperator (v1) to the Hub version (v2).
func (src *TestOperator) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*testolmv2.TestOperator)
	log.Printf("ConvertTo: Converting TestOperator from Spoke version v1 to Hub version v2;"+
		"source: %s/%s, target: %s/%s", src.Namespace, src.Name, dst.Namespace, dst.Name)
	dst.ObjectMeta = src.ObjectMeta
	dst.Spec.EchoMessage = src.Spec.Message
	log.Printf("ConvertedTo: %s/%s", dst.Namespace, dst.Name)
	return nil
}

// ConvertFrom converts the Hub version (v2) to this TestOperator (v1).
func (dst *TestOperator) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*testolmv2.TestOperator)
	log.Printf("ConvertFrom: Converting TestOperator from Hub version v2 to Spoke version v1;"+
		"source: %s/%s, target: %s/%s", src.Namespace, src.Name, dst.Namespace, dst.Name)
	dst.ObjectMeta = src.ObjectMeta
	dst.Spec.Message = src.Spec.EchoMessage
	log.Printf("ConvertedTo: %s/%s", dst.Namespace, dst.Name)
	return nil
}
