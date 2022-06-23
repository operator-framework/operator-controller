package bundle_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kscheme "k8s.io/client-go/kubernetes/scheme"
	pkgclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	plain "github.com/operator-framework/rukpak/internal/provisioner/plain/types"
	"github.com/operator-framework/rukpak/internal/updater/bundle"
)

var _ = Describe("Updater", func() {
	var (
		client pkgclient.Client
		u      bundle.Updater
		obj    *rukpakv1alpha1.Bundle
		status = &rukpakv1alpha1.BundleStatus{
			Phase:              rukpakv1alpha1.PhaseFailing,
			ResolvedSource:     &rukpakv1alpha1.BundleSource{Type: rukpakv1alpha1.SourceTypeImage, Image: &rukpakv1alpha1.ImageSource{Ref: "digest"}},
			ObservedGeneration: 1,
			Conditions: []metav1.Condition{
				{
					Type:               "Working",
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 3,
					LastTransitionTime: metav1.Time{},
					Reason:             "requested",
					Message:            "Working correctly",
				},
				{
					Type:               "starting",
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 1,
					LastTransitionTime: metav1.Time{},
					Reason:             "started",
					Message:            "starting up",
				},
			},
		}
	)

	BeforeEach(func() {
		schemeBuilder := runtime.NewSchemeBuilder(
			kscheme.AddToScheme,
			rukpakv1alpha1.AddToScheme,
		)
		scheme := runtime.NewScheme()
		Expect(schemeBuilder.AddToScheme(scheme)).ShouldNot(HaveOccurred())

		client = fake.NewClientBuilder().WithScheme(scheme).Build()
		u = bundle.NewBundleUpdater(client)
		obj = &rukpakv1alpha1.Bundle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "testBundle",
				Namespace: "testNamespace",
			},
			Spec: rukpakv1alpha1.BundleSpec{
				ProvisionerClassName: plain.ProvisionerID,
				Source: rukpakv1alpha1.BundleSource{
					Type: rukpakv1alpha1.SourceTypeImage,
					Image: &rukpakv1alpha1.ImageSource{
						Ref: "quay.io/tflannag/olm-plain-bundle:olm-crds-v0.20.0",
					},
				},
			},
			Status: rukpakv1alpha1.BundleStatus{
				Phase:              rukpakv1alpha1.PhaseFailing,
				ResolvedSource:     &rukpakv1alpha1.BundleSource{Type: rukpakv1alpha1.SourceTypeImage, Image: &rukpakv1alpha1.ImageSource{Ref: "digest"}},
				ObservedGeneration: 1,
				Conditions: []metav1.Condition{
					{
						Type:               "Working",
						Status:             metav1.ConditionTrue,
						ObservedGeneration: 3,
						LastTransitionTime: metav1.Time{},
						Reason:             "requested",
						Message:            "Working correctly",
					},
					{
						Type:               "starting",
						Status:             metav1.ConditionTrue,
						ObservedGeneration: 1,
						LastTransitionTime: metav1.Time{},
						Reason:             "started",
						Message:            "starting up",
					},
				},
			},
		}
		Expect(client.Create(context.Background(), obj)).To(Succeed())
	})

	When("the object does not exist", func() {
		It("should fail", func() {
			Expect(client.Delete(context.Background(), obj)).To(Succeed())
			u.UpdateStatus(
				bundle.EnsureCondition(status.Conditions[0]),
				bundle.EnsureObservedGeneration(status.ObservedGeneration),
				bundle.EnsureResolvedSource(&rukpakv1alpha1.BundleSource{Type: rukpakv1alpha1.SourceTypeImage, Image: &rukpakv1alpha1.ImageSource{Ref: "digest"}}),
				bundle.SetPhase(status.Phase),
			)
			err := u.Apply(context.Background(), obj)
			Expect(err).NotTo(BeNil())
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
	})

	When("an update is a change", func() {
		It("should apply an update status function", func() {
			u.UpdateStatus(bundle.EnsureCondition(metav1.Condition{

				Type:               "Working",
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 4,
				LastTransitionTime: metav1.Time{},
				Reason:             "requested",
				Message:            "Working correctly",
			}))
			resourceVersion := obj.GetResourceVersion()

			Expect(u.Apply(context.Background(), obj)).To(Succeed())
			Expect(client.Get(context.Background(), pkgclient.ObjectKeyFromObject(obj), obj)).To(Succeed())
			Expect(obj.Status.Conditions).To(HaveLen(2))
			Expect(obj.GetResourceVersion()).NotTo(Equal(resourceVersion))
		})
	})
})

