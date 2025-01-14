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
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ActionConfigGetter interface {
	ActionConfigFor(ctx context.Context, obj client.Object) (*action.Configuration, error)
}

func NewActionConfigGetter(baseRestConfig *rest.Config, rm meta.RESTMapper, opts ...ActionConfigGetterOption) (ActionConfigGetter, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(baseRestConfig)
	if err != nil {
		return nil, fmt.Errorf("create discovery client: %v", err)
	}
	cdc := memory.NewMemCacheClient(dc)

	acg := &actionConfigGetter{
		baseRestConfig:  baseRestConfig,
		restMapper:      rm,
		discoveryClient: cdc,
	}
	for _, o := range opts {
		o(acg)
	}
	if acg.objectToClientNamespace == nil {
		acg.objectToClientNamespace = getObjectNamespace
	}
	if acg.objectToClientRestConfig == nil {
		acg.objectToClientRestConfig = func(_ context.Context, _ client.Object, baseRestConfig *rest.Config) (*rest.Config, error) {
			return rest.CopyConfig(baseRestConfig), nil
		}
	}
	if acg.objectToStorageRestConfig == nil {
		acg.objectToStorageRestConfig = func(_ context.Context, _ client.Object, baseRestConfig *rest.Config) (*rest.Config, error) {
			return rest.CopyConfig(baseRestConfig), nil
		}
	}
	if acg.objectToStorageDriver == nil {
		if acg.objectToStorageNamespace == nil {
			acg.objectToStorageNamespace = getObjectNamespace
		}
		acg.objectToStorageDriver = DefaultSecretsStorageDriver(SecretsStorageDriverOpts{
			DisableOwnerRefInjection: acg.disableStorageOwnerRefInjection,
			StorageNamespaceMapper:   acg.objectToStorageNamespace,
		})
	}
	return acg, nil
}

var _ ActionConfigGetter = &actionConfigGetter{}

type ActionConfigGetterOption func(getter *actionConfigGetter)

type ObjectToStringMapper func(client.Object) (string, error)
type ObjectToRestConfigMapper func(context.Context, client.Object, *rest.Config) (*rest.Config, error)
type ObjectToStorageDriverMapper func(context.Context, client.Object, *rest.Config) (driver.Driver, error)

func ClientRestConfigMapper(f ObjectToRestConfigMapper) ActionConfigGetterOption { // nolint:revive
	return func(getter *actionConfigGetter) {
		getter.objectToClientRestConfig = f
	}
}

func ClientNamespaceMapper(m ObjectToStringMapper) ActionConfigGetterOption { // nolint:revive
	return func(getter *actionConfigGetter) {
		getter.objectToClientNamespace = m
	}
}

func StorageRestConfigMapper(f ObjectToRestConfigMapper) ActionConfigGetterOption {
	return func(getter *actionConfigGetter) {
		getter.objectToStorageRestConfig = f
	}
}

func StorageDriverMapper(f ObjectToStorageDriverMapper) ActionConfigGetterOption {
	return func(getter *actionConfigGetter) {
		getter.objectToStorageDriver = f
	}
}

// Deprecated: use StorageDriverMapper(DefaultSecretsStorageDriver(SecretsStorageDriverOpts)) instead.
func StorageNamespaceMapper(m ObjectToStringMapper) ActionConfigGetterOption {
	return func(getter *actionConfigGetter) {
		getter.objectToStorageNamespace = m
	}
}

// Deprecated: use StorageDriverMapper(DefaultSecretsStorageDriver(SecretsStorageDriverOpts)) instead.
func DisableStorageOwnerRefInjection(v bool) ActionConfigGetterOption {
	return func(getter *actionConfigGetter) {
		getter.disableStorageOwnerRefInjection = v
	}
}

// Deprecated: use ClientRestConfigMapper and StorageRestConfigMapper instead.
func RestConfigMapper(f func(context.Context, client.Object, *rest.Config) (*rest.Config, error)) ActionConfigGetterOption {
	return func(getter *actionConfigGetter) {
		getter.objectToClientRestConfig = f
		getter.objectToStorageRestConfig = f
	}
}

