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
	"errors"
	"io"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/postrender"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/releaseutil"
	"helm.sh/helm/v3/pkg/storage/driver"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/yaml"

	"github.com/operator-framework/operator-controller/internal/helm/client/testutil"
)

const mockTestDesc = "Test Description"

var _ = Describe("ActionClient", func() {
	var (
		rm meta.RESTMapper
	)
	BeforeEach(func() {
		var err error

		httpClient, err := rest.HTTPClientFor(cfg)
		Expect(err).NotTo(HaveOccurred())

		rm, err = apiutil.NewDynamicRESTMapper(cfg, httpClient)
		Expect(err).ToNot(HaveOccurred())
	})
	var _ = Describe("NewActionClientGetter", func() {
		It("should return a valid ActionConfigGetter", func() {
			actionConfigGetter, err := NewActionConfigGetter(cfg, rm)
			Expect(err).ShouldNot(HaveOccurred())
			acg, err := NewActionClientGetter(actionConfigGetter)
			Expect(err).ToNot(HaveOccurred())
			Expect(acg).NotTo(BeNil())
		})

		When("options are specified", func() {
			expectErr := errors.New("expect this error")

			var (
				actionConfigGetter ActionConfigGetter
				cli                kube.Interface
				obj                client.Object
			)
			BeforeEach(func() {
				var err error
				actionConfigGetter, err = NewActionConfigGetter(cfg, rm)
				Expect(err).ShouldNot(HaveOccurred())
				dc, err := discovery.NewDiscoveryClientForConfig(cfg)
				Expect(err).ShouldNot(HaveOccurred())
				cdc := memory.NewMemCacheClient(dc)
				cli = kube.New(newRESTClientGetter(cfg, rm, cdc, ""))
				Expect(err).ShouldNot(HaveOccurred())
				obj = testutil.BuildTestCR(gvk)
			})

			It("should get clients with custom get options", func() {
				expectVersion := rand.Int()
				acg, err := NewActionClientGetter(actionConfigGetter, AppendGetOptions(
					func(get *action.Get) error {
						get.Version = expectVersion
						return nil
					},
					func(get *action.Get) error {
						Expect(get.Version).To(Equal(expectVersion))
						return expectErr
					},
				))
				Expect(err).ToNot(HaveOccurred())
				Expect(acg).NotTo(BeNil())

				ac, err := acg.ActionClientFor(context.Background(), obj)
				Expect(err).ToNot(HaveOccurred())
				Expect(ac).NotTo(BeNil())

				_, err = ac.Get(obj.GetName())
				Expect(err).To(MatchError(expectErr))
			})
			It("should get clients with custom install options", func() {
				acg, err := NewActionClientGetter(actionConfigGetter, AppendInstallOptions(
					func(install *action.Install) error {
						install.Description = mockTestDesc
						return nil
					},
					func(install *action.Install) error {
						Expect(install.Description).To(Equal(mockTestDesc))
						return expectErr
					},
				))
				Expect(err).ToNot(HaveOccurred())
				Expect(acg).NotTo(BeNil())

				ac, err := acg.ActionClientFor(context.Background(), obj)
				Expect(err).ToNot(HaveOccurred())
				Expect(ac).NotTo(BeNil())

				_, err = ac.Install(obj.GetName(), obj.GetNamespace(), &chrt, chartutil.Values{})
				Expect(err).To(MatchError(expectErr))
			})
			It("should get clients with custom upgrade options", func() {
				acg, err := NewActionClientGetter(actionConfigGetter, AppendUpgradeOptions(
					func(upgrade *action.Upgrade) error {
						upgrade.Description = mockTestDesc
						return nil
					},
					func(upgrade *action.Upgrade) error {
						Expect(upgrade.Description).To(Equal(mockTestDesc))
						return expectErr
					},
				))
				Expect(err).ToNot(HaveOccurred())
				Expect(acg).NotTo(BeNil())

				ac, err := acg.ActionClientFor(context.Background(), obj)
				Expect(err).ToNot(HaveOccurred())
				Expect(ac).NotTo(BeNil())

				_, err = ac.Upgrade(obj.GetName(), obj.GetNamespace(), &chrt, chartutil.Values{})
				Expect(err).To(MatchError(expectErr))
			})
			It("should get clients with custom uninstall options", func() {
				acg, err := NewActionClientGetter(actionConfigGetter, AppendUninstallOptions(
					func(uninstall *action.Uninstall) error {
						uninstall.Description = mockTestDesc
						return nil
					},
					func(uninstall *action.Uninstall) error {
						Expect(uninstall.Description).To(Equal(mockTestDesc))
						return expectErr
					},
				))
				Expect(err).ToNot(HaveOccurred())
				Expect(acg).NotTo(BeNil())

				ac, err := acg.ActionClientFor(context.Background(), obj)
				Expect(err).ToNot(HaveOccurred())
				Expect(ac).NotTo(BeNil())

				_, err = ac.Uninstall(obj.GetName())
				Expect(err).To(MatchError(expectErr))
			})
			It("should get clients with custom install failure uninstall options", func() {
				acg, err := NewActionClientGetter(actionConfigGetter, AppendInstallFailureUninstallOptions(
					func(uninstall *action.Uninstall) error {
						uninstall.Description = mockTestDesc
						return nil
					},
					func(uninstall *action.Uninstall) error {
						Expect(uninstall.Description).To(Equal(mockTestDesc))
						return expectErr
					},
				))
				Expect(err).ToNot(HaveOccurred())
				Expect(acg).NotTo(BeNil())

				ac, err := acg.ActionClientFor(context.Background(), obj)
				Expect(err).ToNot(HaveOccurred())
				Expect(ac).NotTo(BeNil())

				_, err = ac.Install(obj.GetName(), obj.GetNamespace(), &chrt, chartutil.Values{}, func(install *action.Install) error {
					// Force the installatiom to fail by using an impossibly short wait.
					// When the installation fails, the failure uninstall logic is attempted.
					install.Wait = true
					install.Timeout = time.Nanosecond * 1
					return nil
				})
				Expect(err).To(MatchError(ContainSubstring(expectErr.Error())))

				// Uninstall the chart to cleanup for other tests.
				_, err = ac.Uninstall(obj.GetName())
				Expect(err).ToNot(HaveOccurred())
			})
			It("should get clients with custom upgrade failure rollback options", func() {
				expectMaxHistory := rand.Int()
				acg, err := NewActionClientGetter(actionConfigGetter, AppendUpgradeFailureRollbackOptions(
					func(rollback *action.Rollback) error {
						rollback.MaxHistory = expectMaxHistory
						return nil
					},
					func(rollback *action.Rollback) error {
						Expect(rollback.MaxHistory).To(Equal(expectMaxHistory))
						return expectErr
					},
				))
				Expect(err).ToNot(HaveOccurred())
				Expect(acg).NotTo(BeNil())

				ac, err := acg.ActionClientFor(context.Background(), obj)
				Expect(err).ToNot(HaveOccurred())
				Expect(ac).NotTo(BeNil())

				// Install the chart so that we can try an upgrade.
				rel, err := ac.Install(obj.GetName(), obj.GetNamespace(), &chrt, chartutil.Values{})
				Expect(err).ToNot(HaveOccurred())
				Expect(rel).NotTo(BeNil())

				_, err = ac.Upgrade(obj.GetName(), obj.GetNamespace(), &chrt, chartutil.Values{}, func(upgrade *action.Upgrade) error {
					// Force the upgrade to fail by using an impossibly short wait.
					// When the upgrade fails, the rollback logic is attempted.
					upgrade.Wait = true
					upgrade.Timeout = time.Nanosecond * 1
					return nil
				})
				Expect(err).To(MatchError(ContainSubstring(expectErr.Error())))

				// Uninstall the chart to cleanup for other tests.
				_, err = ac.Uninstall(obj.GetName())
				Expect(err).ToNot(HaveOccurred())
			})
			It("should get clients with postrenderers", func() {

				acg, err := NewActionClientGetter(actionConfigGetter, AppendPostRenderers(newMockPostRenderer("foo", "bar")))
				Expect(err).ToNot(HaveOccurred())
				Expect(acg).NotTo(BeNil())

				ac, err := acg.ActionClientFor(context.Background(), obj)
				Expect(err).ToNot(HaveOccurred())

				_, err = ac.Install(obj.GetName(), obj.GetNamespace(), &chrt, chartutil.Values{})
				Expect(err).ToNot(HaveOccurred())

				rel, err := ac.Get(obj.GetName())
				Expect(err).ToNot(HaveOccurred())

				rl, err := cli.Build(bytes.NewBufferString(rel.Manifest), false)
				Expect(err).ToNot(HaveOccurred())

				Expect(rl).NotTo(BeEmpty())
				err = rl.Visit(func(info *resource.Info, err error) error {
					Expect(err).ToNot(HaveOccurred())
					Expect(info.Object).NotTo(BeNil())
					objMeta, err := meta.Accessor(info.Object)
					Expect(err).ToNot(HaveOccurred())
					Expect(objMeta.GetAnnotations()).To(HaveKey("foo"))
					Expect(objMeta.GetAnnotations()["foo"]).To(Equal("bar"))
					return nil
				})
				Expect(err).ToNot(HaveOccurred())

				// Uninstall the chart to cleanup for other tests.
				_, err = ac.Uninstall(obj.GetName())
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})

	var _ = Describe("ActionClientGetterFunc", func() {
		It("implements the ActionClientGetter interface", func() {
			gvk := schema.GroupVersionKind{Group: "test", Version: "v1alpha1", Kind: "Test"}
			expectedObj := &unstructured.Unstructured{}
			expectedObj.SetGroupVersionKind(gvk)
			var actualObj client.Object
			f := ActionClientGetterFunc(func(_ context.Context, obj client.Object) (ActionInterface, error) {
				actualObj = obj
				return nil, nil
			})
			_, _ = f.ActionClientFor(context.Background(), expectedObj)
			Expect(actualObj.GetObjectKind().GroupVersionKind()).To(Equal(gvk))
		})
	})

	var _ = Describe("ActionClientFor", func() {
		var obj client.Object
		BeforeEach(func() {
			obj = testutil.BuildTestCR(gvk)
		})
		It("should return a valid ActionClient", func() {
			actionConfGetter, err := NewActionConfigGetter(cfg, rm)
			Expect(err).ShouldNot(HaveOccurred())
			acg, err := NewActionClientGetter(actionConfGetter)
			Expect(err).ToNot(HaveOccurred())
			ac, err := acg.ActionClientFor(context.Background(), obj)
			Expect(err).ToNot(HaveOccurred())
			Expect(ac).NotTo(BeNil())
		})
	})

	var _ = Describe("ActionClient methods", func() {
		var (
			obj             client.Object
			cl              client.Client
			actionCfgGetter ActionConfigGetter
			ac              ActionInterface
			vals            = chartutil.Values{"service": map[string]interface{}{"type": "NodePort"}}
		)
		BeforeEach(func() {
			obj = testutil.BuildTestCR(gvk)

			var err error
			actionCfgGetter, err = NewActionConfigGetter(cfg, rm)
			Expect(err).ShouldNot(HaveOccurred())
			acg, err := NewActionClientGetter(actionCfgGetter)
			Expect(err).ToNot(HaveOccurred())
			ac, err = acg.ActionClientFor(context.Background(), obj)
			Expect(err).ToNot(HaveOccurred())

			cl, err = client.New(cfg, client.Options{})
			Expect(err).ToNot(HaveOccurred())

			Expect(cl.Create(context.TODO(), obj)).To(Succeed())
		})

		AfterEach(func() {
			Expect(cl.Delete(context.TODO(), obj)).To(Succeed())
		})

		When("release is not installed", func() {
			AfterEach(func() {
				if _, err := ac.Get(obj.GetName()); errors.Is(err, driver.ErrReleaseNotFound) {
					return
				}
				_, err := ac.Uninstall(obj.GetName())
				if err != nil {
					panic(err)
				}
			})
			var _ = Describe("Install", func() {
				It("should succeed", func() {
					var (
						rel *release.Release
						err error
					)
					By("installing the release", func() {
						opt := func(i *action.Install) error { i.Description = mockTestDesc; return nil }
						rel, err = ac.Install(obj.GetName(), obj.GetNamespace(), &chrt, vals, opt)
						Expect(err).ToNot(HaveOccurred())
						Expect(rel).NotTo(BeNil())
					})
					verifyRelease(cl, obj, rel)
				})
				It("should uninstall a failed install", func() {
					By("failing to install the release", func() {
						vals := chartutil.Values{"service": map[string]interface{}{"type": "FooBar"}}
						r, err := ac.Install(obj.GetName(), obj.GetNamespace(), &chrt, vals)
						Expect(err).To(HaveOccurred())
						Expect(r).NotTo(BeNil())
					})
					verifyNoRelease(cl, obj.GetNamespace(), obj.GetName(), nil)
				})
				When("failure uninstall is disabled", func() {
					BeforeEach(func() {
						acg, err := NewActionClientGetter(actionCfgGetter, WithFailureRollbacks(false))
						Expect(err).ToNot(HaveOccurred())
						ac, err = acg.ActionClientFor(context.Background(), obj)
						Expect(err).ToNot(HaveOccurred())
					})
					It("should not uninstall a failed install", func() {
						vals := chartutil.Values{"service": map[string]interface{}{"type": "FooBar"}}
						returnedRelease, err := ac.Install(obj.GetName(), obj.GetNamespace(), &chrt, vals)
						Expect(err).To(HaveOccurred())
						Expect(returnedRelease).ToNot(BeNil())
						Expect(returnedRelease.Info.Status).To(Equal(release.StatusFailed))
						latestRelease, err := ac.Get(obj.GetName())
						Expect(err).ToNot(HaveOccurred())
						Expect(latestRelease).ToNot(BeNil())
						Expect(latestRelease.Version).To(Equal(returnedRelease.Version))
					})
				})
				When("using an option function that returns an error", func() {
					It("should fail", func() {
						opt := func(*action.Install) error { return errors.New("expect this error") }
						r, err := ac.Install(obj.GetName(), obj.GetNamespace(), &chrt, vals, opt)
						Expect(err).To(MatchError("expect this error"))
						Expect(r).To(BeNil())
					})
				})
			})
			var _ = Describe("Upgrade", func() {
				It("should fail", func() {
					r, err := ac.Upgrade(obj.GetName(), obj.GetNamespace(), &chrt, vals)
					Expect(err).To(HaveOccurred())
					Expect(r).To(BeNil())
				})
			})
			var _ = Describe("Uninstall", func() {
				It("should fail", func() {
					resp, err := ac.Uninstall(obj.GetName())
					Expect(err).To(HaveOccurred())
					Expect(resp).To(BeNil())
				})
			})
		})

		When("release is installed", func() {
			var (
				installedRelease *release.Release
			)
			BeforeEach(func() {
				var err error
				opt := func(i *action.Install) error { i.Description = mockTestDesc; return nil }
				installedRelease, err = ac.Install(obj.GetName(), obj.GetNamespace(), &chrt, vals, opt)
				Expect(err).ToNot(HaveOccurred())
				Expect(installedRelease).NotTo(BeNil())
			})
			AfterEach(func() {
				if _, err := ac.Get(obj.GetName()); errors.Is(err, driver.ErrReleaseNotFound) {
					return
				}
				_, err := ac.Uninstall(obj.GetName())
				if err != nil {
					panic(err)
				}
			})
			var _ = Describe("Get", func() {
				var (
					rel *release.Release
					err error
				)
				It("should succeed", func() {
					By("getting the release", func() {
						rel, err = ac.Get(obj.GetName())
						Expect(err).ToNot(HaveOccurred())
						Expect(rel).NotTo(BeNil())
					})
					verifyRelease(cl, obj, rel)
				})
				When("using an option function that returns an error", func() {
					It("should fail", func() {
						opt := func(*action.Get) error { return errors.New("expect this error") }
						rel, err = ac.Get(obj.GetName(), opt)
						Expect(err).To(MatchError("expect this error"))
						Expect(rel).To(BeNil())
					})
				})
				When("setting the version option", func() {
					It("should succeed with an existing version", func() {
						opt := func(g *action.Get) error { g.Version = 1; return nil }
						rel, err = ac.Get(obj.GetName(), opt)
						Expect(err).ToNot(HaveOccurred())
						Expect(rel).NotTo(BeNil())
					})
					It("should fail with a non-existent version", func() {
						opt := func(g *action.Get) error { g.Version = 10; return nil }
						rel, err = ac.Get(obj.GetName(), opt)
						Expect(err).To(HaveOccurred())
						Expect(rel).To(BeNil())
					})
				})
			})
			var _ = Describe("Install", func() {
				It("should fail", func() {
					r, err := ac.Install(obj.GetName(), obj.GetNamespace(), &chrt, vals)
					Expect(err).To(HaveOccurred())
					Expect(r).To(BeNil())
				})
			})
			var _ = Describe("Upgrade", func() {
				It("should succeed", func() {
					var (
						rel *release.Release
						err error
					)
					By("upgrading the release", func() {
						opt := func(u *action.Upgrade) error { u.Description = mockTestDesc; return nil }
						rel, err = ac.Upgrade(obj.GetName(), obj.GetNamespace(), &chrt, vals, opt)
						Expect(err).ToNot(HaveOccurred())
						Expect(rel).NotTo(BeNil())
					})
					verifyRelease(cl, obj, rel)
				})
				It("should rollback a failed upgrade", func() {
					By("failing to upgrade the release", func() {
						vals := chartutil.Values{"service": map[string]interface{}{"type": "FooBar"}}
						r, err := ac.Upgrade(obj.GetName(), obj.GetNamespace(), &chrt, vals)
						Expect(err).To(HaveOccurred())
						Expect(r).ToNot(BeNil())
					})
					tmp := *installedRelease
					rollbackRelease := &tmp
					rollbackRelease.Version = installedRelease.Version + 2
					verifyRelease(cl, obj, rollbackRelease)
				})
				When("failure rollback is disabled", func() {
					BeforeEach(func() {
						acg, err := NewActionClientGetter(actionCfgGetter, WithFailureRollbacks(false))
						Expect(err).ToNot(HaveOccurred())
						ac, err = acg.ActionClientFor(context.Background(), obj)
						Expect(err).ToNot(HaveOccurred())
					})
					It("should not rollback a failed upgrade", func() {
						vals := chartutil.Values{"service": map[string]interface{}{"type": "FooBar"}}
						returnedRelease, err := ac.Upgrade(obj.GetName(), obj.GetNamespace(), &chrt, vals)
						Expect(err).To(HaveOccurred())
						Expect(returnedRelease).ToNot(BeNil())
						Expect(returnedRelease.Info.Status).To(Equal(release.StatusFailed))
						latestRelease, err := ac.Get(obj.GetName())
						Expect(err).ToNot(HaveOccurred())
						Expect(latestRelease).ToNot(BeNil())
						Expect(latestRelease.Version).To(Equal(returnedRelease.Version))
					})
				})
				When("using an option function that returns an error", func() {
					It("should fail", func() {
						opt := func(*action.Upgrade) error { return errors.New("expect this error") }
						r, err := ac.Upgrade(obj.GetName(), obj.GetNamespace(), &chrt, vals, opt)
						Expect(err).To(MatchError("expect this error"))
						Expect(r).To(BeNil())
					})
				})
			})
			var _ = Describe("Uninstall", func() {
				It("should succeed", func() {
					var (
						resp *release.UninstallReleaseResponse
						err  error
					)
					By("uninstalling the release", func() {
						opt := func(i *action.Uninstall) error { i.Description = mockTestDesc; return nil }
						resp, err = ac.Uninstall(obj.GetName(), opt)
						Expect(err).ToNot(HaveOccurred())
						Expect(resp).NotTo(BeNil())
					})
					verifyNoRelease(cl, obj.GetNamespace(), obj.GetName(), resp.Release)
				})
				When("using an option function that returns an error", func() {
					It("should fail", func() {
						opt := func(*action.Uninstall) error { return errors.New("expect this error") }
						r, err := ac.Uninstall(obj.GetName(), opt)
						Expect(err).To(MatchError("expect this error"))
						Expect(r).To(BeNil())
					})
				})
			})
			var _ = Describe("Reconcile", func() {
				It("should succeed", func() {
					By("reconciling the release", func() {
						err := ac.Reconcile(installedRelease)
						Expect(err).ToNot(HaveOccurred())
					})
					verifyRelease(cl, obj, installedRelease)
				})
				It("should re-create deleted resources", func() {
					By("deleting the manifest resources", func() {
						objs := manifestToObjects(installedRelease.Manifest)
						for _, obj := range objs {
							err := cl.Delete(context.TODO(), obj)
							Expect(err).ToNot(HaveOccurred())
						}
					})
					By("reconciling the release", func() {
						err := ac.Reconcile(installedRelease)
						Expect(err).ToNot(HaveOccurred())
					})
					verifyRelease(cl, obj, installedRelease)
				})
				It("should patch changed resources", func() {
					By("changing manifest resources", func() {
						objs := manifestToObjects(installedRelease.Manifest)
						for _, obj := range objs {
							key := client.ObjectKeyFromObject(obj)

							u := &unstructured.Unstructured{}
							u.SetGroupVersionKind(obj.GetObjectKind().GroupVersionKind())
							err := cl.Get(context.TODO(), key, u)
							Expect(err).ToNot(HaveOccurred())

							labels := u.GetLabels()
							labels["app.kubernetes.io/managed-by"] = "Unmanaged"
							u.SetLabels(labels)

							err = cl.Update(context.TODO(), u)
							Expect(err).ToNot(HaveOccurred())
						}
					})
					By("reconciling the release", func() {
						err := ac.Reconcile(installedRelease)
						Expect(err).ToNot(HaveOccurred())
					})
					verifyRelease(cl, obj, installedRelease)
				})
			})
		})
	})

	var _ = Describe("createPatch", func() {
		It("ignores extra fields in custom resource types", func() {
			o1 := newTestUnstructured([]interface{}{
				map[string]interface{}{
					"name": "test1",
				},
				map[string]interface{}{
					"name": "test2",
				},
			})
			o2 := &resource.Info{
				Object: newTestUnstructured([]interface{}{
					map[string]interface{}{
						"name": "test1",
					},
				}),
			}
			patch, patchType, err := createPatch(o1, o2)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(patch)).To(Equal(``))
			Expect(patchType).To(Equal(apitypes.JSONPatchType))
		})
		It("patches missing fields in custom resource types", func() {
			o1 := newTestUnstructured([]interface{}{
				map[string]interface{}{
					"name": "test1",
				},
			})
			o2 := &resource.Info{
				Object: newTestUnstructured([]interface{}{
					map[string]interface{}{
						"name": "test1",
					},
					map[string]interface{}{
						"name": "test2",
					},
				}),
			}
			patch, patchType, err := createPatch(o1, o2)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(patch)).To(Equal(`[{"op":"add","path":"/spec/template/spec/containers/1","value":{"name":"test2"}}]`))
			Expect(patchType).To(Equal(apitypes.JSONPatchType))
		})
		It("ignores nil fields in custom resource types", func() {
			o1 := newTestUnstructured([]interface{}{
				map[string]interface{}{
					"name": "test1",
				},
			})
			o2 := &resource.Info{
				Object: newTestUnstructured([]interface{}{
					map[string]interface{}{
						"name": "test1",
						"test": nil,
					},
				}),
			}
			patch, patchType, err := createPatch(o1, o2)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(patch)).To(Equal(``))
			Expect(patchType).To(Equal(apitypes.JSONPatchType))
		})
		It("replaces incorrect fields in custom resource types", func() {
			o1 := newTestUnstructured([]interface{}{
				map[string]interface{}{
					"name": "test1",
				},
			})
			o2 := &resource.Info{
				Object: newTestUnstructured([]interface{}{
					map[string]interface{}{
						"name": "test2",
					},
				}),
			}
			patch, patchType, err := createPatch(o1, o2)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(patch)).To(Equal(`[{"op":"replace","path":"/spec/template/spec/containers/0/name","value":"test2"}]`))
			Expect(patchType).To(Equal(apitypes.JSONPatchType))
		})
		It("ignores extra fields in core types", func() {
			o1 := newTestDeployment([]corev1.Container{
				{Name: "test1"},
				{Name: "test2"},
			})
			o2 := &resource.Info{
				Object: newTestDeployment([]corev1.Container{
					{Name: "test1"},
				}),
			}
			patch, patchType, err := createPatch(o1, o2)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(patch)).To(Equal(`{"spec":{"template":{"spec":{"$setElementOrder/containers":[{"name":"test1"}]}}}}`))
			Expect(patchType).To(Equal(apitypes.StrategicMergePatchType))
		})
		It("patches missing fields in core types", func() {
			o1 := newTestDeployment([]corev1.Container{
				{Name: "test1"},
			})
			o2 := &resource.Info{
				Object: newTestDeployment([]corev1.Container{
					{Name: "test1"},
					{Name: "test2"},
				}),
			}
			patch, patchType, err := createPatch(o1, o2)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(patch)).To(Equal(`{"spec":{"template":{"spec":{"$setElementOrder/containers":[{"name":"test1"},{"name":"test2"}],"containers":[{"name":"test2","resources":{}}]}}}}`))
			Expect(patchType).To(Equal(apitypes.StrategicMergePatchType))
		})
		It("ignores nil fields in core types", func() {
			o1 := newTestDeployment([]corev1.Container{
				{Name: "test1"},
			})
			o2 := &resource.Info{
				Object: newTestDeployment([]corev1.Container{
					{Name: "test1", LivenessProbe: nil},
				}),
			}
			patch, patchType, err := createPatch(o1, o2)
			Expect(err).ToNot(HaveOccurred())
			Expect(patch).To(BeNil())
			Expect(patchType).To(Equal(apitypes.StrategicMergePatchType))
		})
		It("replaces incorrect fields in core types", func() {
			o1 := newTestDeployment([]corev1.Container{
				{Name: "test1"},
			})
			o2 := &resource.Info{
				Object: newTestDeployment([]corev1.Container{
					{Name: "test2"},
				}),
			}
			patch, patchType, err := createPatch(o1, o2)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(patch)).To(Equal(`{"spec":{"template":{"spec":{"$setElementOrder/containers":[{"name":"test2"}],"containers":[{"name":"test2","resources":{}}]}}}}`))
			Expect(patchType).To(Equal(apitypes.StrategicMergePatchType))
		})
		It("does not remove extra annotations in core types", func() {
			o1 := &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "ns",
					Annotations: map[string]string{
						"testannotation": "testvalue",
					},
				},
				Spec: appsv1.DeploymentSpec{},
			}
			o2 := &resource.Info{
				Object: &appsv1.Deployment{
					TypeMeta: metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "ns",
					},
					Spec: appsv1.DeploymentSpec{},
				},
			}
			patch, patchType, err := createPatch(o1, o2)
			Expect(err).ToNot(HaveOccurred())
			Expect(patch).To(BeNil())
			Expect(patchType).To(Equal(apitypes.StrategicMergePatchType))
		})
	})
})

