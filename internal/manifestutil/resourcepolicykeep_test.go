/*
Copyright 2021 The Operator-SDK Authors.

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

package manifestutil_test

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"helm.sh/helm/v3/pkg/kube"

	"github.com/operator-framework/operator-controller/internal/manifestutil"
)

var _ = Describe("HasResourcePolicyKeep", func() {
	It("returns false for nil annotations", func() {
		Expect(manifestutil.HasResourcePolicyKeep(nil)).To(BeFalse())
	})
	It("returns true on base case", func() {
		annotations := map[string]string{kube.ResourcePolicyAnno: kube.KeepPolicy}
		Expect(manifestutil.HasResourcePolicyKeep(annotations)).To(BeTrue())
	})
	It("returns false when annotation key is not found", func() {
		annotations := map[string]string{"not-" + kube.ResourcePolicyAnno: kube.KeepPolicy}
		Expect(manifestutil.HasResourcePolicyKeep(annotations)).To(BeFalse())
	})
	It("returns false when annotation value is not 'keep'", func() {
		annotations := map[string]string{"not-" + kube.ResourcePolicyAnno: "not-" + kube.KeepPolicy}
		Expect(manifestutil.HasResourcePolicyKeep(annotations)).To(BeFalse())
	})
	It("returns true when annotation is uppercase", func() {
		annotations := map[string]string{kube.ResourcePolicyAnno: strings.ToUpper(kube.KeepPolicy)}
		Expect(manifestutil.HasResourcePolicyKeep(annotations)).To(BeTrue())
	})
	It("returns true when annotation is has whitespace prefix and/or suffix", func() {
		annotations := map[string]string{kube.ResourcePolicyAnno: " " + kube.KeepPolicy + "  "}
		Expect(manifestutil.HasResourcePolicyKeep(annotations)).To(BeTrue())
	})
	It("returns true when annotation is uppercase and has whitespace prefix and/or suffix", func() {
		annotations := map[string]string{kube.ResourcePolicyAnno: " " + strings.ToUpper(kube.KeepPolicy) + "  "}
		Expect(manifestutil.HasResourcePolicyKeep(annotations)).To(BeTrue())
	})
})
