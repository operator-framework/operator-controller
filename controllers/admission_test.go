package controllers_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	It("should fail if an invalid semver range is given", func() {
		err := cl.Create(ctx, operator(operatorsv1alpha1.OperatorSpec{
			PackageName: "package",
			Version:     "this-is-not-a-valid-semver",
		}))

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.version in body should match '^(\\|\\||\\s+)?([\\s~^><=]*)v?(\\d+)(\\.(\\d+))?(\\.(\\d+))?(\\-(.+))?$"))
	})
})
