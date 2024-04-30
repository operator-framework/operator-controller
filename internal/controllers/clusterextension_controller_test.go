package controllers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
	rukpakv1alpha2 "github.com/operator-framework/rukpak/api/v1alpha2"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	"github.com/operator-framework/operator-controller/internal/conditionsets"
	"github.com/operator-framework/operator-controller/internal/controllers"
	"github.com/operator-framework/operator-controller/pkg/features"
)

// Describe: ClusterExtension Controller Test
func TestClusterExtensionDoesNotExist(t *testing.T) {
	_, reconciler := newClientAndReconciler(t)

	t.Log("When the cluster extension does not exist")
	t.Log("It returns no error")
	res, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "non-existent"}})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)
}

func TestClusterExtensionNonExistentPackage(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a non-existent package")
	t.Log("By initializing cluster state")
	pkgName := fmt.Sprintf("non-existent-%s", rand.String(6))
	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName:      pkgName,
			InstallNamespace: "default",
		},
	}
	require.NoError(t, cl.Create(ctx, clusterExtension))

	t.Log("It sets resolution failure status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.EqualError(t, err, fmt.Sprintf("no package %q found", pkgName))

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	require.Empty(t, clusterExtension.Status.ResolvedBundle)
	require.Empty(t, clusterExtension.Status.InstalledBundle)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonResolutionFailed, cond.Reason)
	require.Equal(t, fmt.Sprintf("no package %q found", pkgName), cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, clusterExtension)
	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha2.BundleDeployment{}))
}

func TestClusterExtensionNonExistentVersion(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a version that does not exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName:      pkgName,
			Version:          "0.50.0", // this version of the package does not exist
			InstallNamespace: "default",
		},
	}
	require.NoError(t, cl.Create(ctx, clusterExtension))

	t.Log("It sets resolution failure status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.EqualError(t, err, fmt.Sprintf(`no package %q matching version "0.50.0" found`, pkgName))

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	require.Empty(t, clusterExtension.Status.ResolvedBundle)
	require.Empty(t, clusterExtension.Status.InstalledBundle)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonResolutionFailed, cond.Reason)
	require.Equal(t, fmt.Sprintf(`no package %q matching version "0.50.0" found`, pkgName), cond.Message)
	cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "installation has not been attempted as resolution failed", cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, clusterExtension)
	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha2.BundleDeployment{}))
}

func TestClusterExtensionBundleDeploymentDoesNotExist(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}
	const pkgName = "prometheus"
	installNamespace := fmt.Sprintf("test-ns-%s", rand.String(8))

	t.Log("When the cluster extension specifies a valid available package")
	t.Log("By initializing cluster state")
	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName:      pkgName,
			InstallNamespace: installNamespace,
		},
	}
	require.NoError(t, cl.Create(ctx, clusterExtension))

	t.Log("When the BundleDeployment does not exist")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("It results in the expected BundleDeployment")
	bd := &rukpakv1alpha2.BundleDeployment{}
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: extKey.Name}, bd))
	require.Equal(t, "core-rukpak-io-registry", bd.Spec.ProvisionerClassName)
	require.Equal(t, installNamespace, bd.Spec.InstallNamespace)
	require.Equal(t, rukpakv1alpha2.SourceTypeImage, bd.Spec.Source.Type)
	require.NotNil(t, bd.Spec.Source.Image)
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", bd.Spec.Source.Image.Ref)

	t.Log("It sets the ResolvedBundle status field")
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/2.0.0", Version: "2.0.0"}, clusterExtension.Status.ResolvedBundle)

	t.Log("It sets the InstalledBundle status field")
	require.Empty(t, clusterExtension.Status.InstalledBundle)

	t.Log("It sets the status on the cluster extension")
	cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)

	cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "bundledeployment status is unknown", cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, clusterExtension)
	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha2.BundleDeployment{}))
}

func TestClusterExtensionBundleDeploymentOutOfDate(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}
	const pkgName = "prometheus"
	installNamespace := fmt.Sprintf("test-ns-%s", rand.String(8))

	t.Log("When the cluster extension specifies a valid available package")
	t.Log("By initializing cluster state")
	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName:      pkgName,
			InstallNamespace: installNamespace,
		},
	}
	require.NoError(t, cl.Create(ctx, clusterExtension))

	t.Log("When the expected BundleDeployment already exists")
	t.Log("When the BundleDeployment spec is out of date")
	t.Log("By patching the existing BD")
	bd := &rukpakv1alpha2.BundleDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: extKey.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         ocv1alpha1.GroupVersion.String(),
					Kind:               "ClusterExtension",
					Name:               clusterExtension.Name,
					UID:                clusterExtension.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
		Spec: rukpakv1alpha2.BundleDeploymentSpec{
			InstallNamespace:     "default",
			ProvisionerClassName: "core-rukpak-io-registry",
			Source: rukpakv1alpha2.BundleSource{
				Type: rukpakv1alpha2.SourceTypeImage,
				Image: &rukpakv1alpha2.ImageSource{
					Ref: "quay.io/operatorhubio/prometheus@fake2.0.0",
				},
			},
		},
	}

	t.Log("By modifying the BD spec and creating the object")
	bd.Spec.InstallNamespace = "incorrect"
	bd.Spec.ProvisionerClassName = "core-rukpak-io-helm"
	require.NoError(t, cl.Create(ctx, bd))

	t.Log("It results in the expected BundleDeployment")

	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the expected BD spec")
	bd = &rukpakv1alpha2.BundleDeployment{}
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: extKey.Name}, bd))
	require.Equal(t, "core-rukpak-io-registry", bd.Spec.ProvisionerClassName)
	require.Equal(t, installNamespace, bd.Spec.InstallNamespace)
	require.Equal(t, rukpakv1alpha2.SourceTypeImage, bd.Spec.Source.Type)
	require.NotNil(t, bd.Spec.Source.Image)
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", bd.Spec.Source.Image.Ref)

	t.Log("By checking the status fields")
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/2.0.0", Version: "2.0.0"}, clusterExtension.Status.ResolvedBundle)
	require.Empty(t, clusterExtension.Status.InstalledBundle)

	t.Log("By checking the expected status conditions")
	cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "bundledeployment status is unknown", cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, clusterExtension)
	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha2.BundleDeployment{}))
}