func manifestToObjects(manifest string) []client.Object {
	objs := []client.Object{}
	for _, m := range releaseutil.SplitManifests(manifest) {
		u := &unstructured.Unstructured{}
		err := yaml.Unmarshal([]byte(m), u)
		Expect(err).ToNot(HaveOccurred())
		objs = append(objs, u)
	}
	return objs
}

func verifyRelease(cl client.Client, owner client.Object, rel *release.Release) {
	By("verifying release secret exists at release version", func() {
		releaseSecrets := &corev1.SecretList{}
		err := cl.List(context.TODO(), releaseSecrets, client.InNamespace(owner.GetNamespace()), client.MatchingLabels{"owner": "helm", "name": rel.Name})
		Expect(err).ToNot(HaveOccurred())
		Expect(releaseSecrets.Items).To(HaveLen(rel.Version))
		Expect(releaseSecrets.Items[rel.Version-1].Type).To(Equal(corev1.SecretType("helm.sh/release.v1")))
		Expect(releaseSecrets.Items[rel.Version-1].Labels["version"]).To(Equal(strconv.Itoa(rel.Version)))
		Expect(releaseSecrets.Items[rel.Version-1].Data["release"]).NotTo(BeNil())
	})

	By("verifying release status description option was honored", func() {
		Expect(rel.Info.Description).To(Equal(mockTestDesc))
	})

	By("verifying the release resources exist", func() {
		objs := manifestToObjects(rel.Manifest)
		for _, obj := range objs {
			key := client.ObjectKeyFromObject(obj)
			err := cl.Get(context.TODO(), key, obj)
			Expect(err).ToNot(HaveOccurred())
			Expect(obj.GetOwnerReferences()).To(HaveLen(1))
			Expect(obj.GetOwnerReferences()[0]).To(Equal(
				metav1.OwnerReference{
					APIVersion:         owner.GetObjectKind().GroupVersionKind().GroupVersion().String(),
					Kind:               owner.GetObjectKind().GroupVersionKind().Kind,
					Name:               owner.GetName(),
					UID:                owner.GetUID(),
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				}),
			)
		}
	})
}

