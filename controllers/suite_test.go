/*
Copyright 2023.

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

package controllers_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/resolution"
	operatorutil "github.com/operator-framework/operator-controller/internal/util"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "config", "crd", "bases"),
			filepath.Join("..", "testdata", "crds")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = operatorsv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = rukpakv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	err = controllers.NewOperatorReconciler(
		k8sManager.GetClient(),
		k8sManager.GetScheme(),
		resolution.NewOperatorResolver(k8sManager.GetClient(), resolution.HardcodedEntitySource),
	).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	ctx, cancel = context.WithCancel(context.Background())
	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

var _ = Describe("Reconcile Test", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	When("an Operator is created", func() {
		var (
			operator *operatorsv1alpha1.Operator
			ctx      context.Context
			opName   string
			pkgName  string
			err      error
		)
		BeforeEach(func() {
			ctx = context.Background()
			opName = fmt.Sprintf("operator-test-%s", rand.String(8))
			pkgName = fmt.Sprintf("package-test-%s", rand.String(8))
			operator = &operatorsv1alpha1.Operator{
				ObjectMeta: metav1.ObjectMeta{
					Name: opName,
				},
				Spec: operatorsv1alpha1.OperatorSpec{
					PackageName: pkgName,
				},
			}
			err = k8sClient.Create(ctx, operator)
			Expect(err).To(Not(HaveOccurred()))
		})
		AfterEach(func() {
			err = k8sClient.Delete(ctx, operator)
			Expect(err).To(Not(HaveOccurred()))
		})
		It("has all Conditions created", func() {
			op := &operatorsv1alpha1.Operator{}
			opLookupKey := client.ObjectKey{Name: opName}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, opLookupKey, op)
				if err != nil {
					return false
				}
				return len(op.Status.Conditions) > 0
			}, timeout, interval).Should(BeTrue())

			// All defined condition Types MUST exist after reconciliation
			conds := op.Status.Conditions
			Expect(conds).To(Not(BeEmpty()))
			Expect(conds).To(HaveLen(len(operatorutil.ConditionTypes)))
			for _, t := range operatorutil.ConditionTypes {
				Expect(apimeta.FindStatusCondition(conds, t)).To(Not(BeNil()))
			}
		})
		It("has matching generations in Conditions", func() {
			op := &operatorsv1alpha1.Operator{}

			err = k8sClient.Get(ctx, client.ObjectKey{
				Name: opName,
			}, op)
			Expect(err).To(Not(HaveOccurred()))

			// The ObservedGeneration MUST match the resource generation after reconciliation
			for _, c := range op.Status.Conditions {
				Expect(c.ObservedGeneration).To(Equal(op.GetGeneration()))
			}
		})
		It("has only pre-defined Reasons", func() {
			op := &operatorsv1alpha1.Operator{}

			err = k8sClient.Get(ctx, client.ObjectKey{
				Name: opName,
			}, op)
			Expect(err).To(Not(HaveOccurred()))

			// A given Reason MUST be in the list of ConditionReasons
			for _, c := range op.Status.Conditions {
				Expect(c.Reason).To(BeElementOf(operatorutil.ConditionReasons))
			}
		})
	})
})
