package validators_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/controllers/validators"
)

var _ = Describe("Validators", func() {
	Describe("ValidateOperatorSpec", func() {
		It("should not return an error for valid SemVer", func() {
			operator := &v1alpha1.Operator{
				Spec: v1alpha1.OperatorSpec{
					Version: "1.2.3",
				},
			}
			err := validators.ValidateOperatorSpec(operator)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return an error for invalid SemVer", func() {
			operator := &v1alpha1.Operator{
				Spec: v1alpha1.OperatorSpec{
					Version: "invalid-semver",
				},
			}
			err := validators.ValidateOperatorSpec(operator)
			Expect(err).To(HaveOccurred())
		})

		It("should not return an error for empty SemVer", func() {
			operator := &v1alpha1.Operator{
				Spec: v1alpha1.OperatorSpec{
					Version: "",
				},
			}
			err := validators.ValidateOperatorSpec(operator)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should not return an error for valid SemVer with pre-release and metadata", func() {
			operator := &v1alpha1.Operator{
				Spec: v1alpha1.OperatorSpec{
					Version: "1.2.3-alpha.1+metadata",
				},
			}
			err := validators.ValidateOperatorSpec(operator)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should not return an error for valid SemVer with pre-release", func() {
			operator := &v1alpha1.Operator{
				Spec: v1alpha1.OperatorSpec{
					Version: "1.2.3-alpha-beta",
				},
			}
			err := validators.ValidateOperatorSpec(operator)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
