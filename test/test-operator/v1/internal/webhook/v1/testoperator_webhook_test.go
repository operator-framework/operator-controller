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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	testolmv1 "github.com/operator-framework/operator-controller/test/test-operator/v1/api/v1"
)

var _ = Describe("TestOperator Webhook", func() {
	var (
		obj       *testolmv1.TestOperator
		oldObj    *testolmv1.TestOperator
		validator TestOperatorCustomValidator
		defaulter TestOperatorCustomDefaulter
	)

	BeforeEach(func() {
		obj = &testolmv1.TestOperator{}
		oldObj = &testolmv1.TestOperator{}
		validator = TestOperatorCustomValidator{}
		Expect(validator).NotTo(BeNil(), "Expected validator to be initialized")
		defaulter = TestOperatorCustomDefaulter{}
		Expect(defaulter).NotTo(BeNil(), "Expected defaulter to be initialized")
		Expect(oldObj).NotTo(BeNil(), "Expected oldObj to be initialized")
		Expect(obj).NotTo(BeNil(), "Expected obj to be initialized")
	})

	Context("When creating TestOperator under Defaulting Webhook", func() {
		It("Should apply defaults when a required field is empty", func() {
			By("simulating a scenario where defaults should be applied")
			obj.Spec.Message = ""
			Expect(defaulter.Default(ctx, obj)).To(Succeed())
			By("checking that the default values are set")
			Expect(obj.Spec.Message).To(Equal("Echo"))
		})
	})

	Context("When creating or updating TestOperator under Validating Webhook", func() {
		It("Should deny creation if talking about fight club", func() {
			By("talking about fight club")
			obj.Spec.Message = "Have you heard about fight club?"
			Expect(validator.ValidateCreate(ctx, obj)).Error().To(HaveOccurred())
		})

		It("Should admit creation not talking about fight club", func() {
			By("not talking about fight club")
			obj.Spec.Message = "I am a message"
			Expect(validator.ValidateCreate(ctx, obj)).To(BeNil())
		})

		It("Should validate updates correctly", func() {
			By("by admitting if not talking about fight club")
			oldObj.Spec.Message = "The hills are alive"
			obj.Spec.Message = "his name was Robert Paulson"
			Expect(validator.ValidateUpdate(ctx, oldObj, obj)).To(BeNil())

			By("by rejecting if talking about fight club")
			oldObj.Spec.Message = "The hills are alive"
			obj.Spec.Message = "with the sound of fight club"
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).ToNot(Succeed())
		})
	})
})
