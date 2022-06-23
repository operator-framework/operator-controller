package bundleinstance_test

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
	bundleinstance "github.com/operator-framework/rukpak/internal/updater/bundle-instance"
)

var _ = Describe("Updater", func() {
	var (
		client pkgclient.Client
		u      bundleinstance.Updater
		obj    *rukpakv1alpha1.BundleInstance
		status = &rukpakv1alpha1.BundleInstanceStatus{
			InstalledBundleName: "bundle",
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
		u = bundleinstance.NewBundleInstanceUpdater(client)
		obj = &rukpakv1alpha1.BundleInstance{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "olm-crds",
			},
			Spec: rukpakv1alpha1.BundleInstanceSpec{
				ProvisionerClassName: plain.ProvisionerID,
				Template: &rukpakv1alpha1.BundleTemplate{
					Spec: rukpakv1alpha1.BundleSpec{
						ProvisionerClassName: plain.ProvisionerID,
						Source: rukpakv1alpha1.BundleSource{
							Type: rukpakv1alpha1.SourceTypeImage,
							Image: &rukpakv1alpha1.ImageSource{
								Ref: "testdata/bundles/plain-v0:valid",
							},
						},
					},
				},
			},
			Status: rukpakv1alpha1.BundleInstanceStatus{
				InstalledBundleName: "bundle",
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
			u.UpdateStatus(bundleinstance.EnsureCondition(status.Conditions[0]), bundleinstance.EnsureInstalledName("bundle"))
			err := u.Apply(context.Background(), obj)
			Expect(err).NotTo(BeNil())
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
	})

	When("an update is a change", func() {
		It("should apply an update status function", func() {
			u.UpdateStatus(bundleinstance.EnsureCondition(metav1.Condition{

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

var _ = Describe("EnsureCondition", func() {
	var status *rukpakv1alpha1.BundleInstanceStatus
	var condition, anotherCondition metav1.Condition

	BeforeEach(func() {
		status = &rukpakv1alpha1.BundleInstanceStatus{}
		condition = metav1.Condition{Type: "Working"}
		anotherCondition = metav1.Condition{Type: "Completed"}
	})

	It("should add Condition if not present", func() {
		Expect(bundleinstance.EnsureCondition(condition)(status)).To(BeTrue())
		status.Conditions[0].LastTransitionTime = metav1.Time{}
		Expect(status.Conditions[0]).To(Equal(condition))
	})

	It("should return false for no update", func() {
		status = &rukpakv1alpha1.BundleInstanceStatus{Conditions: []metav1.Condition{condition}}
		Expect(bundleinstance.EnsureCondition(condition)(status)).To(BeFalse())
		Expect(status.Conditions[0]).To(Equal(condition))
	})

	It("should add Condition if same type not present", func() {
		status = &rukpakv1alpha1.BundleInstanceStatus{Conditions: []metav1.Condition{condition}}
		Expect(bundleinstance.EnsureCondition(anotherCondition)(status)).To(BeTrue())
		status.Conditions[1].LastTransitionTime = metav1.Time{}
		Expect(status.Conditions[1]).To(Equal(anotherCondition))
	})
})

var _ = Describe("EnsureInstalledName", func() {
	var status *rukpakv1alpha1.BundleInstanceStatus
	var installedBundleName string

	BeforeEach(func() {
		status = &rukpakv1alpha1.BundleInstanceStatus{}
		installedBundleName = "bundle"
	})

	It("should update the installedBundleName if not set", func() {
		Expect(bundleinstance.EnsureInstalledName(installedBundleName)(status)).To(BeTrue())
		Expect(status.InstalledBundleName).To(Equal(installedBundleName))
	})

	It("should not update the installedBundleName if already set", func() {
		status = &rukpakv1alpha1.BundleInstanceStatus{InstalledBundleName: installedBundleName}
		Expect(bundleinstance.EnsureInstalledName(installedBundleName)(status)).To(BeFalse())
	})
})