func TestClusterExtensionBundleDeploymentUpToDate(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}
	const pkgName = "prometheus"
	installNamespace := fmt.Sprintf("test-ns-%s", rand.String(8))

	t.Log("When the cluster extension specifies a valid available package")
	t.Log("By initializing cluster state")
	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName:      pkgName,
			InstallNamespace: installNamespace,
		},
	}
	require.NoError(t, cl.Create(ctx, clusterExtension))

	t.Log("When the expected BundleDeployment already exists")
	t.Log("When the BundleDeployment spec is up-to-date")
	t.Log("By patching the existing BD")
	bd := &rukpakv1alpha2.BundleDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: extKey.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         ocv1alpha1.GroupVersion.String(),
					Kind:               "ClusterExtension",
					Name:               clusterExtension.Name,
					UID:                clusterExtension.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
		Spec: rukpakv1alpha2.BundleDeploymentSpec{
			InstallNamespace:     installNamespace,
			ProvisionerClassName: "core-rukpak-io-registry",
			Source: rukpakv1alpha2.BundleSource{
				Type: rukpakv1alpha2.SourceTypeImage,
				Image: &rukpakv1alpha2.ImageSource{
					Ref: "quay.io/operatorhubio/prometheus@fake2.0.0",
				},
			},
		},
	}

	require.NoError(t, cl.Create(ctx, bd))
	bd.Status.ObservedGeneration = bd.GetGeneration()

	t.Log("When the BundleDeployment status is mapped to the expected ClusterExtension status")
	t.Log("It verifies cluster extension status when bundle deployment is waiting to be created")
	t.Log("By updating the status of bundleDeployment")
	require.NoError(t, cl.Status().Update(ctx, bd))

	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching the updated cluster extension after reconcile")
	ext := &ocv1alpha1.ClusterExtension{}
	require.NoError(t, cl.Get(ctx, extKey, ext))

	t.Log("By checking the status fields")
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/2.0.0", Version: "2.0.0"}, ext.Status.ResolvedBundle)
	require.Empty(t, ext.Status.InstalledBundle)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "bundledeployment status is unknown", cond.Message)

	t.Log("It verifies cluster extension status when `Unpacked` condition of rukpak is false")
	apimeta.SetStatusCondition(&bd.Status.Conditions, metav1.Condition{
		Type:    rukpakv1alpha2.TypeUnpacked,
		Status:  metav1.ConditionFalse,
		Message: "failed to unpack",
		Reason:  rukpakv1alpha2.ReasonUnpackFailed,
	})

	t.Log("By updating the status of bundleDeployment")
	require.NoError(t, cl.Status().Update(ctx, bd))

	t.Log("By running reconcile")
	res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching the updated cluster extension after reconcile")
	ext = &ocv1alpha1.ClusterExtension{}
	require.NoError(t, cl.Get(ctx, extKey, ext))

	t.Log("By checking the status fields")
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/2.0.0", Version: "2.0.0"}, ext.Status.ResolvedBundle)
	require.Nil(t, ext.Status.InstalledBundle)

	t.Log("By checking the expected conditions")
	cond = apimeta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "bundledeployment status is unknown", cond.Message)

	t.Log("It verifies cluster extension status when `InstallReady` condition of rukpak is false")
	apimeta.SetStatusCondition(&bd.Status.Conditions, metav1.Condition{
		Type:    rukpakv1alpha2.TypeInstalled,
		Status:  metav1.ConditionFalse,
		Message: "failed to install",
		Reason:  rukpakv1alpha2.ReasonInstallFailed,
	})

	t.Log("By updating the status of bundleDeployment")
	require.NoError(t, cl.Status().Update(ctx, bd))

	t.Log("By running reconcile")
	res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching the updated cluster extension after reconcile")
	ext = &ocv1alpha1.ClusterExtension{}
	err = cl.Get(ctx, extKey, ext)
	require.NoError(t, err)

	t.Log("By checking the status fields")
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/2.0.0", Version: "2.0.0"}, ext.Status.ResolvedBundle)
	require.Empty(t, ext.Status.InstalledBundle)

	t.Log("By checking the expected conditions")
	cond = apimeta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)

	cond = apimeta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationFailed, cond.Reason)
	require.Contains(t, cond.Message, `failed to install`)

	t.Log("It verifies cluster extension status when `InstallReady` condition of rukpak is true")
	apimeta.SetStatusCondition(&bd.Status.Conditions, metav1.Condition{
		Type:    rukpakv1alpha2.TypeInstalled,
		Status:  metav1.ConditionTrue,
		Message: "cluster extension installed successfully",
		Reason:  rukpakv1alpha2.ReasonInstallationSucceeded,
	})

	t.Log("By updating the status of bundleDeployment")
	require.NoError(t, cl.Status().Update(ctx, bd))

	t.Log("By running reconcile")
	res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching the updated cluster extension after reconcile")
	ext = &ocv1alpha1.ClusterExtension{}
	require.NoError(t, cl.Get(ctx, extKey, ext))

	t.Log("By checking the status fields")
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/2.0.0", Version: "2.0.0"}, ext.Status.ResolvedBundle)
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/2.0.0", Version: "2.0.0"}, ext.Status.InstalledBundle)

	t.Log("By checking the expected conditions")
	cond = apimeta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "installed from \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)

	t.Log("It verifies any other unknown status of bundledeployment")
	apimeta.SetStatusCondition(&bd.Status.Conditions, metav1.Condition{
		Type:    rukpakv1alpha2.TypeUnpacked,
		Status:  metav1.ConditionUnknown,
		Message: "unpacking",
		Reason:  rukpakv1alpha2.ReasonUnpackSuccessful,
	})

	apimeta.SetStatusCondition(&bd.Status.Conditions, metav1.Condition{
		Type:    rukpakv1alpha2.TypeInstalled,
		Status:  metav1.ConditionUnknown,
		Message: "installing",
		Reason:  rukpakv1alpha2.ReasonInstallationSucceeded,
	})

	t.Log("By updating the status of bundleDeployment")
	require.NoError(t, cl.Status().Update(ctx, bd))

	t.Log("By running reconcile")
	res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching the updated cluster extension after reconcile")
	ext = &ocv1alpha1.ClusterExtension{}
	require.NoError(t, cl.Get(ctx, extKey, ext))

	t.Log("By checking the status fields")
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/2.0.0", Version: "2.0.0"}, ext.Status.ResolvedBundle)
	require.Nil(t, ext.Status.InstalledBundle)

	t.Log("By checking the expected conditions")
	cond = apimeta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)

	cond = apimeta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationFailed, cond.Reason)
	require.Equal(t, "bundledeployment not ready: installing", cond.Message)

	t.Log("It verifies cluster extension status when bundleDeployment installation status is unknown")
	apimeta.SetStatusCondition(&bd.Status.Conditions, metav1.Condition{
		Type:    rukpakv1alpha2.TypeInstalled,
		Status:  metav1.ConditionUnknown,
		Message: "installing",
		Reason:  rukpakv1alpha2.ReasonInstallationSucceeded,
	})

	t.Log("By updating the status of bundleDeployment")
	require.NoError(t, cl.Status().Update(ctx, bd))

	t.Log("running reconcile")
	res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching the updated cluster extension after reconcile")
	ext = &ocv1alpha1.ClusterExtension{}
	require.NoError(t, cl.Get(ctx, extKey, ext))

	t.Log("By checking the status fields")
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/2.0.0", Version: "2.0.0"}, ext.Status.ResolvedBundle)
	require.Nil(t, ext.Status.InstalledBundle)

	t.Log("By checking the expected conditions")
	cond = apimeta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationFailed, cond.Reason)
	require.Equal(t, "bundledeployment not ready: installing", cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, clusterExtension)
	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha2.BundleDeployment{}))
}

