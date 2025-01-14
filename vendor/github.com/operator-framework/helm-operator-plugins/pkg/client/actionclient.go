/*
Copyright 2020 The Operator-SDK Authors.

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

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gomodules.xyz/jsonpatch/v2"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	helmkube "helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/releaseutil"
	"helm.sh/helm/v3/pkg/storage/driver"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/cli-runtime/pkg/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ActionClientGetter interface {
	ActionClientFor(ctx context.Context, obj client.Object) (ActionInterface, error)
}

type ActionClientGetterFunc func(ctx context.Context, obj client.Object) (ActionInterface, error)

func (acgf ActionClientGetterFunc) ActionClientFor(ctx context.Context, obj client.Object) (ActionInterface, error) {
	return acgf(ctx, obj)
}

type ActionInterface interface {
	Get(name string, opts ...GetOption) (*release.Release, error)
	History(name string, opts ...HistoryOption) ([]*release.Release, error)
	Install(name, namespace string, chrt *chart.Chart, vals map[string]interface{}, opts ...InstallOption) (*release.Release, error)
	Upgrade(name, namespace string, chrt *chart.Chart, vals map[string]interface{}, opts ...UpgradeOption) (*release.Release, error)
	Uninstall(name string, opts ...UninstallOption) (*release.UninstallReleaseResponse, error)
	Reconcile(rel *release.Release) error
}

type GetOption func(*action.Get) error
type HistoryOption func(*action.History) error
type InstallOption func(*action.Install) error
type UpgradeOption func(*action.Upgrade) error
type UninstallOption func(*action.Uninstall) error
type RollbackOption func(*action.Rollback) error

type ActionClientGetterOption func(*actionClientGetter) error

func AppendGetOptions(opts ...GetOption) ActionClientGetterOption {
	return func(getter *actionClientGetter) error {
		getter.defaultGetOpts = append(getter.defaultGetOpts, opts...)
		return nil
	}
}

func AppendHistoryOptions(opts ...HistoryOption) ActionClientGetterOption {
	return func(getter *actionClientGetter) error {
		getter.defaultHistoryOpts = append(getter.defaultHistoryOpts, opts...)
		return nil
	}
}

func AppendInstallOptions(opts ...InstallOption) ActionClientGetterOption {
	return func(getter *actionClientGetter) error {
		getter.defaultInstallOpts = append(getter.defaultInstallOpts, opts...)
		return nil
	}
}

func AppendUpgradeOptions(opts ...UpgradeOption) ActionClientGetterOption {
	return func(getter *actionClientGetter) error {
		getter.defaultUpgradeOpts = append(getter.defaultUpgradeOpts, opts...)
		return nil
	}
}

func AppendUninstallOptions(opts ...UninstallOption) ActionClientGetterOption {
	return func(getter *actionClientGetter) error {
		getter.defaultUninstallOpts = append(getter.defaultUninstallOpts, opts...)
		return nil
	}
}

func AppendInstallFailureUninstallOptions(opts ...UninstallOption) ActionClientGetterOption {
	return func(getter *actionClientGetter) error {
		getter.installFailureUninstallOpts = append(getter.installFailureUninstallOpts, opts...)
		return nil
	}
}

func AppendUpgradeFailureRollbackOptions(opts ...RollbackOption) ActionClientGetterOption {
	return func(getter *actionClientGetter) error {
		getter.upgradeFailureRollbackOpts = append(getter.upgradeFailureRollbackOpts, opts...)
		return nil
	}
}

func AppendPostRenderers(postRendererFns ...PostRendererProvider) ActionClientGetterOption {
	return func(getter *actionClientGetter) error {
		getter.postRendererProviders = append(getter.postRendererProviders, postRendererFns...)
		return nil
	}
}

func WithFailureRollbacks(enableFailureRollbacks bool) ActionClientGetterOption {
	return func(getter *actionClientGetter) error {
		getter.enableFailureRollbacks = enableFailureRollbacks
		return nil
	}
}

func NewActionClientGetter(acg ActionConfigGetter, opts ...ActionClientGetterOption) (ActionClientGetter, error) {
	actionClientGetter := &actionClientGetter{
		acg:                    acg,
		enableFailureRollbacks: true,
	}
	for _, opt := range opts {
		if err := opt(actionClientGetter); err != nil {
			return nil, err
		}
	}
	return actionClientGetter, nil
}

type actionClientGetter struct {
	acg ActionConfigGetter

	defaultGetOpts       []GetOption
	defaultHistoryOpts   []HistoryOption
	defaultInstallOpts   []InstallOption
	defaultUpgradeOpts   []UpgradeOption
	defaultUninstallOpts []UninstallOption

	enableFailureRollbacks      bool
	installFailureUninstallOpts []UninstallOption
	upgradeFailureRollbackOpts  []RollbackOption

	postRendererProviders []PostRendererProvider
}

var _ ActionClientGetter = &actionClientGetter{}

func (hcg *actionClientGetter) ActionClientFor(ctx context.Context, obj client.Object) (ActionInterface, error) {
	actionConfig, err := hcg.acg.ActionConfigFor(ctx, obj)
	if err != nil {
		return nil, err
	}
	rm, err := actionConfig.RESTClientGetter.ToRESTMapper()
	if err != nil {
		return nil, err
	}
	var cpr = chainedPostRenderer{}
	for _, provider := range hcg.postRendererProviders {
		cpr = append(cpr, provider(rm, actionConfig.KubeClient, obj))
	}
	cpr = append(cpr, DefaultPostRendererFunc(rm, actionConfig.KubeClient, obj))

	return &actionClient{
		conf: actionConfig,

		// For the install and upgrade options, we put the post renderer first in the list
		// on purpose because we want user-provided defaults to be able to override the
		// post-renderer that we automatically configure for the client.
		defaultGetOpts:       hcg.defaultGetOpts,
		defaultHistoryOpts:   hcg.defaultHistoryOpts,
		defaultInstallOpts:   append([]InstallOption{WithInstallPostRenderer(cpr)}, hcg.defaultInstallOpts...),
		defaultUpgradeOpts:   append([]UpgradeOption{WithUpgradePostRenderer(cpr)}, hcg.defaultUpgradeOpts...),
		defaultUninstallOpts: hcg.defaultUninstallOpts,

		enableFailureRollbacks:      hcg.enableFailureRollbacks,
		installFailureUninstallOpts: hcg.installFailureUninstallOpts,
		upgradeFailureRollbackOpts:  hcg.upgradeFailureRollbackOpts,
	}, nil
}

type actionClient struct {
	conf *action.Configuration

	defaultGetOpts       []GetOption
	defaultHistoryOpts   []HistoryOption
	defaultInstallOpts   []InstallOption
	defaultUpgradeOpts   []UpgradeOption
	defaultUninstallOpts []UninstallOption

	enableFailureRollbacks      bool
	installFailureUninstallOpts []UninstallOption
	upgradeFailureRollbackOpts  []RollbackOption
}

var _ ActionInterface = &actionClient{}

func (c *actionClient) Get(name string, opts ...GetOption) (*release.Release, error) {
	get := action.NewGet(c.conf)
	for _, o := range concat(c.defaultGetOpts, opts...) {
		if err := o(get); err != nil {
			return nil, err
		}
	}
	return get.Run(name)
}

// History returns the release history for a given release name. The releases are sorted
// by revision number in descending order.
func (c *actionClient) History(name string, opts ...HistoryOption) ([]*release.Release, error) {
	history := action.NewHistory(c.conf)
	for _, o := range concat(c.defaultHistoryOpts, opts...) {
		if err := o(history); err != nil {
			return nil, err
		}
	}
	rels, err := history.Run(name)
	if err != nil {
		return nil, err
	}
	releaseutil.Reverse(rels, releaseutil.SortByRevision)
	return rels, nil
}

func (c *actionClient) Install(name, namespace string, chrt *chart.Chart, vals map[string]interface{}, opts ...InstallOption) (*release.Release, error) {
	install := action.NewInstall(c.conf)
	for _, o := range concat(c.defaultInstallOpts, opts...) {
		if err := o(install); err != nil {
			return nil, err
		}
	}
	install.ReleaseName = name
	install.Namespace = namespace
	c.conf.Log("Starting install")
	rel, err := install.Run(chrt, vals)
	if err != nil {
		c.conf.Log("Install failed")
		if c.enableFailureRollbacks && rel != nil {
			// Uninstall the failed release installation so that we can retry
			// the installation again during the next reconciliation. In many
			// cases, the issue is unresolvable without a change to the CR, but
			// controller-runtime will backoff on retries after failed attempts.
			//
			// In certain cases, Install will return a partial release in
			// the response even when it doesn't record the release in its release
			// store (e.g. when there is an error rendering the release manifest).
			// In that case the rollback will fail with a not found error because
			// there was nothing to rollback.
			//
			// Only return an error about a rollback failure if the failure was
			// caused by something other than the release not being found.
			_, uninstallErr := c.uninstall(name, c.installFailureUninstallOpts...)
			if uninstallErr != nil && !errors.Is(uninstallErr, driver.ErrReleaseNotFound) {
				return nil, fmt.Errorf("uninstall failed: %v: original install error: %w", uninstallErr, err)
			}
		}
		return rel, err
	}
	return rel, nil
}

func (c *actionClient) Upgrade(name, namespace string, chrt *chart.Chart, vals map[string]interface{}, opts ...UpgradeOption) (*release.Release, error) {
	upgrade := action.NewUpgrade(c.conf)
	for _, o := range concat(c.defaultUpgradeOpts, opts...) {
		if err := o(upgrade); err != nil {
			return nil, err
		}
	}
	upgrade.Namespace = namespace
	rel, err := upgrade.Run(name, chrt, vals)
	if err != nil {
		if c.enableFailureRollbacks && rel != nil {
			rollbackOpts := append([]RollbackOption{func(rollback *action.Rollback) error {
				rollback.Force = true
				rollback.MaxHistory = upgrade.MaxHistory
				return nil
			}}, c.upgradeFailureRollbackOpts...)

			// As of Helm 2.13, if Upgrade returns a non-nil release, that
			// means the release was also recorded in the release store.
			// Therefore, we should perform the rollback when we have a non-nil
			// release. Any rollback error here would be unexpected, so always
			// log both the update and rollback errors.
			rollbackErr := c.rollback(name, rollbackOpts...)
			if rollbackErr != nil {
				return nil, fmt.Errorf("rollback failed: %v: original upgrade error: %w", rollbackErr, err)
			}
		}
		return rel, err
	}
	return rel, nil
}

func (c *actionClient) rollback(name string, opts ...RollbackOption) error {
	rollback := action.NewRollback(c.conf)
	for _, o := range opts {
		if err := o(rollback); err != nil {
			return err
		}
	}
	return rollback.Run(name)
}

func (c *actionClient) Uninstall(name string, opts ...UninstallOption) (*release.UninstallReleaseResponse, error) {
	return c.uninstall(name, concat(c.defaultUninstallOpts, opts...)...)
}

func (c *actionClient) uninstall(name string, opts ...UninstallOption) (*release.UninstallReleaseResponse, error) {
	uninstall := action.NewUninstall(c.conf)
	for _, o := range opts {
		if err := o(uninstall); err != nil {
			return nil, err
		}
	}
	return uninstall.Run(name)
}

func (c *actionClient) Reconcile(rel *release.Release) error {
	infos, err := c.conf.KubeClient.Build(bytes.NewBufferString(rel.Manifest), false)
	if err != nil {
		return err
	}
	return infos.Visit(func(expected *resource.Info, err error) error {
		if err != nil {
			return fmt.Errorf("visit error: %w", err)
		}

		helper := resource.NewHelper(expected.Client, expected.Mapping)

		existing, err := helper.Get(expected.Namespace, expected.Name)
		if apierrors.IsNotFound(err) {
			if _, err := helper.Create(expected.Namespace, true, expected.Object); err != nil {
				return fmt.Errorf("create error: %w", err)
			}
			return nil
		} else if err != nil {
			return fmt.Errorf("could not get object: %w", err)
		}

		patch, patchType, err := createPatch(existing, expected)
		if err != nil {
			return fmt.Errorf("error creating patch: %w", err)
		}

		if patch == nil {
			// nothing to do
			return nil
		}

		_, err = helper.Patch(expected.Namespace, expected.Name, patchType, patch,
			&metav1.PatchOptions{})
		if err != nil {
			return fmt.Errorf("patch error: %w", err)
		}
		return nil
	})
}

func createPatch(existing runtime.Object, expected *resource.Info) ([]byte, apitypes.PatchType, error) {
	existingJSON, err := json.Marshal(existing)
	if err != nil {
		return nil, apitypes.StrategicMergePatchType, err
	}
	expectedJSON, err := json.Marshal(expected.Object)
	if err != nil {
		return nil, apitypes.StrategicMergePatchType, err
	}

	// Get a versioned object
	versionedObject := helmkube.AsVersioned(expected)

	// Unstructured objects, such as CRDs, may not have an not registered error
	// returned from ConvertToVersion. Anything that's unstructured should
	// use the jsonpatch.CreateMergePatch. Strategic Merge Patch is not supported
	// on objects like CRDs.
	_, isUnstructured := versionedObject.(runtime.Unstructured)

	// On newer K8s versions, CRDs aren't unstructured but has this dedicated type
	_, isCRDv1beta1 := versionedObject.(*apiextv1beta1.CustomResourceDefinition)
	_, isCRDv1 := versionedObject.(*apiextensionsv1.CustomResourceDefinition)

	if isUnstructured || isCRDv1beta1 || isCRDv1 {
		// fall back to generic JSON merge patch
		patch, err := createJSONMergePatch(existingJSON, expectedJSON)
		return patch, apitypes.JSONPatchType, err
	}

	patchMeta, err := strategicpatch.NewPatchMetaFromStruct(versionedObject)
	if err != nil {
		return nil, apitypes.StrategicMergePatchType, err
	}
	patch, err := strategicpatch.CreateThreeWayMergePatch(expectedJSON, expectedJSON, existingJSON, patchMeta, true)
	if err != nil {
		return nil, apitypes.StrategicMergePatchType, err
	}

	// An empty patch could be in the form of "{}" which represents an empty map out of the 3-way merge;
	// filter them out here too to avoid sending the apiserver empty patch requests.
	if len(patch) == 0 || bytes.Equal(patch, []byte("{}")) {
		return nil, apitypes.StrategicMergePatchType, nil
	}
	return patch, apitypes.StrategicMergePatchType, nil
}

func createJSONMergePatch(existingJSON, expectedJSON []byte) ([]byte, error) {
	ops, err := jsonpatch.CreatePatch(existingJSON, expectedJSON)
	if err != nil {
		return nil, err
	}

	// We ignore the "remove" operations from the full patch because they are
	// fields added by Kubernetes or by the user after the existing release
	// resource has been applied. The goal for this patch is to make sure that
	// the fields managed by the Helm chart are applied.
	// All "add" operations without a value (null) can be ignored
	patchOps := make([]jsonpatch.JsonPatchOperation, 0)
	for _, op := range ops {
		if op.Operation != "remove" && !(op.Operation == "add" && op.Value == nil) {
			patchOps = append(patchOps, op)
		}
	}

	// If there are no patch operations, return nil. Callers are expected
	// to check for a nil response and skip the patch operation to avoid
	// unnecessary chatter with the API server.
	if len(patchOps) == 0 {
		return nil, nil
	}

	return json.Marshal(patchOps)
}

func concat[T any](s1 []T, s2 ...T) []T {
	out := make([]T, 0, len(s1)+len(s2))
	out = append(out, s1...)
	out = append(out, s2...)
	return out
}
