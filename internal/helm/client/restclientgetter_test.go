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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

var _ = Describe("restClientGetter", func() {
	var (
		rm  meta.RESTMapper
		rcg genericclioptions.RESTClientGetter
	)

	BeforeEach(func() {
		var err error

		httpClient, err := rest.HTTPClientFor(cfg)
		Expect(err).NotTo(HaveOccurred())

		rm, err = apiutil.NewDynamicRESTMapper(cfg, httpClient)
		Expect(err).ToNot(HaveOccurred())

		dc, err := discovery.NewDiscoveryClientForConfig(cfg)
		Expect(err).ToNot(HaveOccurred())
		cdc := memory.NewMemCacheClient(dc)

		rcg = newRESTClientGetter(cfg, rm, cdc, "test-ns")
		Expect(rcg).NotTo(BeNil())
	})

	It("returns the configured rest config", func() {
		restConfig, err := rcg.ToRESTConfig()
		Expect(err).ToNot(HaveOccurred())
		Expect(restConfig).To(Equal(cfg))
	})

	It("returns a valid discovery client", func() {
		cdc, err := rcg.ToDiscoveryClient()
		Expect(err).ToNot(HaveOccurred())
		Expect(cdc).NotTo(BeNil())

		vers, err := cdc.ServerVersion()
		Expect(err).ToNot(HaveOccurred())
		Expect(vers.GitTreeState).To(Equal("clean"))
	})

	It("returns the configured rest mapper", func() {
		restMapper, err := rcg.ToRESTMapper()
		Expect(err).ToNot(HaveOccurred())
		Expect(restMapper).To(Equal(rm))
	})

	It("returns a minimal raw kube config loader", func() {
		rkcl := rcg.ToRawKubeConfigLoader()
		Expect(rkcl).NotTo(BeNil())

		By("verifying the namespace", func() {
			ns, _, err := rkcl.Namespace()
			Expect(err).ToNot(HaveOccurred())
			Expect(ns).To(Equal("test-ns"))
		})

		By("verifying raw config is empty", func() {
			rc, err := rkcl.RawConfig()
			Expect(err).ToNot(HaveOccurred())
			Expect(rc).To(Equal(clientcmdapi.Config{}))
		})

		By("verifying client config is empty", func() {
			cc, err := rkcl.ClientConfig()
			Expect(err).ToNot(HaveOccurred())
			Expect(cc).To(BeNil())
		})

		By("verifying config access is nil", func() {
			Expect(rkcl.ConfigAccess()).To(BeNil())
		})
	})
})