func TestClusterExtensionExpectedBundleDeployment(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}
	const pkgName = "prometheus"
	installNamespace := fmt.Sprintf("test-ns-%s", rand.String(8))

	t.Log("When the cluster extension specifies a valid available package")
	t.Log("By initializing cluster state")
	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName:      pkgName,
			InstallNamespace: installNamespace,
		},
	}
	require.NoError(t, cl.Create(ctx, clusterExtension))

	t.Log("When an out-of-date BundleDeployment exists")
	t.Log("By creating the expected BD")
	bd := &rukpakv1alpha2.BundleDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: rukpakv1alpha2.BundleDeploymentSpec{
			InstallNamespace:     "foo",
			ProvisionerClassName: "bar",
			Source: rukpakv1alpha2.BundleSource{
				Type: rukpakv1alpha2.SourceTypeHTTP,
				HTTP: &rukpakv1alpha2.HTTPSource{
					URL: "http://localhost:8080/",
				},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, bd))

	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("It results in the expected BundleDeployment")
	bd = &rukpakv1alpha2.BundleDeployment{}
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: extKey.Name}, bd))
	require.Equal(t, "core-rukpak-io-registry", bd.Spec.ProvisionerClassName)
	require.Equal(t, installNamespace, bd.Spec.InstallNamespace)
	require.Equal(t, rukpakv1alpha2.SourceTypeImage, bd.Spec.Source.Type)
	require.NotNil(t, bd.Spec.Source.Image)
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", bd.Spec.Source.Image.Ref)

	t.Log("It sets the ResolvedBundle status field")
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/2.0.0", Version: "2.0.0"}, clusterExtension.Status.ResolvedBundle)

	t.Log("It sets the InstalledBundle status field")
	require.Empty(t, clusterExtension.Status.InstalledBundle)

	t.Log("It sets resolution to unknown status")
	cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "bundledeployment status is unknown", cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, clusterExtension)
	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha2.BundleDeployment{}))
}

func TestClusterExtensionChannelVersionExists(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a channel with version that exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "1.0.0"
	pkgChan := "beta"
	installNamespace := fmt.Sprintf("test-ns-%s", rand.String(8))

	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName:      pkgName,
			Version:          pkgVer,
			Channel:          pkgChan,
			InstallNamespace: installNamespace,
		},
	}
	err := cl.Create(ctx, clusterExtension)
	require.NoError(t, err)

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/1.0.0", Version: "1.0.0"}, clusterExtension.Status.ResolvedBundle)
	require.Empty(t, clusterExtension.Status.InstalledBundle)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake1.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "bundledeployment status is unknown", cond.Message)

	t.Log("By fetching the bundled deployment")
	bd := &rukpakv1alpha2.BundleDeployment{}
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: extKey.Name}, bd))
	require.Equal(t, "core-rukpak-io-registry", bd.Spec.ProvisionerClassName)
	require.Equal(t, installNamespace, bd.Spec.InstallNamespace)
	require.Equal(t, rukpakv1alpha2.SourceTypeImage, bd.Spec.Source.Type)
	require.NotNil(t, bd.Spec.Source.Image)
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake1.0.0", bd.Spec.Source.Image.Ref)

	verifyInvariants(ctx, t, reconciler.Client, clusterExtension)
	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha2.BundleDeployment{}))
}

func TestClusterExtensionChannelExistsNoVersion(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a package that exists within a channel but no version specified")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := ""
	pkgChan := "beta"
	installNamespace := fmt.Sprintf("test-ns-%s", rand.String(8))
	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName:      pkgName,
			Version:          pkgVer,
			Channel:          pkgChan,
			InstallNamespace: installNamespace,
		},
	}
	require.NoError(t, cl.Create(ctx, clusterExtension))

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)
	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/2.0.0", Version: "2.0.0"}, clusterExtension.Status.ResolvedBundle)
	require.Empty(t, clusterExtension.Status.InstalledBundle)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "bundledeployment status is unknown", cond.Message)

	t.Log("By fetching the bundledeployment")
	bd := &rukpakv1alpha2.BundleDeployment{}
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: extKey.Name}, bd))
	require.Equal(t, "core-rukpak-io-registry", bd.Spec.ProvisionerClassName)
	require.Equal(t, installNamespace, bd.Spec.InstallNamespace)
	require.Equal(t, rukpakv1alpha2.SourceTypeImage, bd.Spec.Source.Type)
	require.NotNil(t, bd.Spec.Source.Image)
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", bd.Spec.Source.Image.Ref)

	verifyInvariants(ctx, t, reconciler.Client, clusterExtension)
	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha2.BundleDeployment{}))
}

