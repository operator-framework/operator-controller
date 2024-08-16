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

package controllerutil_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/operator-framework/operator-controller/internal/controllerutil"
)

var _ = Describe("Controllerutil", func() {
	Describe("WaitForDeletion", func() {
		var (
			ctx    context.Context
			cancel context.CancelFunc
			pod    *corev1.Pod
			client client.Client
		)

		BeforeEach(func() {
			ctx, cancel = context.WithCancel(context.Background())
			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testName",
					Namespace: "testNamespace",
				},
			}
			client = fake.NewClientBuilder().
				WithObjects(pod).
				Build()
		})

		AfterEach(func() {
			cancel()
		})

		It("should be cancellable", func() {
			cancel()
			err := WaitForDeletion(ctx, client, pod)
			// wait.ErrWaitTimeOut is deprecated. The method will poll till context is alive and
			// if a cancelled context is passed it would error.
			Expect(err).To(HaveOccurred())
		})

		It("should succeed after pod is deleted", func() {
			Expect(client.Delete(ctx, pod)).To(Succeed())
			Expect(WaitForDeletion(ctx, client, pod)).To(Succeed())
		})
	})

	Describe("SupportsOwnerReference", func() {
		var (
			rm              *meta.DefaultRESTMapper
			owner           client.Object
			dependent       client.Object
			clusterScoped   = schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "ClusterScoped"}
			namespaceScoped = schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "NamespaceScoped"}
		)
		When("GVK REST mappings exist", func() {
			BeforeEach(func() {
				rm = meta.NewDefaultRESTMapper([]schema.GroupVersion{})
				rm.Add(clusterScoped, meta.RESTScopeRoot)
				rm.Add(namespaceScoped, meta.RESTScopeNamespace)
			})
			When("owner is cluster scoped", func() {
				BeforeEach(func() {
					owner = createObject(clusterScoped, types.NamespacedName{Namespace: "", Name: "owner"})
				})
				It("should be true for cluster-scoped dependents", func() {
					dependent = createObject(clusterScoped, types.NamespacedName{Namespace: "", Name: "dependent"})
					supportsOwnerRef, err := SupportsOwnerReference(rm, owner, dependent)
					Expect(supportsOwnerRef).To(BeTrue())
					Expect(err).ToNot(HaveOccurred())
				})
				It("should be true for namespace-scoped dependents", func() {
					dependent = createObject(namespaceScoped, types.NamespacedName{Namespace: "ns1", Name: "dependent"})
					supportsOwnerRef, err := SupportsOwnerReference(rm, owner, dependent)
					Expect(supportsOwnerRef).To(BeTrue())
					Expect(err).ToNot(HaveOccurred())
				})
			})
			When("owner is namespace scoped", func() {
				BeforeEach(func() {
					owner = createObject(namespaceScoped, types.NamespacedName{Namespace: "ns1", Name: "owner"})
				})
				It("should be false for cluster-scoped dependents", func() {
					dependent = createObject(clusterScoped, types.NamespacedName{Namespace: "", Name: "dependent"})
					supportsOwnerRef, err := SupportsOwnerReference(rm, owner, dependent)
					Expect(supportsOwnerRef).To(BeFalse())
					Expect(err).ToNot(HaveOccurred())
				})
				When("dependent is in owner namespace", func() {
					It("should be true", func() {
						dependent = createObject(namespaceScoped, types.NamespacedName{Namespace: "ns1", Name: "dependent"})
						supportsOwnerRef, err := SupportsOwnerReference(rm, owner, dependent)
						Expect(supportsOwnerRef).To(BeTrue())
						Expect(err).ToNot(HaveOccurred())
					})
				})
				When("dependent is not in owner namespace", func() {
					It("should be false", func() {
						dependent = createObject(namespaceScoped, types.NamespacedName{Namespace: "ns2", Name: "dependent"})
						supportsOwnerRef, err := SupportsOwnerReference(rm, owner, dependent)
						Expect(supportsOwnerRef).To(BeFalse())
						Expect(err).ToNot(HaveOccurred())
					})
				})
			})
		})
		When("GVK REST mappings are missing", func() {
			var (
				owner     = createObject(clusterScoped, types.NamespacedName{Namespace: "", Name: "owner"})
				dependent = createObject(namespaceScoped, types.NamespacedName{Namespace: "ns1", Name: "dependent"})
			)

			BeforeEach(func() {
				rm = meta.NewDefaultRESTMapper([]schema.GroupVersion{})
			})
			It("fails when owner REST mapping is missing", func() {
				supportsOwnerRef, err := SupportsOwnerReference(rm, owner, dependent)
				Expect(supportsOwnerRef).To(BeFalse())
				Expect(err).To(HaveOccurred())
			})
			It("fails when dependent REST mapping is missing", func() {
				rm.Add(clusterScoped, meta.RESTScopeRoot)
				supportsOwnerRef, err := SupportsOwnerReference(rm, owner, dependent)
				Expect(supportsOwnerRef).To(BeFalse())
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("ContainsFinalizer", func() {
		var (
			obj       metav1.Object
			gvk       = schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Kind"}
			finalizer = "finalizer"
		)
		BeforeEach(func() {
			obj = createObject(gvk, types.NamespacedName{Namespace: "ns1", Name: "myKind"})
		})
		When("object contains finalizer", func() {
			BeforeEach(func() {
				obj.SetFinalizers([]string{finalizer})
			})
			It("should return true", func() {
				Expect(ContainsFinalizer(obj, finalizer)).To(BeTrue())
			})
		})
		When("object contains finalizer", func() {
			It("should return true", func() {
				Expect(ContainsFinalizer(obj, finalizer)).To(BeFalse())
			})
		})
	})
})

func createObject(gvk schema.GroupVersionKind, key types.NamespacedName) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	u.SetName(key.Name)
	u.SetNamespace(key.Namespace)
	return u
}
