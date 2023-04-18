package controllers_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

func operator(spec operatorsv1alpha1.OperatorSpec) *operatorsv1alpha1.Operator {
	return &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-operator",
		},
		Spec: spec,
	}
}

var _ = Describe("Operator Spec Validations", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)
	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())
	})
	AfterEach(func() {
		cancel()
	})
	It("should fail if the spec is empty", func() {
		err := cl.Create(ctx, operator(operatorsv1alpha1.OperatorSpec{}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.packageName in body should match '^[a-z0-9]+(-[a-z0-9]+)*$'"))
	})
	It("should fail if package name length is greater than 48 characters", func() {
		err := cl.Create(ctx, operator(operatorsv1alpha1.OperatorSpec{
			PackageName: "this-is-a-really-long-package-name-that-is-greater-than-48-characters",
		}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("Too long: may not be longer than 48"))
	})
	It("should fail if version is valid semver but length is greater than 64 characters", func() {
		err := cl.Create(ctx, operator(operatorsv1alpha1.OperatorSpec{
			PackageName: "package",
			Version:     "1234567890.1234567890.12345678901234567890123456789012345678901234",
		}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("Too long: may not be longer than 64"))
	})
	It("should fail if an invalid semver is given", func() {
		invalidSemvers := []string{
			"1.2.3.4",
			"1.02.3",
			"1.2.03",
			"1.2.3-beta!",
			"1.2.3.alpha",
			"1..2.3",
			"1.2.3-pre+bad_metadata",
			"1.2.-3",
			".1.2.3",
		}
		for _, invalidSemver := range invalidSemvers {
			err := cl.Create(ctx, operator(operatorsv1alpha1.OperatorSpec{
				PackageName: "package",
				Version:     invalidSemver,
			}))

			Expect(err).To(HaveOccurred(), "expected error for invalid semver %q", invalidSemver)
			Expect(err.Error()).To(ContainSubstring("spec.version in body should match '^(0|[1-9]\\d*)\\.(0|[1-9]\\d*)\\.(0|[1-9]\\d*)(-(0|[1-9]\\d*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*)(\\.(0|[1-9]\\d*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*))*)?(\\+([0-9a-zA-Z-]+(\\.[0-9a-zA-Z-]+)*))?$'"))
		}
	})
	It("should fail if an invalid channel name is given", func() {
		invalidChannels := []string{
			"spaces spaces",
			"Capitalized",
			"camelCase",
			"many/invalid$characters+in_name",
			"-start-with-hyphen",
			"end-with-hyphen-",
			"channel-has-version-1.0.1",
		}
		for _, invalidChannel := range invalidChannels {
			err := cl.Create(ctx, operator(operatorsv1alpha1.OperatorSpec{
				PackageName: "package",
				Channel:     invalidChannel,
			}))
			Expect(err).To(HaveOccurred(), "expected error for invalid channel '%q'", invalidChannel)
			Expect(err.Error()).To(ContainSubstring("spec.channel in body should match '^[a-z0-9]+(-[a-z0-9]+)*$'"))
		}
	})
	It("should fail if an invalid channel name length", func() {
		err := cl.Create(ctx, operator(operatorsv1alpha1.OperatorSpec{
			PackageName: "package",
			Channel:     "longname01234567890123456789012345678901234567890",
		}))
		Expect(err).To(HaveOccurred(), "expected error for invalid channel length")
		Expect(err.Error()).To(ContainSubstring("spec.channel: Too long: may not be longer than 48"))
	})
})