func TestClusterExtensionVersionNoChannel(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a package version in a channel that does not exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "0.47.0"
	pkgChan := "alpha"
	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName:      pkgName,
			Version:          pkgVer,
			Channel:          pkgChan,
			InstallNamespace: "default",
		},
	}
	require.NoError(t, cl.Create(ctx, clusterExtension))

	t.Log("It sets resolution failure status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.EqualError(t, err, fmt.Sprintf("no package %q matching version %q found in channel %q", pkgName, pkgVer, pkgChan))

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	require.Empty(t, clusterExtension.Status.ResolvedBundle)
	require.Empty(t, clusterExtension.Status.InstalledBundle)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonResolutionFailed, cond.Reason)
	require.Equal(t, fmt.Sprintf("no package %q matching version %q found in channel %q", pkgName, pkgVer, pkgChan), cond.Message)
	cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)

	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "installation has not been attempted as resolution failed", cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, clusterExtension)
	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha2.BundleDeployment{}))
}

func TestClusterExtensionNoChannel(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a package in a channel that does not exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgChan := "non-existent"
	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName:      pkgName,
			Channel:          pkgChan,
			InstallNamespace: "default",
		},
	}
	require.NoError(t, cl.Create(ctx, clusterExtension))

	t.Log("It sets resolution failure status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.EqualError(t, err, fmt.Sprintf("no package %q found in channel %q", pkgName, pkgChan))

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	require.Empty(t, clusterExtension.Status.ResolvedBundle)
	require.Empty(t, clusterExtension.Status.InstalledBundle)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonResolutionFailed, cond.Reason)
	require.Equal(t, fmt.Sprintf("no package %q found in channel %q", pkgName, pkgChan), cond.Message)
	cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "installation has not been attempted as resolution failed", cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, clusterExtension)
	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha2.BundleDeployment{}))
}

func TestClusterExtensionNoVersion(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a package version that does not exist in the channel")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "0.57.0"
	pkgChan := "non-existent"
	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName:      pkgName,
			Version:          pkgVer,
			Channel:          pkgChan,
			InstallNamespace: "default",
		},
	}
	require.NoError(t, cl.Create(ctx, clusterExtension))

	t.Log("It sets resolution failure status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.EqualError(t, err, fmt.Sprintf("no package %q matching version %q found in channel %q", pkgName, pkgVer, pkgChan))

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	require.Empty(t, clusterExtension.Status.ResolvedBundle)
	require.Empty(t, clusterExtension.Status.InstalledBundle)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonResolutionFailed, cond.Reason)
	require.Equal(t, fmt.Sprintf("no package %q matching version %q found in channel %q", pkgName, pkgVer, pkgChan), cond.Message)
	cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "installation has not been attempted as resolution failed", cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, clusterExtension)
	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha2.BundleDeployment{}))
}

func TestClusterExtensionPlainV0Bundle(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a package with a plain+v0 bundle")
	t.Log("By initializing cluster state")
	pkgName := "plain"
	pkgVer := "0.1.0"
	pkgChan := "beta"
	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName:      pkgName,
			Version:          pkgVer,
			Channel:          pkgChan,
			InstallNamespace: "default",
		},
	}
	require.NoError(t, cl.Create(ctx, clusterExtension))

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/plain/0.1.0", Version: "0.1.0"}, clusterExtension.Status.ResolvedBundle)
	require.Empty(t, clusterExtension.Status.InstalledBundle)
	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhub/plain@sha256:plain\"", cond.Message)
	cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "bundledeployment status is unknown", cond.Message)

	t.Log("By fetching the bundled deployment")
	bd := &rukpakv1alpha2.BundleDeployment{}
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: extKey.Name}, bd))
	require.Equal(t, "core-rukpak-io-plain", bd.Spec.ProvisionerClassName)
	require.Equal(t, rukpakv1alpha2.SourceTypeImage, bd.Spec.Source.Type)
	require.NotNil(t, bd.Spec.Source.Image)
	require.Equal(t, "quay.io/operatorhub/plain@sha256:plain", bd.Spec.Source.Image.Ref)

	verifyInvariants(ctx, t, reconciler.Client, clusterExtension)
	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha2.BundleDeployment{}))
}

func TestClusterExtensionBadBundleMediaType(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a package with a bad bundle mediatype")
	t.Log("By initializing cluster state")
	pkgName := "badmedia"
	pkgVer := "0.1.0"
	pkgChan := "beta"
	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName:      pkgName,
			Version:          pkgVer,
			Channel:          pkgChan,
			InstallNamespace: "default",
		},
	}
	require.NoError(t, cl.Create(ctx, clusterExtension))

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.Error(t, err)
	require.ErrorContains(t, err, "unknown bundle mediatype: badmedia+v1")

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/badmedia/0.1.0", Version: "0.1.0"}, clusterExtension.Status.ResolvedBundle)
	require.Empty(t, clusterExtension.Status.InstalledBundle)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhub/badmedia@sha256:badmedia\"", cond.Message)
	cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationFailed, cond.Reason)
	require.Equal(t, "unknown bundle mediatype: badmedia+v1", cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, clusterExtension)
	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha2.BundleDeployment{}))
}

func verifyInvariants(ctx context.Context, t *testing.T, c client.Client, ext *ocv1alpha1.ClusterExtension) {
	key := client.ObjectKeyFromObject(ext)
	require.NoError(t, c.Get(ctx, key, ext))

	verifyConditionsInvariants(t, ext)
}