var _ = Describe("EnsureResolvedSource", func() {
	var status *rukpakv1alpha1.BundleStatus

	BeforeEach(func() {
		status = &rukpakv1alpha1.BundleStatus{}
	})

	It("should add BundleDigest if not present", func() {
		Expect(bundle.EnsureResolvedSource(&rukpakv1alpha1.BundleSource{Type: rukpakv1alpha1.SourceTypeImage, Image: &rukpakv1alpha1.ImageSource{Ref: "digest"}})(status)).To(BeTrue())
		Expect(status.ResolvedSource).To(Equal(&rukpakv1alpha1.BundleSource{Type: rukpakv1alpha1.SourceTypeImage, Image: &rukpakv1alpha1.ImageSource{Ref: "digest"}}))
	})

	It("should return false for no update", func() {
		status.ResolvedSource = &rukpakv1alpha1.BundleSource{Type: rukpakv1alpha1.SourceTypeImage, Image: &rukpakv1alpha1.ImageSource{Ref: "digest"}}
		Expect(bundle.EnsureResolvedSource(&rukpakv1alpha1.BundleSource{Type: rukpakv1alpha1.SourceTypeImage, Image: &rukpakv1alpha1.ImageSource{Ref: "digest"}})(status)).To(BeFalse())
		Expect(status.ResolvedSource).To(Equal(&rukpakv1alpha1.BundleSource{Type: rukpakv1alpha1.SourceTypeImage, Image: &rukpakv1alpha1.ImageSource{Ref: "digest"}}))
	})
})

var _ = Describe("EnsureContentURL", func() {
	var status *rukpakv1alpha1.BundleStatus

	BeforeEach(func() {
		status = &rukpakv1alpha1.BundleStatus{}
	})

	It("should add ContentURL if not present", func() {
		Expect(bundle.EnsureContentURL("url")(status)).To(BeTrue())
		Expect(status.ContentURL).To(Equal("url"))
	})

	It("should return false for no update", func() {
		status.ContentURL = "url"
		Expect(bundle.EnsureContentURL("url")(status)).To(BeFalse())
		Expect(status.ContentURL).To(Equal("url"))
	})
})

var _ = Describe("EnsureCondition", func() {
	var status *rukpakv1alpha1.BundleStatus
	var condition, anotherCondition metav1.Condition

	BeforeEach(func() {
		status = &rukpakv1alpha1.BundleStatus{}
		condition = metav1.Condition{Type: "Working"}
		anotherCondition = metav1.Condition{Type: "Completed"}
	})

	It("should add Condition if not present", func() {
		Expect(bundle.EnsureCondition(condition)(status)).To(BeTrue())
		status.Conditions[0].LastTransitionTime = metav1.Time{}
		Expect(status.Conditions[0]).To(Equal(condition))
	})

	It("should return false for no update", func() {
		status = &rukpakv1alpha1.BundleStatus{Conditions: []metav1.Condition{condition}}
		Expect(bundle.EnsureCondition(condition)(status)).To(BeFalse())
		Expect(status.Conditions[0]).To(Equal(condition))
	})

	It("should add Condition if same type not present", func() {
		status = &rukpakv1alpha1.BundleStatus{Conditions: []metav1.Condition{condition}}
		Expect(bundle.EnsureCondition(anotherCondition)(status)).To(BeTrue())
		status.Conditions[1].LastTransitionTime = metav1.Time{}
		Expect(status.Conditions[1]).To(Equal(anotherCondition))
	})
})

var _ = Describe("EnsureObservedGeneration", func() {
	var status *rukpakv1alpha1.BundleStatus

	BeforeEach(func() {
		status = &rukpakv1alpha1.BundleStatus{}
	})

	It("should add ObservedGeneration if not present", func() {
		Expect(bundle.EnsureObservedGeneration(3)(status)).To(BeTrue())
		Expect(status.ObservedGeneration).To(Equal(int64(3)))
	})

	It("should return false for no update", func() {
		status.ObservedGeneration = 5
		Expect(bundle.EnsureObservedGeneration(5)(status)).To(BeFalse())
		Expect(status.ObservedGeneration).To(Equal(int64(5)))
	})
})

var _ = Describe("SetPhase", func() {
	var status *rukpakv1alpha1.BundleStatus

	BeforeEach(func() {
		status = &rukpakv1alpha1.BundleStatus{}
	})

	It("should set phase if not present", func() {
		Expect(bundle.SetPhase("phase")(status)).To(BeTrue())
		Expect(status.Phase).To(Equal("phase"))
	})

	It("should return false for no update", func() {
		status.Phase = "phase"
		Expect(bundle.SetPhase("phase")(status)).To(BeFalse())
		Expect(status.Phase).To(Equal("phase"))
	})
})
