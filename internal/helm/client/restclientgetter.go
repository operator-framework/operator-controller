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
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func newRESTClientGetter(cfg *rest.Config, rm meta.RESTMapper, cachedDiscoveryClient discovery.CachedDiscoveryInterface, ns string) *namespacedRCG {
	return &namespacedRCG{
		restClientGetter: &restClientGetter{
			restConfig:            cfg,
			restMapper:            rm,
			cachedDiscoveryClient: cachedDiscoveryClient,
		},
		namespaceConfig: namespaceClientConfig{ns},
	}
}

type restClientGetter struct {
	restConfig            *rest.Config
	restMapper            meta.RESTMapper
	cachedDiscoveryClient discovery.CachedDiscoveryInterface
}

func (c *restClientGetter) ToRESTConfig() (*rest.Config, error) {
	return rest.CopyConfig(c.restConfig), nil
}

func (c *restClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	return c.cachedDiscoveryClient, nil
}

func (c *restClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	return c.restMapper, nil
}

func (c *restClientGetter) ForNamespace(ns string) genericclioptions.RESTClientGetter {
	return &namespacedRCG{
		restClientGetter: c,
		namespaceConfig:  namespaceClientConfig{namespace: ns},
	}
}

var _ genericclioptions.RESTClientGetter = &namespacedRCG{}

type namespacedRCG struct {
	*restClientGetter
	namespaceConfig namespaceClientConfig
}

func (c *namespacedRCG) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return c.namespaceConfig
}

var _ clientcmd.ClientConfig = &namespaceClientConfig{}

type namespaceClientConfig struct {
	namespace string
}

func (c namespaceClientConfig) RawConfig() (clientcmdapi.Config, error) {
	return clientcmdapi.Config{}, nil
}

func (c namespaceClientConfig) ClientConfig() (*rest.Config, error) {
	return nil, nil
}

func (c namespaceClientConfig) Namespace() (string, bool, error) {
	return c.namespace, false, nil
}

func (c namespaceClientConfig) ConfigAccess() clientcmd.ConfigAccess {
	return nil
}