func verifyConditionsInvariants(t *testing.T, ext *ocv1alpha1.ClusterExtension) {
	// Expect that the cluster extension's set of conditions contains all defined
	// condition types for the ClusterExtension API. Every reconcile should always
	// ensure every condition type's status/reason/message reflects the state
	// read during _this_ reconcile call.
	require.Len(t, ext.Status.Conditions, len(conditionsets.ConditionTypes))
	for _, tt := range conditionsets.ConditionTypes {
		cond := apimeta.FindStatusCondition(ext.Status.Conditions, tt)
		require.NotNil(t, cond)
		require.NotEmpty(t, cond.Status)
		require.Contains(t, conditionsets.ConditionReasons, cond.Reason)
		require.Equal(t, ext.GetGeneration(), cond.ObservedGeneration)
	}
}

func TestGeneratedBundleDeployment(t *testing.T) {
	test := []struct {
		name              string
		clusterExtension  ocv1alpha1.ClusterExtension
		bundlePath        string
		bundleProvisioner string
	}{
		{
			name: "when all the specs are provided.",
			clusterExtension: ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-bd",
					UID:  types.UID("test"),
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					InstallNamespace: "test-ns",
				},
			},
			bundlePath:        "testpath",
			bundleProvisioner: "foo",
		},
	}

	for _, tt := range test {
		fakeReconciler := &controllers.ClusterExtensionReconciler{}
		objUnstructured := fakeReconciler.GenerateExpectedBundleDeployment(tt.clusterExtension, tt.bundlePath, tt.bundleProvisioner)
		resultBundleDeployment := &rukpakv1alpha2.BundleDeployment{}
		require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(objUnstructured.Object, resultBundleDeployment))
		// Verify the fields that have are being taken from cluster extension.
		require.Equal(t, tt.clusterExtension.GetName(), resultBundleDeployment.GetName())
		require.Equal(t, tt.bundlePath, resultBundleDeployment.Spec.Source.Image.Ref)
		require.Equal(t, tt.bundleProvisioner, resultBundleDeployment.Spec.ProvisionerClassName)
		require.Equal(t, tt.clusterExtension.Spec.InstallNamespace, resultBundleDeployment.Spec.InstallNamespace)
	}
}