func verifyNoRelease(cl client.Client, ns string, name string, rel *release.Release) {
	By("verifying all release secrets are removed", func() {
		releaseSecrets := &corev1.SecretList{}
		err := cl.List(context.TODO(), releaseSecrets, client.InNamespace(ns), client.MatchingLabels{"owner": "helm", "name": name})
		Expect(err).ToNot(HaveOccurred())
		Expect(releaseSecrets.Items).To(BeEmpty())
	})
	By("verifying the uninstall description option was honored", func() {
		if rel != nil {
			Expect(rel.Info.Description).To(Equal(mockTestDesc))
		}
	})
	By("verifying all release resources are removed", func() {
		if rel != nil {
			for _, r := range releaseutil.SplitManifests(rel.Manifest) {
				u := &unstructured.Unstructured{}
				err := yaml.Unmarshal([]byte(r), u)
				Expect(err).ToNot(HaveOccurred())

				key := client.ObjectKeyFromObject(u)
				err = cl.Get(context.TODO(), key, u)
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}
		}
	})
}

func newTestUnstructured(containers []interface{}) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "MyResource",
			"apiVersion": "myApi",
			"metadata": map[string]interface{}{
				"name":      "test",
				"namespace": "ns",
			},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": containers,
					},
				},
			},
		},
	}
}