func getObjectNamespace(obj client.Object) (string, error) {
	return obj.GetNamespace(), nil
}

type actionConfigGetter struct {
	baseRestConfig  *rest.Config
	restMapper      meta.RESTMapper
	discoveryClient discovery.CachedDiscoveryInterface

	objectToClientRestConfig ObjectToRestConfigMapper
	objectToClientNamespace  ObjectToStringMapper

	objectToStorageRestConfig ObjectToRestConfigMapper
	objectToStorageDriver     ObjectToStorageDriverMapper

	// Deprecated: only keep around for backward compatibility with StorageNamespaceMapper option.
	objectToStorageNamespace ObjectToStringMapper
	// Deprecated: only keep around for backward compatibility with DisableStorageOwnerRefInjection option.
	disableStorageOwnerRefInjection bool
}

func (acg *actionConfigGetter) ActionConfigFor(ctx context.Context, obj client.Object) (*action.Configuration, error) {
	clientRestConfig, err := acg.objectToClientRestConfig(ctx, obj, acg.baseRestConfig)
	if err != nil {
		return nil, fmt.Errorf("get client rest config for object: %v", err)
	}

	clientNamespace, err := acg.objectToClientNamespace(obj)
	if err != nil {
		return nil, fmt.Errorf("get client namespace for object: %v", err)
	}

	clientRCG := newRESTClientGetter(clientRestConfig, acg.restMapper, acg.discoveryClient, clientNamespace)
	clientKC := kube.New(clientRCG)
	clientKC.Namespace = clientNamespace

	// Setup the debug log function that Helm will use
	debugLog := getDebugLogger(ctx)

	storageRestConfig, err := acg.objectToStorageRestConfig(ctx, obj, acg.baseRestConfig)
	if err != nil {
		return nil, fmt.Errorf("get storage rest config for object: %v", err)
	}

	d, err := acg.objectToStorageDriver(ctx, obj, storageRestConfig)
	if err != nil {
		return nil, fmt.Errorf("get storage driver for object: %v", err)
	}

	// Initialize the storage backend
	s := storage.Init(d)

	return &action.Configuration{
		RESTClientGetter: clientRCG,
		Releases:         s,
		KubeClient:       clientKC,
		Log:              debugLog,
	}, nil
}

func getDebugLogger(ctx context.Context) func(format string, v ...interface{}) {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return func(_ string, _ ...interface{}) {}
	}
	return func(format string, v ...interface{}) {
		logger.V(1).Info(fmt.Sprintf(format, v...))
	}
}

type SecretsStorageDriverOpts struct {
	DisableOwnerRefInjection bool
	StorageNamespaceMapper   ObjectToStringMapper
}

func DefaultSecretsStorageDriver(opts SecretsStorageDriverOpts) ObjectToStorageDriverMapper {
	if opts.StorageNamespaceMapper == nil {
		opts.StorageNamespaceMapper = getObjectNamespace
	}
	return func(ctx context.Context, obj client.Object, restConfig *rest.Config) (driver.Driver, error) {
		storageNamespace, err := opts.StorageNamespaceMapper(obj)
		if err != nil {
			return nil, fmt.Errorf("get storage namespace for object: %v", err)
		}
		secretsInterface, err := v1.NewForConfig(restConfig)
		if err != nil {
			return nil, fmt.Errorf("create secrets client for storage: %v", err)
		}

		secretClient := secretsInterface.Secrets(storageNamespace)
		if !opts.DisableOwnerRefInjection {
			ownerRef := metav1.NewControllerRef(obj, obj.GetObjectKind().GroupVersionKind())
			secretClient = NewOwnerRefSecretClient(secretClient, []metav1.OwnerReference{*ownerRef}, MatchAllSecrets)
		}
		d := driver.NewSecrets(secretClient)
		d.Log = getDebugLogger(ctx)
		return d, nil
	}
}