func TestClusterExtensionUpgrade(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()

	t.Run("semver upgrade constraints enforcement of upgrades within major version", func(t *testing.T) {
		defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, true)()
		defer func() {
			require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
			require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha2.BundleDeployment{}))
		}()

		pkgName := "prometheus"
		pkgVer := "1.0.0"
		pkgChan := "beta"
		installNamespace := fmt.Sprintf("test-ns-%s", rand.String(8))
		extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}
		clusterExtension := &ocv1alpha1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
			Spec: ocv1alpha1.ClusterExtensionSpec{
				PackageName:      pkgName,
				Version:          pkgVer,
				Channel:          pkgChan,
				InstallNamespace: installNamespace,
			},
		}
		// Create a cluster extension
		err := cl.Create(ctx, clusterExtension)
		require.NoError(t, err)

		// Run reconcile
		res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
		require.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, res)

		// Refresh the cluster extension after reconcile
		err = cl.Get(ctx, extKey, clusterExtension)
		require.NoError(t, err)

		// Checking the status fields
		assert.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/1.0.0", Version: "1.0.0"}, clusterExtension.Status.ResolvedBundle)

		// checking the expected conditions
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
		assert.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
		assert.Equal(t, `resolved to "quay.io/operatorhubio/prometheus@fake1.0.0"`, cond.Message)

		// Invalid update: can not go to the next major version
		clusterExtension.Spec.Version = "2.0.0"
		err = cl.Update(ctx, clusterExtension)
		require.NoError(t, err)

		// Run reconcile again
		res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
		require.Error(t, err)
		assert.Equal(t, ctrl.Result{}, res)

		// Refresh the cluster extension after reconcile
		err = cl.Get(ctx, extKey, clusterExtension)
		require.NoError(t, err)

		// Checking the status fields
		// TODO: https://github.com/operator-framework/operator-controller/issues/320
		assert.Nil(t, clusterExtension.Status.ResolvedBundle)

		// checking the expected conditions
		cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, ocv1alpha1.ReasonResolutionFailed, cond.Reason)
		assert.Equal(t, "error upgrading from currently installed version \"1.0.0\": no package \"prometheus\" matching version \"2.0.0\" found in channel \"beta\"", cond.Message)

		// Valid update skipping one version
		clusterExtension.Spec.Version = "1.2.0"
		err = cl.Update(ctx, clusterExtension)
		require.NoError(t, err)

		// Run reconcile again
		res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
		require.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, res)

		// Refresh the cluster extension after reconcile
		err = cl.Get(ctx, extKey, clusterExtension)
		require.NoError(t, err)

		// Checking the status fields
		assert.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/1.2.0", Version: "1.2.0"}, clusterExtension.Status.ResolvedBundle)

		// checking the expected conditions
		cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
		assert.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
		assert.Equal(t, `resolved to "quay.io/operatorhubio/prometheus@fake1.2.0"`, cond.Message)
	})

	t.Run("legacy semantics upgrade constraints enforcement", func(t *testing.T) {
		defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, false)()
		defer func() {
			require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
			require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha2.BundleDeployment{}))
		}()

		pkgName := "prometheus"
		pkgVer := "1.0.0"
		pkgChan := "beta"
		installNamespace := fmt.Sprintf("test-ns-%s", rand.String(8))
		extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}
		clusterExtension := &ocv1alpha1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
			Spec: ocv1alpha1.ClusterExtensionSpec{
				PackageName:      pkgName,
				Version:          pkgVer,
				Channel:          pkgChan,
				InstallNamespace: installNamespace,
			},
		}
		// Create a cluster extension
		err := cl.Create(ctx, clusterExtension)
		require.NoError(t, err)

		// Run reconcile
		res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
		require.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, res)

		// Refresh the cluster extension after reconcile
		err = cl.Get(ctx, extKey, clusterExtension)
		require.NoError(t, err)

		// Checking the status fields
		assert.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/1.0.0", Version: "1.0.0"}, clusterExtension.Status.ResolvedBundle)

		// checking the expected conditions
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
		assert.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
		assert.Equal(t, `resolved to "quay.io/operatorhubio/prometheus@fake1.0.0"`, cond.Message)

		// Invalid update: can not upgrade by skipping a version in the replaces chain
		clusterExtension.Spec.Version = "1.2.0"
		err = cl.Update(ctx, clusterExtension)
		require.NoError(t, err)

		// Run reconcile again
		res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
		require.Error(t, err)
		assert.Equal(t, ctrl.Result{}, res)

		// Refresh the cluster extension after reconcile
		err = cl.Get(ctx, extKey, clusterExtension)
		require.NoError(t, err)

		// Checking the status fields
		// TODO: https://github.com/operator-framework/operator-controller/issues/320
		assert.Nil(t, clusterExtension.Status.ResolvedBundle)

		// checking the expected conditions
		cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, ocv1alpha1.ReasonResolutionFailed, cond.Reason)
		assert.Equal(t, "error upgrading from currently installed version \"1.0.0\": no package \"prometheus\" matching version \"1.2.0\" found in channel \"beta\"", cond.Message)

		// Valid update skipping one version
		clusterExtension.Spec.Version = "1.0.1"
		err = cl.Update(ctx, clusterExtension)
		require.NoError(t, err)

		// Run reconcile again
		res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
		require.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, res)

		// Refresh the cluster extension after reconcile
		err = cl.Get(ctx, extKey, clusterExtension)
		require.NoError(t, err)

		// Checking the status fields
		assert.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/1.0.1", Version: "1.0.1"}, clusterExtension.Status.ResolvedBundle)

		// checking the expected conditions
		cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
		assert.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
		assert.Equal(t, `resolved to "quay.io/operatorhubio/prometheus@fake1.0.1"`, cond.Message)
	})

	t.Run("ignore upgrade constraints", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			flagState bool
		}{
			{
				name:      "ForceSemverUpgradeConstraints feature gate enabled",
				flagState: true,
			},
			{
				name:      "ForceSemverUpgradeConstraints feature gate disabled",
				flagState: false,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, tt.flagState)()
				defer func() {
					require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
					require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha2.BundleDeployment{}))
				}()

				installNamespace := fmt.Sprintf("test-ns-%s", rand.String(8))
				extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}
				clusterExtension := &ocv1alpha1.ClusterExtension{
					ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
					Spec: ocv1alpha1.ClusterExtensionSpec{
						PackageName:             "prometheus",
						Version:                 "1.0.0",
						Channel:                 "beta",
						UpgradeConstraintPolicy: ocv1alpha1.UpgradeConstraintPolicyIgnore,
						InstallNamespace:        installNamespace,
					},
				}
				// Create a cluster extension
				err := cl.Create(ctx, clusterExtension)
				require.NoError(t, err)

				// Run reconcile
				res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
				require.NoError(t, err)
				assert.Equal(t, ctrl.Result{}, res)

				// Refresh the cluster extension after reconcile
				err = cl.Get(ctx, extKey, clusterExtension)
				require.NoError(t, err)

				// Checking the status fields
				assert.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/1.0.0", Version: "1.0.0"}, clusterExtension.Status.ResolvedBundle)

				// checking the expected conditions
				cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
				require.NotNil(t, cond)
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
				assert.Equal(t, `resolved to "quay.io/operatorhubio/prometheus@fake1.0.0"`, cond.Message)

				// We can go to the next major version when using semver
				// as well as to the version which is not next in the channel
				// when using legacy constraints
				clusterExtension.Spec.Version = "2.0.0"
				err = cl.Update(ctx, clusterExtension)
				require.NoError(t, err)

				// Run reconcile again
				res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
				require.NoError(t, err)
				assert.Equal(t, ctrl.Result{}, res)

				// Refresh the cluster extension after reconcile
				err = cl.Get(ctx, extKey, clusterExtension)
				require.NoError(t, err)

				// Checking the status fields
				assert.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/2.0.0", Version: "2.0.0"}, clusterExtension.Status.ResolvedBundle)

				// checking the expected conditions
				cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
				require.NotNil(t, cond)
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
				assert.Equal(t, `resolved to "quay.io/operatorhubio/prometheus@fake2.0.0"`, cond.Message)
			})
		}
	})
}