func newTestDeployment(containers []corev1.Container) *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "ns"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: containers,
				},
			},
		},
	}
}

type mockPostRenderer struct {
	k8sCli kube.Interface
	key    string
	value  string
}

var _ postrender.PostRenderer = &mockPostRenderer{}

func newMockPostRenderer(key, value string) PostRendererProvider {
	return func(rm meta.RESTMapper, kubeClient kube.Interface, obj client.Object) postrender.PostRenderer {
		return &mockPostRenderer{
			k8sCli: kubeClient,
			key:    key,
			value:  value,
		}
	}
}

func (m *mockPostRenderer) Run(renderedManifests *bytes.Buffer) (*bytes.Buffer, error) {
	b, err := io.ReadAll(renderedManifests)
	if err != nil {
		return nil, err
	}
	rl, err := m.k8sCli.Build(bytes.NewBuffer(b), false)
	if err != nil {
		return nil, err
	}
	out := bytes.Buffer{}
	if err := rl.Visit(m.visit(&out)); err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *mockPostRenderer) visit(out *bytes.Buffer) func(r *resource.Info, err error) error {
	return func(r *resource.Info, rErr error) error {
		if rErr != nil {
			return rErr
		}
		objMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(r.Object)
		if err != nil {
			return err
		}
		u := &unstructured.Unstructured{Object: objMap}

		annotations := u.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[m.key] = m.value
		u.SetAnnotations(annotations)

		outData, err := yaml.Marshal(u.Object)
		if err != nil {
			return err
		}
		if _, err := out.WriteString("---\n" + string(outData)); err != nil {
			return err
		}
		return nil
	}
}