func TestClusterExtensionDowngrade(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()

	t.Run("enforce upgrade constraints", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			flagState bool
		}{
			{
				name:      "ForceSemverUpgradeConstraints feature gate enabled",
				flagState: true,
			},
			{
				name:      "ForceSemverUpgradeConstraints feature gate disabled",
				flagState: false,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, tt.flagState)()
				defer func() {
					require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
					require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha2.BundleDeployment{}))
				}()

				installNamespace := fmt.Sprintf("test-ns-%s", rand.String(8))
				extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}
				clusterExtension := &ocv1alpha1.ClusterExtension{
					ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
					Spec: ocv1alpha1.ClusterExtensionSpec{
						PackageName:      "prometheus",
						Version:          "1.0.1",
						Channel:          "beta",
						InstallNamespace: installNamespace,
					},
				}
				// Create a cluster extension
				err := cl.Create(ctx, clusterExtension)
				require.NoError(t, err)

				// Run reconcile
				res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
				require.NoError(t, err)
				assert.Equal(t, ctrl.Result{}, res)

				// Refresh the cluster extension after reconcile
				err = cl.Get(ctx, extKey, clusterExtension)
				require.NoError(t, err)

				// Checking the status fields
				assert.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/1.0.1", Version: "1.0.1"}, clusterExtension.Status.ResolvedBundle)

				// checking the expected conditions
				cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
				require.NotNil(t, cond)
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
				assert.Equal(t, `resolved to "quay.io/operatorhubio/prometheus@fake1.0.1"`, cond.Message)

				// Invalid operation: can not downgrade
				clusterExtension.Spec.Version = "1.0.0"
				err = cl.Update(ctx, clusterExtension)
				require.NoError(t, err)

				// Run reconcile again
				res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
				require.Error(t, err)
				assert.Equal(t, ctrl.Result{}, res)

				// Refresh the cluster extension after reconcile
				err = cl.Get(ctx, extKey, clusterExtension)
				require.NoError(t, err)

				// Checking the status fields
				// TODO: https://github.com/operator-framework/operator-controller/issues/320
				assert.Nil(t, clusterExtension.Status.ResolvedBundle)

				// checking the expected conditions
				cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
				require.NotNil(t, cond)
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
				assert.Equal(t, ocv1alpha1.ReasonResolutionFailed, cond.Reason)
				assert.Equal(t, "error upgrading from currently installed version \"1.0.1\": no package \"prometheus\" matching version \"1.0.0\" found in channel \"beta\"", cond.Message)
			})
		}
	})

	t.Run("ignore upgrade constraints", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			flagState bool
		}{
			{
				name:      "ForceSemverUpgradeConstraints feature gate enabled",
				flagState: true,
			},
			{
				name:      "ForceSemverUpgradeConstraints feature gate disabled",
				flagState: false,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, tt.flagState)()
				defer func() {
					require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
					require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha2.BundleDeployment{}))
				}()

				installNamespace := fmt.Sprintf("test-ns-%s", rand.String(8))
				extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}
				clusterExtension := &ocv1alpha1.ClusterExtension{
					ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
					Spec: ocv1alpha1.ClusterExtensionSpec{
						PackageName:             "prometheus",
						Version:                 "2.0.0",
						Channel:                 "beta",
						UpgradeConstraintPolicy: ocv1alpha1.UpgradeConstraintPolicyIgnore,
						InstallNamespace:        installNamespace,
					},
				}
				// Create a cluster extension
				err := cl.Create(ctx, clusterExtension)
				require.NoError(t, err)

				// Run reconcile
				res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
				require.NoError(t, err)
				assert.Equal(t, ctrl.Result{}, res)

				// Refresh the cluster extension after reconcile
				err = cl.Get(ctx, extKey, clusterExtension)
				require.NoError(t, err)

				// Checking the status fields
				assert.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/2.0.0", Version: "2.0.0"}, clusterExtension.Status.ResolvedBundle)

				// checking the expected conditions
				cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
				require.NotNil(t, cond)
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
				assert.Equal(t, `resolved to "quay.io/operatorhubio/prometheus@fake2.0.0"`, cond.Message)

				// We downgrade
				clusterExtension.Spec.Version = "1.0.0"
				err = cl.Update(ctx, clusterExtension)
				require.NoError(t, err)

				// Run reconcile again
				res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
				require.NoError(t, err)
				assert.Equal(t, ctrl.Result{}, res)

				// Refresh the cluster extension after reconcile
				err = cl.Get(ctx, extKey, clusterExtension)
				require.NoError(t, err)

				// Checking the status fields
				assert.Equal(t, &ocv1alpha1.BundleMetadata{Name: "operatorhub/prometheus/beta/1.0.0", Version: "1.0.0"}, clusterExtension.Status.ResolvedBundle)

				// checking the expected conditions
				cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
				require.NotNil(t, cond)
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
				assert.Equal(t, `resolved to "quay.io/operatorhubio/prometheus@fake1.0.0"`, cond.Message)
			})
		}
	})
}

func TestSetDeprecationStatus(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		clusterExtension         *ocv1alpha1.ClusterExtension
		expectedClusterExtension *ocv1alpha1.ClusterExtension
		bundle                   *catalogmetadata.Bundle
	}{
		{
			name: "non-deprecated bundle, no deprecations associated with bundle, all deprecation statuses set to False",
			clusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1alpha1.TypeDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypePackageDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeChannelDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeBundleDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
					},
				},
			},
			bundle: &catalogmetadata.Bundle{},
		},
		{
			name: "non-deprecated bundle, olm.channel deprecations associated with bundle, no channel specified, all deprecation statuses set to False",
			clusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1alpha1.TypeDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypePackageDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeChannelDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeBundleDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
					},
				},
			},
			bundle: &catalogmetadata.Bundle{
				Deprecations: []declcfg.DeprecationEntry{
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaChannel,
							Name:   "badchannel",
						},
					},
				},
			},
		},
		{
			name: "non-deprecated bundle, olm.channel deprecations associated with bundle, non-deprecated channel specified, all deprecation statuses set to False",
			clusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Channel: "nondeprecated",
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Channel: "nondeprecated",
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1alpha1.TypeDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypePackageDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeChannelDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeBundleDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
					},
				},
			},
			bundle: &catalogmetadata.Bundle{
				Deprecations: []declcfg.DeprecationEntry{
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaChannel,
							Name:   "badchannel",
						},
					},
				},
			},
		},
		{
			name: "non-deprecated bundle, olm.channel deprecations associated with bundle, deprecated channel specified, ChannelDeprecated and Deprecated status set to true",
			clusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Channel: "badchannel",
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Channel: "badchannel",
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1alpha1.TypeDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypePackageDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeChannelDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeBundleDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
					},
				},
			},
			bundle: &catalogmetadata.Bundle{
				Deprecations: []declcfg.DeprecationEntry{
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaChannel,
							Name:   "badchannel",
						},
						Message: "bad channel!",
					},
				},
			},
		},
		{
			name: "deprecated package + bundle, olm.channel deprecations associated with bundle, deprecated channel specified, all deprecation statuses set to true",
			clusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Channel: "badchannel",
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Channel: "badchannel",
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1alpha1.TypeDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypePackageDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeChannelDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeBundleDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
			},
			bundle: &catalogmetadata.Bundle{
				Deprecations: []declcfg.DeprecationEntry{
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaChannel,
							Name:   "badchannel",
						},
						Message: "bad channel!",
					},
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaPackage,
						},
						Message: "bad package!",
					},
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaBundle,
							Name:   "badbundle",
						},
						Message: "bad bundle!",
					},
				},
			},
		},
		{
			name: "deprecated bundle, olm.channel deprecations associated with bundle, deprecated channel specified, all deprecation statuses set to true except PackageDeprecated",
			clusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Channel: "badchannel",
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Channel: "badchannel",
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1alpha1.TypeDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypePackageDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeChannelDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeBundleDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
			},
			bundle: &catalogmetadata.Bundle{
				Deprecations: []declcfg.DeprecationEntry{
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaChannel,
							Name:   "badchannel",
						},
						Message: "bad channel!",
					},
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaBundle,
							Name:   "badbundle",
						},
						Message: "bad bundle!",
					},
				},
			},
		},
		{
			name: "deprecated package, olm.channel deprecations associated with bundle, deprecated channel specified, all deprecation statuses set to true except BundleDeprecated",
			clusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Channel: "badchannel",
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Channel: "badchannel",
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1alpha1.TypeDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypePackageDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeChannelDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeBundleDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
					},
				},
			},
			bundle: &catalogmetadata.Bundle{
				Deprecations: []declcfg.DeprecationEntry{
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaChannel,
							Name:   "badchannel",
						},
						Message: "bad channel!",
					},
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaPackage,
						},
						Message: "bad package!",
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			controllers.SetDeprecationStatus(tc.clusterExtension, tc.bundle)
			assert.Equal(t, "", cmp.Diff(tc.expectedClusterExtension, tc.clusterExtension, cmpopts.IgnoreFields(metav1.Condition{}, "Message", "LastTransitionTime")))
		})
	}
}

var (
	prometheusAlphaChannel = catalogmetadata.Channel{
		Channel: declcfg.Channel{
			Name:    "alpha",
			Package: "prometheus",
		},
	}
	prometheusBetaChannel = catalogmetadata.Channel{
		Channel: declcfg.Channel{
			Name:    "beta",
			Package: "prometheus",
			Entries: []declcfg.ChannelEntry{
				{
					Name: "operatorhub/prometheus/beta/1.0.0",
				},
				{
					Name:     "operatorhub/prometheus/beta/1.0.1",
					Replaces: "operatorhub/prometheus/beta/1.0.0",
				},
				{
					Name:     "operatorhub/prometheus/beta/1.2.0",
					Replaces: "operatorhub/prometheus/beta/1.0.1",
				},
				{
					Name:     "operatorhub/prometheus/beta/2.0.0",
					Replaces: "operatorhub/prometheus/beta/1.2.0",
				},
			},
		},
	}
	plainBetaChannel = catalogmetadata.Channel{
		Channel: declcfg.Channel{
			Name:    "beta",
			Package: "plain",
		},
	}
	badmediaBetaChannel = catalogmetadata.Channel{
		Channel: declcfg.Channel{
			Name:    "beta",
			Package: "badmedia",
		},
	}
)

var testBundleList = []*catalogmetadata.Bundle{
	{
		Bundle: declcfg.Bundle{
			Name:    "operatorhub/prometheus/alpha/0.37.0",
			Package: "prometheus",
			Image:   "quay.io/operatorhubio/prometheus@sha256:3e281e587de3d03011440685fc4fb782672beab044c1ebadc42788ce05a21c35",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"prometheus","version":"0.37.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[]`)},
			},
		},
		CatalogName: "fake-catalog",
		InChannels:  []*catalogmetadata.Channel{&prometheusAlphaChannel},
	},
	{
		Bundle: declcfg.Bundle{
			Name:    "operatorhub/prometheus/beta/1.0.0",
			Package: "prometheus",
			Image:   "quay.io/operatorhubio/prometheus@fake1.0.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"prometheus","version":"1.0.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[]`)},
			},
		},
		CatalogName: "fake-catalog",
		InChannels:  []*catalogmetadata.Channel{&prometheusBetaChannel},
	},
	{
		Bundle: declcfg.Bundle{
			Name:    "operatorhub/prometheus/beta/1.0.1",
			Package: "prometheus",
			Image:   "quay.io/operatorhubio/prometheus@fake1.0.1",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"prometheus","version":"1.0.1"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[]`)},
			},
		},
		CatalogName: "fake-catalog",
		InChannels:  []*catalogmetadata.Channel{&prometheusBetaChannel},
	},
	{
		Bundle: declcfg.Bundle{
			Name:    "operatorhub/prometheus/beta/1.2.0",
			Package: "prometheus",
			Image:   "quay.io/operatorhubio/prometheus@fake1.2.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"prometheus","version":"1.2.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[]`)},
			},
		},
		CatalogName: "fake-catalog",
		InChannels:  []*catalogmetadata.Channel{&prometheusBetaChannel},
	},
	{
		Bundle: declcfg.Bundle{
			Name:    "operatorhub/prometheus/beta/2.0.0",
			Package: "prometheus",
			Image:   "quay.io/operatorhubio/prometheus@fake2.0.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"prometheus","version":"2.0.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[]`)},
			},
		},
		CatalogName: "fake-catalog",
		InChannels:  []*catalogmetadata.Channel{&prometheusBetaChannel},
	},
	{
		Bundle: declcfg.Bundle{
			Name:    "operatorhub/plain/0.1.0",
			Package: "plain",
			Image:   "quay.io/operatorhub/plain@sha256:plain",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"plain","version":"0.1.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[]`)},
				{Type: "olm.bundle.mediatype", Value: json.RawMessage(`"plain+v0"`)},
			},
		},
		CatalogName: "fake-catalog",
		InChannels:  []*catalogmetadata.Channel{&plainBetaChannel},
	},
	{
		Bundle: declcfg.Bundle{
			Name:    "operatorhub/badmedia/0.1.0",
			Package: "badmedia",
			Image:   "quay.io/operatorhub/badmedia@sha256:badmedia",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"badmedia","version":"0.1.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[]`)},
				{Type: "olm.bundle.mediatype", Value: json.RawMessage(`"badmedia+v1"`)},
			},
		},
		CatalogName: "fake-catalog",
		InChannels:  []*catalogmetadata.Channel{&badmediaBetaChannel},
	},
}
