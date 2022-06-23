package e2e

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	plain "github.com/operator-framework/rukpak/internal/provisioner/plain/types"
	"github.com/operator-framework/rukpak/internal/storage"
	"github.com/operator-framework/rukpak/internal/util"
)

const (
	// TODO: make this is a CLI flag?
	defaultSystemNamespace = "rukpak-system"
)

func Logf(f string, v ...interface{}) {
	if !strings.HasSuffix(f, "\n") {
		f += "\n"
	}
	fmt.Fprintf(GinkgoWriter, f, v...)
}

var _ = Describe("plain provisioner bundle", func() {
	When("a valid Bundle references the wrong unique provisioner ID", func() {
		var (
			bundle *rukpakv1alpha1.Bundle
			ctx    context.Context
		)
		BeforeEach(func() {
			ctx = context.Background()

			By("creating the testing Bundle resource")
			bundle = &rukpakv1alpha1.Bundle{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "olm-crds-valid",
				},
				Spec: rukpakv1alpha1.BundleSpec{
					ProvisionerClassName: "non-existent-class-name",
					Source: rukpakv1alpha1.BundleSource{
						Type: rukpakv1alpha1.SourceTypeImage,
						Image: &rukpakv1alpha1.ImageSource{
							Ref: "testdata/bundles/plain-v0:valid",
						},
					},
				},
			}
			err := c.Create(ctx, bundle)
			Expect(err).To(BeNil())
		})
		AfterEach(func() {
			By("deleting the testing Bundle resource")
			err := c.Delete(ctx, bundle)
			Expect(err).To(BeNil())
		})
		It("should consistently contain an empty status", func() {
			Consistently(func() bool {
				if err := c.Get(ctx, client.ObjectKeyFromObject(bundle), bundle); err != nil {
					return false
				}
				return len(bundle.Status.Conditions) == 0
			}, 10*time.Second, 1*time.Second).Should(BeTrue())
		})
	})
	When("a valid Bundle referencing a remote container image is created", func() {
		var (
			bundle *rukpakv1alpha1.Bundle
			ctx    context.Context
		)
		BeforeEach(func() {
			ctx = context.Background()

			By("creating the testing Bundle resource")
			bundle = &rukpakv1alpha1.Bundle{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "olm-crds-valid",
				},
				Spec: rukpakv1alpha1.BundleSpec{
					ProvisionerClassName: plain.ProvisionerID,
					Source: rukpakv1alpha1.BundleSource{
						Type: rukpakv1alpha1.SourceTypeImage,
						Image: &rukpakv1alpha1.ImageSource{
							Ref: "testdata/bundles/plain-v0:valid",
						},
					},
				},
			}
			err := c.Create(ctx, bundle)
			Expect(err).To(BeNil())
		})
		AfterEach(func() {
			By("deleting the testing Bundle resource")
			err := c.Delete(ctx, bundle)
			Expect(err).To(BeNil())
		})

		It("should eventually report a successful state", func() {
			By("eventually reporting an Unpacked phase", func() {
				Eventually(func() (string, error) {
					if err := c.Get(ctx, client.ObjectKeyFromObject(bundle), bundle); err != nil {
						return "", err
					}
					return bundle.Status.Phase, nil
				}).Should(Equal(rukpakv1alpha1.PhaseUnpacked))
			})

			By("eventually writing a non-empty image digest to the status", func() {
				Eventually(func() (*rukpakv1alpha1.BundleSource, error) {
					if err := c.Get(ctx, client.ObjectKeyFromObject(bundle), bundle); err != nil {
						return nil, err
					}
					return bundle.Status.ResolvedSource, nil
				}).Should(And(
					Not(BeNil()),
					WithTransform(func(s *rukpakv1alpha1.BundleSource) rukpakv1alpha1.SourceType { return s.Type }, Equal(rukpakv1alpha1.SourceTypeImage)),
					WithTransform(func(s *rukpakv1alpha1.BundleSource) *rukpakv1alpha1.ImageSource { return s.Image }, And(
						Not(BeNil()),
						WithTransform(func(i *rukpakv1alpha1.ImageSource) string { return i.Ref }, Not(Equal(""))),
					)),
				))
			})
		})

		It("should re-create underlying system resources", func() {
			var (
				pod *corev1.Pod
			)

			By("getting the underlying bundle unpacking pod")
			selector := util.NewBundleLabelSelector(bundle)
			Eventually(func() bool {
				pods := &corev1.PodList{}
				if err := c.List(ctx, pods, &client.ListOptions{
					Namespace:     defaultSystemNamespace,
					LabelSelector: selector,
				}); err != nil {
					return false
				}
				if len(pods.Items) != 1 {
					return false
				}
				pod = &pods.Items[0]
				return true
			}).Should(BeTrue())

			By("storing the pod's original UID")
			originalUID := pod.GetUID()

			By("deleting the underlying pod and waiting for it to be re-created")
			err := c.Delete(context.Background(), pod)
			Expect(err).To(BeNil())

			By("verifying the pod's UID has changed")
			Eventually(func() (types.UID, error) {
				err := c.Get(ctx, client.ObjectKeyFromObject(pod), pod)
				return pod.GetUID(), err
			}).ShouldNot(Equal(originalUID))
		})
		It("should block spec.source updates", func() {
			Consistently(func() error {
				return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
					if err := c.Get(ctx, client.ObjectKeyFromObject(bundle), bundle); err != nil {
						return err
					}
					bundle.Spec.Source.Image.Ref = "foobar"
					return c.Update(ctx, bundle)
				})
			}, 3*time.Second, 250*time.Millisecond).Should(MatchError(ContainSubstring("bundle.spec is immutable")))
		})
		It("should block spec.provisionerClassName updates", func() {
			Consistently(func() error {
				return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
					if err := c.Get(ctx, client.ObjectKeyFromObject(bundle), bundle); err != nil {
						return err
					}
					bundle.Spec.ProvisionerClassName = "foobar"
					return c.Update(ctx, bundle)
				})
			}, 3*time.Second, 250*time.Millisecond).Should(MatchError(ContainSubstring("bundle.spec is immutable")))
		})
	})

	When("an invalid Bundle referencing a remote container image is created", func() {
		var (
			bundle *rukpakv1alpha1.Bundle
			ctx    context.Context
		)
		BeforeEach(func() {
			ctx = context.Background()

			By("creating the testing Bundle resource")
			bundle = &rukpakv1alpha1.Bundle{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "olm-crds-invalid",
				},
				Spec: rukpakv1alpha1.BundleSpec{
					ProvisionerClassName: plain.ProvisionerID,
					Source: rukpakv1alpha1.BundleSource{
						Type: rukpakv1alpha1.SourceTypeImage,
						Image: &rukpakv1alpha1.ImageSource{
							Ref: "testdata/bundles/plain-v0:non-existent-tag",
						},
					},
				},
			}
			err := c.Create(ctx, bundle)
			Expect(err).To(BeNil())
		})
		AfterEach(func() {
			By("deleting the testing Bundle resource")
			err := c.Delete(ctx, bundle)
			Expect(err).To(BeNil())
		})

		It("checks the bundle's phase is stuck in pending", func() {
			By("waiting until the pod is reporting ImagePullBackOff state")
			Eventually(func() bool {
				pod := &corev1.Pod{}
				if err := c.Get(ctx, types.NamespacedName{
					Name:      util.PodName("plain", bundle.GetName()),
					Namespace: defaultSystemNamespace,
				}, pod); err != nil {
					return false
				}
				if pod.Status.Phase != corev1.PodPending {
					return false
				}
				for _, status := range pod.Status.ContainerStatuses {
					if status.State.Waiting != nil && status.State.Waiting.Reason == "ImagePullBackOff" {
						return true
					}
				}
				return false
			}).Should(BeTrue())

			By("waiting for the bundle to report back that state")
			Eventually(func() bool {
				err := c.Get(ctx, client.ObjectKeyFromObject(bundle), bundle)
				if err != nil {
					return false
				}
				if bundle.Status.Phase != rukpakv1alpha1.PhasePending {
					return false
				}
				unpackPending := meta.FindStatusCondition(bundle.Status.Conditions, rukpakv1alpha1.PhaseUnpacked)
				if unpackPending == nil {
					return false
				}
				if unpackPending.Message != fmt.Sprintf(`Back-off pulling image "%s"`, bundle.Spec.Source.Image.Ref) {
					return false
				}
				return true
			}).Should(BeTrue())
		})
	})

	When("a bundle containing no manifests is created", func() {
		var (
			bundle *rukpakv1alpha1.Bundle
			ctx    context.Context
		)
		BeforeEach(func() {
			ctx = context.Background()

			By("creating the testing Bundle resource")
			bundle = &rukpakv1alpha1.Bundle{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "olm-crds-unsupported",
				},
				Spec: rukpakv1alpha1.BundleSpec{
					ProvisionerClassName: plain.ProvisionerID,
					Source: rukpakv1alpha1.BundleSource{
						Type: rukpakv1alpha1.SourceTypeImage,
						Image: &rukpakv1alpha1.ImageSource{
							Ref: "testdata/bundles/plain-v0:empty",
						},
					},
				},
			}
			err := c.Create(ctx, bundle)
			Expect(err).To(BeNil())
		})
		AfterEach(func() {
			By("deleting the testing Bundle resource")
			err := c.Delete(ctx, bundle)
			Expect(err).To(BeNil())
		})
		It("reports an unpack error when the manifests directory is missing", func() {
			By("waiting for the bundle to report back that state")
			Eventually(func() (*metav1.Condition, error) {
				err := c.Get(ctx, client.ObjectKeyFromObject(bundle), bundle)
				if err != nil {
					return nil, err
				}
				return meta.FindStatusCondition(bundle.Status.Conditions, rukpakv1alpha1.TypeUnpacked), nil
			}).Should(And(
				Not(BeNil()),
				WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(rukpakv1alpha1.TypeUnpacked)),
				WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionFalse)),
				WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(rukpakv1alpha1.ReasonUnpackFailed)),
				WithTransform(func(c *metav1.Condition) string { return c.Message }, ContainSubstring(`readdir manifests: file does not exist`)),
			))
		})
	})

	When("a bundle containing an empty manifests directory is created", func() {
		var (
			bundle *rukpakv1alpha1.Bundle
			ctx    context.Context
		)
		BeforeEach(func() {
			ctx = context.Background()

			By("creating the testing Bundle resource")
			bundle = &rukpakv1alpha1.Bundle{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "olm-crds-unsupported",
				},
				Spec: rukpakv1alpha1.BundleSpec{
					ProvisionerClassName: plain.ProvisionerID,
					Source: rukpakv1alpha1.BundleSource{
						Type: rukpakv1alpha1.SourceTypeImage,
						Image: &rukpakv1alpha1.ImageSource{
							Ref: "testdata/bundles/plain-v0:no-manifests",
						},
					},
				},
			}
			err := c.Create(ctx, bundle)
			Expect(err).To(BeNil())
		})
		AfterEach(func() {
			By("deleting the testing Bundle resource")
			err := c.Delete(ctx, bundle)
			Expect(err).To(BeNil())
		})
		It("reports an unpack error when the manifests directory contains no objects", func() {
			By("waiting for the bundle to report back that state")
			Eventually(func() (*metav1.Condition, error) {
				err := c.Get(ctx, client.ObjectKeyFromObject(bundle), bundle)
				if err != nil {
					return nil, err
				}
				return meta.FindStatusCondition(bundle.Status.Conditions, rukpakv1alpha1.TypeUnpacked), nil
			}).Should(And(
				Not(BeNil()),
				WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(rukpakv1alpha1.TypeUnpacked)),
				WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionFalse)),
				WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(rukpakv1alpha1.ReasonUnpackFailed)),
				WithTransform(func(c *metav1.Condition) string { return c.Message }, ContainSubstring(`found zero objects: plain+v0 bundles are required to contain at least one object`)),
			))
		})
	})

	When("Bundles are backed by a git repository", func() {
		var (
			ctx context.Context
		)

		BeforeEach(func() {
			ctx = context.Background()
		})

		When("the bundle is backed by a git commit", func() {
			var (
				bundle *rukpakv1alpha1.Bundle
			)
			BeforeEach(func() {
				bundle = &rukpakv1alpha1.Bundle{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "combo-git-commit",
					},
					Spec: rukpakv1alpha1.BundleSpec{
						ProvisionerClassName: plain.ProvisionerID,
						Source: rukpakv1alpha1.BundleSource{
							Type: rukpakv1alpha1.SourceTypeGit,
							Git: &rukpakv1alpha1.GitSource{
								Repository: "https://github.com/exdx/combo-bundle",
								Ref: rukpakv1alpha1.GitRef{
									Commit: "9e3ab7f1a36302ef512294d5c9f2e9b9566b811e",
								},
							},
						},
					},
				}
				err := c.Create(ctx, bundle)
				Expect(err).To(BeNil())
			})

			AfterEach(func() {
				err := c.Delete(ctx, bundle)
				Expect(err).To(BeNil())
			})

			It("Can create and unpack the bundle successfully", func() {
				Eventually(func() error {
					if err := c.Get(ctx, client.ObjectKeyFromObject(bundle), bundle); err != nil {
						return err
					}
					if bundle.Status.Phase != rukpakv1alpha1.PhaseUnpacked {
						return errors.New("bundle is not unpacked")
					}

					provisionerPods := &corev1.PodList{}
					if err := c.List(context.Background(), provisionerPods, client.MatchingLabels{"app": "plain-provisioner"}); err != nil {
						return err
					}
					if len(provisionerPods.Items) != 1 {
						return errors.New("expected exactly 1 provisioner pod")
					}

					return checkProvisionerBundle(bundle, provisionerPods.Items[0].Name)
				}).Should(BeNil())
			})
		})

		When("the bundle is backed by a git tag", func() {
			var (
				bundle *rukpakv1alpha1.Bundle
			)
			BeforeEach(func() {
				bundle = &rukpakv1alpha1.Bundle{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "combo-git-tag",
					},
					Spec: rukpakv1alpha1.BundleSpec{
						ProvisionerClassName: plain.ProvisionerID,
						Source: rukpakv1alpha1.BundleSource{
							Type: rukpakv1alpha1.SourceTypeGit,
							Git: &rukpakv1alpha1.GitSource{
								Repository: "https://github.com/exdx/combo-bundle",
								Ref: rukpakv1alpha1.GitRef{
									Tag: "v0.0.1",
								},
							},
						},
					},
				}
				err := c.Create(ctx, bundle)
				Expect(err).To(BeNil())
			})

			AfterEach(func() {
				err := c.Delete(ctx, bundle)
				Expect(err).To(BeNil())
			})

			It("Can create and unpack the bundle successfully", func() {
				Eventually(func() error {
					if err := c.Get(ctx, client.ObjectKeyFromObject(bundle), bundle); err != nil {
						return err
					}
					if bundle.Status.Phase != rukpakv1alpha1.PhaseUnpacked {
						return errors.New("bundle is not unpacked")
					}

					provisionerPods := &corev1.PodList{}
					if err := c.List(context.Background(), provisionerPods, client.MatchingLabels{"app": "plain-provisioner"}); err != nil {
						return err
					}
					if len(provisionerPods.Items) != 1 {
						return errors.New("expected exactly 1 provisioner pod")
					}

					return checkProvisionerBundle(bundle, provisionerPods.Items[0].Name)
				}).Should(BeNil())
			})
		})

		When("the bundle is backed by a git branch", func() {
			var (
				bundle *rukpakv1alpha1.Bundle
			)
			BeforeEach(func() {
				bundle = &rukpakv1alpha1.Bundle{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "combo-git-branch",
					},
					Spec: rukpakv1alpha1.BundleSpec{
						ProvisionerClassName: plain.ProvisionerID,
						Source: rukpakv1alpha1.BundleSource{
							Type: rukpakv1alpha1.SourceTypeGit,
							Git: &rukpakv1alpha1.GitSource{
								Repository: "https://github.com/exdx/combo-bundle.git",
								Ref: rukpakv1alpha1.GitRef{
									Branch: "main",
								},
							},
						},
					},
				}
				err := c.Create(ctx, bundle)
				Expect(err).To(BeNil())
			})

			AfterEach(func() {
				err := c.Delete(ctx, bundle)
				Expect(err).To(BeNil())
			})

			It("Can create and unpack the bundle successfully", func() {
				Eventually(func() error {
					if err := c.Get(ctx, client.ObjectKeyFromObject(bundle), bundle); err != nil {
						return err
					}
					if bundle.Status.Phase != rukpakv1alpha1.PhaseUnpacked {
						return errors.New("bundle is not unpacked")
					}

					provisionerPods := &corev1.PodList{}
					if err := c.List(context.Background(), provisionerPods, client.MatchingLabels{"app": "plain-provisioner"}); err != nil {
						return err
					}
					if len(provisionerPods.Items) != 1 {
						return errors.New("expected exactly 1 provisioner pod")
					}

					return checkProvisionerBundle(bundle, provisionerPods.Items[0].Name)
				}).Should(BeNil())
			})
		})

		When("the bundle has a custom manifests directory", func() {
			var (
				bundle *rukpakv1alpha1.Bundle
			)
			BeforeEach(func() {
				bundle = &rukpakv1alpha1.Bundle{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "combo-git-custom-dir",
					},
					Spec: rukpakv1alpha1.BundleSpec{
						ProvisionerClassName: plain.ProvisionerID,
						Source: rukpakv1alpha1.BundleSource{
							Type: rukpakv1alpha1.SourceTypeGit,
							Git: &rukpakv1alpha1.GitSource{
								Repository: "https://github.com/exdx/combo-bundle",
								Directory:  "./dev/deploy",
								Ref: rukpakv1alpha1.GitRef{
									Branch: "main",
								},
							},
						},
					},
				}
				err := c.Create(ctx, bundle)
				Expect(err).To(BeNil())
			})

			AfterEach(func() {
				err := c.Delete(ctx, bundle)
				Expect(err).To(BeNil())
			})

			It("Can create and unpack the bundle successfully", func() {
				Eventually(func() error {
					if err := c.Get(ctx, client.ObjectKeyFromObject(bundle), bundle); err != nil {
						return err
					}
					if bundle.Status.Phase != rukpakv1alpha1.PhaseUnpacked {
						return errors.New("bundle is not unpacked")
					}

					provisionerPods := &corev1.PodList{}
					if err := c.List(context.Background(), provisionerPods, client.MatchingLabels{"app": "plain-provisioner"}); err != nil {
						return err
					}
					if len(provisionerPods.Items) != 1 {
						return errors.New("expected exactly 1 provisioner pod")
					}

					return checkProvisionerBundle(bundle, provisionerPods.Items[0].Name)
				}).Should(BeNil())
			})
		})
	})

	When("a bundle containing nested directory is created", func() {
		var (
			bundle *rukpakv1alpha1.Bundle
			ctx    context.Context
		)
		const (
			manifestsDir = "manifests"
			subdirName   = "emptydir"
		)
		BeforeEach(func() {
			ctx = context.Background()

			By("creating the testing Bundle resource")
			bundle = &rukpakv1alpha1.Bundle{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "namespace-subdirs",
				},
				Spec: rukpakv1alpha1.BundleSpec{
					ProvisionerClassName: plain.ProvisionerID,
					Source: rukpakv1alpha1.BundleSource{
						Type: rukpakv1alpha1.SourceTypeImage,
						Image: &rukpakv1alpha1.ImageSource{
							Ref: "testdata/bundles/plain-v0:subdir",
						},
					},
				},
			}
			err := c.Create(ctx, bundle)
			Expect(err).To(BeNil())
		})
		AfterEach(func() {
			By("deleting the testing Bundle resource")
			err := c.Delete(ctx, bundle)
			Expect(err).To(BeNil())
		})
		It("reports an unpack error when the manifests directory contains directories", func() {
			By("eventually reporting an Unpacked phase", func() {
				Eventually(func() (*metav1.Condition, error) {
					if err := c.Get(ctx, client.ObjectKeyFromObject(bundle), bundle); err != nil {
						return nil, err
					}
					return meta.FindStatusCondition(bundle.Status.Conditions, rukpakv1alpha1.TypeUnpacked), nil
				}).Should(And(
					Not(BeNil()),
					WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(rukpakv1alpha1.TypeUnpacked)),
					WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionFalse)),
					WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(rukpakv1alpha1.ReasonUnpackFailed)),
					WithTransform(func(c *metav1.Condition) string { return c.Message },
						ContainSubstring(fmt.Sprintf("subdirectories are not allowed within the %q directory of the bundle image filesystem: found %q", manifestsDir, filepath.Join(manifestsDir, subdirName)))),
				))
			})
		})
	})
})

var _ = Describe("plain provisioner bundleinstance", func() {
	Context("embedded bundle template", func() {
		var (
			bi  *rukpakv1alpha1.BundleInstance
			ctx context.Context
		)
		BeforeEach(func() {
			ctx = context.Background()

			bi = &rukpakv1alpha1.BundleInstance{
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
			}
			err := c.Create(ctx, bi)
			Expect(err).To(BeNil())

			By("waiting until the BI reports a successful installation")
			Eventually(func() (*metav1.Condition, error) {
				if err := c.Get(ctx, client.ObjectKeyFromObject(bi), bi); err != nil {
					return nil, err
				}
				if bi.Status.InstalledBundleName == "" {
					return nil, fmt.Errorf("waiting for bundle name to be populated")
				}
				return meta.FindStatusCondition(bi.Status.Conditions, rukpakv1alpha1.TypeInstalled), nil
			}).Should(And(
				Not(BeNil()),
				WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(rukpakv1alpha1.TypeInstalled)),
				WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionTrue)),
				WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(rukpakv1alpha1.ReasonInstallationSucceeded)),
				WithTransform(func(c *metav1.Condition) string { return c.Message }, ContainSubstring("instantiated bundle")),
			))
		})
		AfterEach(func() {
			By("deleting the testing BI resource")
			Expect(c.Delete(ctx, bi)).To(BeNil())
		})
		It("should generate a Bundle that contains an owner reference", func() {
			// Note: cannot use bi.GroupVersionKind() as the Kind/APIVersion fields
			// will be empty during the testing suite.
			biRef := metav1.NewControllerRef(bi, rukpakv1alpha1.BundleInstanceGVK)

			Eventually(func() []metav1.OwnerReference {
				if err := c.Get(ctx, client.ObjectKeyFromObject(bi), bi); err != nil {
					return nil
				}
				b := &rukpakv1alpha1.Bundle{}
				if err := c.Get(ctx, types.NamespacedName{Name: bi.Status.InstalledBundleName}, b); err != nil {
					return nil
				}
				return b.GetOwnerReferences()
			}).Should(And(
				Not(BeNil()),
				ContainElement(*biRef)),
			)
		})
		It("should generate a Bundle that contains the correct labels", func() {
			Eventually(func() (map[string]string, error) {
				if err := c.Get(ctx, client.ObjectKeyFromObject(bi), bi); err != nil {
					return nil, err
				}
				b := &rukpakv1alpha1.Bundle{}
				if err := c.Get(ctx, types.NamespacedName{Name: bi.Status.InstalledBundleName}, b); err != nil {
					return nil, err
				}
				return b.Labels, nil
			}).Should(And(
				Not(BeNil()),
				WithTransform(func(s map[string]string) string { return s[util.CoreOwnerKindKey] }, Equal(rukpakv1alpha1.BundleInstanceKind)),
				WithTransform(func(s map[string]string) string { return s[util.CoreOwnerNameKey] }, Equal(bi.GetName())),
			))
		})
		Describe("template is unsuccessfully updated", func() {
			var (
				originalBundle *rukpakv1alpha1.Bundle
			)
			BeforeEach(func() {
				originalBundle = &rukpakv1alpha1.Bundle{}

				Eventually(func() error {
					if err := c.Get(ctx, client.ObjectKeyFromObject(bi), bi); err != nil {
						return err
					}
					if err := c.Get(ctx, types.NamespacedName{Name: bi.Status.InstalledBundleName}, originalBundle); err != nil {
						return err
					}
					bi.Spec.Template.Spec = rukpakv1alpha1.BundleSpec{
						ProvisionerClassName: plain.ProvisionerID,
						Source: rukpakv1alpha1.BundleSource{
							Type: rukpakv1alpha1.SourceTypeGit,
							Git: &rukpakv1alpha1.GitSource{
								Repository: "github.com/operator-framework/combo",
								Ref: rukpakv1alpha1.GitRef{
									Tag: "non-existent-tag",
								},
							},
						},
					}
					return c.Update(ctx, bi)
				}).Should(Succeed())
			})
			It("should generate a new Bundle resource that matches the desired specification", func() {
				Eventually(func() bool {
					if err := c.Get(ctx, client.ObjectKeyFromObject(bi), bi); err != nil {
						return false
					}
					existingBundles, err := util.GetBundlesForBundleInstanceSelector(ctx, c, bi)
					if err != nil {
						return false
					}
					if len(existingBundles.Items) != 2 {
						return false
					}
					util.SortBundlesByCreation(existingBundles)
					// Note: existing bundles are sorted by metadata.CreationTimestamp, so select
					// the Bundle that was generated second to compare to the desired Bundle template.
					return util.CheckDesiredBundleTemplate(&existingBundles.Items[1], bi.Spec.Template)
				}).Should(BeTrue())
			})

			It("should delete the old Bundle once the newly generated Bundle reports a successful installation state", func() {
				By("waiting until the BI reports a successful installation")
				Eventually(func() (*metav1.Condition, error) {
					if err := c.Get(ctx, client.ObjectKeyFromObject(bi), bi); err != nil {
						return nil, err
					}
					if bi.Status.InstalledBundleName == "" {
						return nil, fmt.Errorf("waiting for bundle name to be populated")
					}
					return meta.FindStatusCondition(bi.Status.Conditions, rukpakv1alpha1.TypeInstalled), nil
				}).Should(And(
					Not(BeNil()),
					WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(rukpakv1alpha1.TypeInstalled)),
					WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionTrue)),
					WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(rukpakv1alpha1.ReasonInstallationSucceeded)),
					WithTransform(func(c *metav1.Condition) string { return c.Message }, ContainSubstring("instantiated bundle")),
				))

				By("verifying that the BI reports an invalid desired Bundle")
				Eventually(func() (*metav1.Condition, error) {
					if err := c.Get(ctx, client.ObjectKeyFromObject(bi), bi); err != nil {
						return nil, err
					}
					return meta.FindStatusCondition(bi.Status.Conditions, rukpakv1alpha1.TypeHasValidBundle), nil
				}).Should(And(
					Not(BeNil()),
					WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(rukpakv1alpha1.TypeHasValidBundle)),
					WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionFalse)),
					WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(rukpakv1alpha1.ReasonUnpackFailed)),
					WithTransform(func(c *metav1.Condition) string { return c.Message }, ContainSubstring(`Failed to unpack`)),
				))

				By("verifying that the old Bundle still exists")
				Consistently(func() error {
					return c.Get(ctx, client.ObjectKeyFromObject(originalBundle), &rukpakv1alpha1.Bundle{})
				}, 15*time.Second, 250*time.Millisecond).Should(Succeed())
			})
		})
		Describe("template is successfully updated", func() {
			var (
				originalBundle *rukpakv1alpha1.Bundle
			)
			BeforeEach(func() {
				originalBundle = &rukpakv1alpha1.Bundle{}

				Eventually(func() error {
					if err := c.Get(ctx, client.ObjectKeyFromObject(bi), bi); err != nil {
						return err
					}
					if err := c.Get(ctx, types.NamespacedName{Name: bi.Status.InstalledBundleName}, originalBundle); err != nil {
						return err
					}
					if len(bi.Spec.Template.Labels) == 0 {
						bi.Spec.Template.Labels = make(map[string]string)
					}
					bi.Spec.Template.Labels["e2e-test"] = "stub"
					return c.Update(ctx, bi)
				}).Should(Succeed())
			})
			It("should generate a new Bundle resource that matches the desired specification", func() {
				Eventually(func() bool {
					if err := c.Get(ctx, client.ObjectKeyFromObject(bi), bi); err != nil {
						return false
					}
					currBundle := &rukpakv1alpha1.Bundle{}
					if err := c.Get(ctx, types.NamespacedName{Name: bi.Status.InstalledBundleName}, currBundle); err != nil {
						return false
					}
					return util.CheckDesiredBundleTemplate(currBundle, bi.Spec.Template)
				}).Should(BeTrue())
			})
			It("should delete the old Bundle once the newly generated Bundle reports a successful installation state", func() {
				By("waiting until the BI reports a successful installation")
				Eventually(func() (*metav1.Condition, error) {
					if err := c.Get(ctx, client.ObjectKeyFromObject(bi), bi); err != nil {
						return nil, err
					}
					if bi.Status.InstalledBundleName == "" {
						return nil, fmt.Errorf("waiting for bundle name to be populated")
					}
					return meta.FindStatusCondition(bi.Status.Conditions, rukpakv1alpha1.TypeInstalled), nil
				}).Should(And(
					Not(BeNil()),
					WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(rukpakv1alpha1.TypeInstalled)),
					WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionTrue)),
					WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(rukpakv1alpha1.ReasonInstallationSucceeded)),
					WithTransform(func(c *metav1.Condition) string { return c.Message }, ContainSubstring("instantiated bundle")),
				))

				By("verifying that the old Bundle no longer exists")
				Eventually(func() error {
					return c.Get(ctx, client.ObjectKeyFromObject(originalBundle), &rukpakv1alpha1.Bundle{})
				}).Should(WithTransform(apierrors.IsNotFound, BeTrue()))
			})
		})
	})

	When("a BundleInstance targets a valid Bundle", func() {
		var (
			bi  *rukpakv1alpha1.BundleInstance
			ctx context.Context
		)
		BeforeEach(func() {
			ctx = context.Background()

			bi = &rukpakv1alpha1.BundleInstance{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "olm-crds",
				},
				Spec: rukpakv1alpha1.BundleInstanceSpec{
					ProvisionerClassName: plain.ProvisionerID,
					Template: &rukpakv1alpha1.BundleTemplate{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name": "olm-crds",
							},
						},
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
			}
			err := c.Create(ctx, bi)
			Expect(err).To(BeNil())
		})
		AfterEach(func() {
			By("deleting the testing BI resource")
			Expect(c.Delete(ctx, bi)).To(BeNil())
		})

		It("should rollout the bundle contents successfully", func() {
			By("eventually writing a successful installation state back to the bundleinstance status")
			Eventually(func() (*metav1.Condition, error) {
				if err := c.Get(ctx, client.ObjectKeyFromObject(bi), bi); err != nil {
					return nil, err
				}
				if bi.Status.InstalledBundleName == "" {
					return nil, fmt.Errorf("waiting for bundle name to be populated")
				}
				return meta.FindStatusCondition(bi.Status.Conditions, rukpakv1alpha1.TypeInstalled), nil
			}).Should(And(
				Not(BeNil()),
				WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(rukpakv1alpha1.TypeInstalled)),
				WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionTrue)),
				WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(rukpakv1alpha1.ReasonInstallationSucceeded)),
				WithTransform(func(c *metav1.Condition) string { return c.Message }, ContainSubstring("instantiated bundle")),
			))
		})
	})

	When("a BundleInstance targets an invalid Bundle", func() {
		var (
			bi  *rukpakv1alpha1.BundleInstance
			ctx context.Context
		)
		BeforeEach(func() {
			ctx = context.Background()
			bi = &rukpakv1alpha1.BundleInstance{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "olm-apis",
				},
				Spec: rukpakv1alpha1.BundleInstanceSpec{
					ProvisionerClassName: plain.ProvisionerID,
					Template: &rukpakv1alpha1.BundleTemplate{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name": "olm-apis",
							},
						},
						Spec: rukpakv1alpha1.BundleSpec{
							ProvisionerClassName: plain.ProvisionerID,
							Source: rukpakv1alpha1.BundleSource{
								Type: rukpakv1alpha1.SourceTypeImage,
								Image: &rukpakv1alpha1.ImageSource{
									Ref: "testdata/bundles/plain-v0:invalid-missing-crds",
								},
							},
						},
					},
				},
			}
			err := c.Create(ctx, bi)
			Expect(err).To(BeNil())
		})
		AfterEach(func() {
			By("deleting the testing BundleInstance resource")
			Eventually(func() error {
				return client.IgnoreNotFound(c.Delete(ctx, bi))
			}).Should(Succeed())
		})

		It("should project a failed installation state", func() {
			Eventually(func() (*metav1.Condition, error) {
				if err := c.Get(ctx, client.ObjectKeyFromObject(bi), bi); err != nil {
					return nil, err
				}
				if bi.Status.InstalledBundleName != "" {
					return nil, fmt.Errorf("bi.Status.InstalledBundleName is non-empty (%q)", bi.Status.InstalledBundleName)
				}
				return meta.FindStatusCondition(bi.Status.Conditions, rukpakv1alpha1.TypeInstalled), nil
			}).Should(And(
				Not(BeNil()),
				WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(rukpakv1alpha1.TypeInstalled)),
				WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionFalse)),
				WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(rukpakv1alpha1.ReasonInstallFailed)),
				WithTransform(func(c *metav1.Condition) string { return c.Message }, And(
					// TODO(tflannag): Add a custom error type for API-based Bundle installations that
					// are missing the requisite CRDs to be able to deploy the unpacked Bundle successfully.
					ContainSubstring(`no matches for kind "CatalogSource" in version "operators.coreos.com/v1alpha1"`),
					ContainSubstring(`no matches for kind "ClusterServiceVersion" in version "operators.coreos.com/v1alpha1"`),
					ContainSubstring(`no matches for kind "OLMConfig" in version "operators.coreos.com/v1"`),
					ContainSubstring(`no matches for kind "OperatorGroup" in version "operators.coreos.com/v1"`),
				)),
			))
		})
	})

	When("a BundleInstance is dependent on another BundleInstance", func() {
		var (
			ctx         context.Context
			dependentBI *rukpakv1alpha1.BundleInstance
		)
		BeforeEach(func() {
			ctx = context.Background()
			By("creating the testing dependent BundleInstance resource")
			dependentBI = &rukpakv1alpha1.BundleInstance{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "e2e-bi-dependent-",
				},
				Spec: rukpakv1alpha1.BundleInstanceSpec{
					ProvisionerClassName: plain.ProvisionerID,
					Template: &rukpakv1alpha1.BundleTemplate{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name": "e2e-dependent-bundle",
							},
						},
						Spec: rukpakv1alpha1.BundleSpec{
							ProvisionerClassName: plain.ProvisionerID,
							Source: rukpakv1alpha1.BundleSource{
								Type: rukpakv1alpha1.SourceTypeImage,
								Image: &rukpakv1alpha1.ImageSource{
									Ref: "testdata/bundles/plain-v0:dependent",
								},
							},
						},
					},
				},
			}
			err := c.Create(ctx, dependentBI)
			Expect(err).To(BeNil())
		})
		AfterEach(func() {
			By("deleting the testing dependent BundleInstance resource")
			Expect(client.IgnoreNotFound(c.Delete(ctx, dependentBI))).To(BeNil())

		})
		When("the providing BundleInstance does not exist", func() {
			It("should eventually project a failed installation for the dependent BundleInstance", func() {
				Eventually(func() (*metav1.Condition, error) {
					if err := c.Get(ctx, client.ObjectKeyFromObject(dependentBI), dependentBI); err != nil {
						return nil, err
					}
					return meta.FindStatusCondition(dependentBI.Status.Conditions, rukpakv1alpha1.TypeInstalled), nil
				}).Should(And(
					Not(BeNil()),
					WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(rukpakv1alpha1.TypeInstalled)),
					WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionFalse)),
					WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(rukpakv1alpha1.ReasonInstallFailed)),
					WithTransform(func(c *metav1.Condition) string { return c.Message },
						ContainSubstring(`required resource not found`)),
				))
			})
		})
		When("the providing BundleInstance is created", func() {
			var (
				providesBI *rukpakv1alpha1.BundleInstance
			)
			BeforeEach(func() {
				ctx = context.Background()

				By("creating the testing providing BI resource")
				providesBI = &rukpakv1alpha1.BundleInstance{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "e2e-bi-providing-",
					},
					Spec: rukpakv1alpha1.BundleInstanceSpec{
						ProvisionerClassName: plain.ProvisionerID,
						Template: &rukpakv1alpha1.BundleTemplate{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app.kubernetes.io/name": "e2e-bundle-providing",
								},
							},
							Spec: rukpakv1alpha1.BundleSpec{
								ProvisionerClassName: plain.ProvisionerID,
								Source: rukpakv1alpha1.BundleSource{
									Type: rukpakv1alpha1.SourceTypeImage,
									Image: &rukpakv1alpha1.ImageSource{
										Ref: "testdata/bundles/plain-v0:provides",
									},
								},
							},
						},
					},
				}
				err := c.Create(ctx, providesBI)
				Expect(err).To(BeNil())
			})
			AfterEach(func() {
				By("deleting the testing providing BundleInstance resource")
				Expect(client.IgnoreNotFound(c.Delete(ctx, providesBI))).To(BeNil())

			})
			It("should eventually project a successful installation for the dependent BundleInstance", func() {
				Eventually(func() (*metav1.Condition, error) {
					if err := c.Get(ctx, client.ObjectKeyFromObject(dependentBI), dependentBI); err != nil {
						return nil, err
					}
					if dependentBI.Status.InstalledBundleName == "" {
						return nil, fmt.Errorf("waiting for bundle name to be populated")
					}
					return meta.FindStatusCondition(dependentBI.Status.Conditions, rukpakv1alpha1.TypeInstalled), nil
				}).Should(And(
					Not(BeNil()),
					WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(rukpakv1alpha1.TypeInstalled)),
					WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionTrue)),
					WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(rukpakv1alpha1.ReasonInstallationSucceeded)),
					WithTransform(func(c *metav1.Condition) string { return c.Message }, ContainSubstring("instantiated bundle")),
				))
			})
		})
	})

	When("a BundleInstance targets a Bundle that contains CRDs and instances of those CRDs", func() {
		var (
			bi  *rukpakv1alpha1.BundleInstance
			ctx context.Context
		)
		BeforeEach(func() {
			ctx = context.Background()

			By("creating the testing BI resource")
			bi = &rukpakv1alpha1.BundleInstance{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "e2e-bi-crds-and-crs-",
				},
				Spec: rukpakv1alpha1.BundleInstanceSpec{
					ProvisionerClassName: plain.ProvisionerID,
					Template: &rukpakv1alpha1.BundleTemplate{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name": "e2e-bundle-crds-and-crs",
							},
						},
						Spec: rukpakv1alpha1.BundleSpec{
							ProvisionerClassName: plain.ProvisionerID,
							Source: rukpakv1alpha1.BundleSource{
								Type: rukpakv1alpha1.SourceTypeImage,
								Image: &rukpakv1alpha1.ImageSource{
									Ref: "testdata/bundles/plain-v0:invalid-crds-and-crs",
								},
							},
						},
					},
				},
			}
			err := c.Create(ctx, bi)
			Expect(err).To(BeNil())
		})
		AfterEach(func() {
			By("deleting the testing BI resource")
			Expect(c.Delete(ctx, bi)).To(BeNil())
		})
		It("eventually reports a failed installation state due to missing APIs on the cluster", func() {
			Eventually(func() (*metav1.Condition, error) {
				if err := c.Get(ctx, client.ObjectKeyFromObject(bi), bi); err != nil {
					return nil, err
				}
				return meta.FindStatusCondition(bi.Status.Conditions, rukpakv1alpha1.TypeInstalled), nil
			}).Should(And(
				Not(BeNil()),
				WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(rukpakv1alpha1.TypeInstalled)),
				WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionFalse)),
				WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(rukpakv1alpha1.ReasonInstallFailed)),
				WithTransform(func(c *metav1.Condition) string { return c.Message }, ContainSubstring(`no matches for kind "CatalogSource" in version "operators.coreos.com/v1alpha1"`)),
			))
		})
	})
})

var _ = Describe("plain provisioner garbage collection", func() {
	When("a Bundle has been deleted", func() {
		var (
			ctx context.Context
			b   *rukpakv1alpha1.Bundle
		)
		BeforeEach(func() {
			ctx = context.Background()

			By("creating the testing Bundle resource")
			b = &rukpakv1alpha1.Bundle{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "e2e-ownerref-bundle-valid",
				},
				Spec: rukpakv1alpha1.BundleSpec{
					ProvisionerClassName: plain.ProvisionerID,
					Source: rukpakv1alpha1.BundleSource{
						Type: rukpakv1alpha1.SourceTypeImage,
						Image: &rukpakv1alpha1.ImageSource{
							Ref: "testdata/bundles/plain-v0:valid",
						},
					},
				},
			}
			Expect(c.Create(ctx, b)).To(BeNil())

			By("eventually reporting an Unpacked phase")
			Eventually(func() (string, error) {
				if err := c.Get(ctx, client.ObjectKeyFromObject(b), b); err != nil {
					return "", err
				}
				return b.Status.Phase, nil
			}).Should(Equal(rukpakv1alpha1.PhaseUnpacked))
		})
		AfterEach(func() {
			By("deleting the testing Bundle resource")
			Expect(c.Get(ctx, client.ObjectKeyFromObject(b), &rukpakv1alpha1.Bundle{})).To(WithTransform(apierrors.IsNotFound, BeTrue()))
		})
		It("should result in the underlying bundle unpack pod being deleted", func() {
			By("deleting the test Bundle resource")
			Expect(c.Delete(ctx, b)).To(BeNil())

			By("waiting until the unpack pods for this bundle have been deleted")
			selector := util.NewBundleLabelSelector(b)
			Eventually(func() bool {
				pods := &corev1.PodList{}
				if err := c.List(ctx, pods, &client.ListOptions{
					Namespace:     defaultSystemNamespace,
					LabelSelector: selector,
				}); err != nil {
					return false
				}
				return len(pods.Items) == 0
			}).Should(BeTrue())
		})
		It("should result in the underlying bundle file being deleted", func() {
			provisionerPods := &corev1.PodList{}
			err := c.List(context.Background(), provisionerPods, client.MatchingLabels{"app": "plain-provisioner"})
			Expect(err).To(BeNil())
			Expect(provisionerPods.Items).To(HaveLen(1))

			By("checking that the bundle file exists")
			Expect(checkProvisionerBundle(b, provisionerPods.Items[0].Name)).To(Succeed())

			By("deleting the test Bundle resource")
			Expect(c.Delete(ctx, b)).To(BeNil())

			By("waiting until the bundle file has been deleted")
			Eventually(func() error {
				return checkProvisionerBundle(b, provisionerPods.Items[0].Name)
			}).Should(MatchError(ContainSubstring("command terminated with exit code 1")))
		})
	})

	When("an embedded Bundle has been deleted", func() {
		var (
			ctx context.Context
			bi  *rukpakv1alpha1.BundleInstance
		)
		BeforeEach(func() {
			ctx = context.Background()
			labels := map[string]string{
				"e2e": "ownerref-bundle-valid",
			}

			By("creating the testing Bundle resource")
			bi = &rukpakv1alpha1.BundleInstance{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "e2e-ownerref-bi-valid",
				},
				Spec: rukpakv1alpha1.BundleInstanceSpec{
					ProvisionerClassName: plain.ProvisionerID,
					Template: &rukpakv1alpha1.BundleTemplate{
						ObjectMeta: metav1.ObjectMeta{
							Labels: labels,
						},
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
			}
			Expect(c.Create(ctx, bi)).To(BeNil())

			By("eventually reporting a successful installation")
			Eventually(func() (*metav1.Condition, error) {
				if err := c.Get(ctx, client.ObjectKeyFromObject(bi), bi); err != nil {
					return nil, err
				}
				if bi.Status.InstalledBundleName == "" {
					return nil, fmt.Errorf("waiting for a populated installed bundle name")
				}
				return meta.FindStatusCondition(bi.Status.Conditions, rukpakv1alpha1.TypeInstalled), nil
			}).Should(And(
				Not(BeNil()),
				WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(rukpakv1alpha1.TypeInstalled)),
				WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionTrue)),
				WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(rukpakv1alpha1.ReasonInstallationSucceeded)),
				WithTransform(func(c *metav1.Condition) string { return c.Message }, ContainSubstring("instantiated bundle")),
			))
		})
		AfterEach(func() {
			By("deleting the testing BI resource")
			Expect(c.Delete(ctx, bi)).To(BeNil())
		})
		It("should result in a new Bundle being generated", func() {
			var (
				originalUUID types.UID
			)
			By("deleting the test Bundle resource")
			Eventually(func() error {
				if err := c.Get(ctx, client.ObjectKeyFromObject(bi), bi); err != nil {
					return err
				}
				originalBundleName := bi.Status.InstalledBundleName
				b := &rukpakv1alpha1.Bundle{}
				if err := c.Get(ctx, types.NamespacedName{Name: originalBundleName}, b); err != nil {
					return err
				}
				originalUUID = b.ObjectMeta.UID
				return c.Delete(ctx, b)
			}).Should(Succeed())

			By("waiting until a new Bundle gets generated")
			Eventually(func() bool {
				if err := c.Get(ctx, client.ObjectKeyFromObject(bi), bi); err != nil {
					return false
				}

				installedBundleName := bi.Status.InstalledBundleName
				if installedBundleName == "" {
					return false
				}

				b := &rukpakv1alpha1.Bundle{}
				if err := c.Get(ctx, types.NamespacedName{Name: installedBundleName}, b); err != nil {
					return false
				}
				return b.UID != originalUUID
			}).Should(BeTrue())
		})
	})

	When("a BundleInstance has been deleted", func() {
		var (
			ctx context.Context
			bi  *rukpakv1alpha1.BundleInstance
		)
		BeforeEach(func() {
			ctx = context.Background()

			By("creating the testing BI resource")
			bi = &rukpakv1alpha1.BundleInstance{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "e2e-ownerref-bi-valid-",
				},
				Spec: rukpakv1alpha1.BundleInstanceSpec{
					ProvisionerClassName: plain.ProvisionerID,
					Template: &rukpakv1alpha1.BundleTemplate{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name": "e2e-ownerref-bundle-valid",
							},
						},
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
			}
			Expect(c.Create(ctx, bi)).To(BeNil())

			By("waiting for the BI to eventually report a successful install status")
			Eventually(func() (*metav1.Condition, error) {
				if err := c.Get(ctx, client.ObjectKeyFromObject(bi), bi); err != nil {
					return nil, err
				}
				return meta.FindStatusCondition(bi.Status.Conditions, rukpakv1alpha1.TypeInstalled), nil
			}).Should(And(
				Not(BeNil()),
				WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(rukpakv1alpha1.TypeInstalled)),
				WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionTrue)),
				WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(rukpakv1alpha1.ReasonInstallationSucceeded)),
				WithTransform(func(c *metav1.Condition) string { return c.Message }, ContainSubstring("instantiated bundle")),
			))
		})
		AfterEach(func() {
			By("deleting the testing BI resource")
			Expect(c.Get(ctx, client.ObjectKeyFromObject(bi), &rukpakv1alpha1.BundleInstance{})).To(WithTransform(apierrors.IsNotFound, BeTrue()))
		})
		It("should eventually result in the installed CRDs being deleted", func() {
			By("deleting the testing BI resource")
			Expect(c.Delete(ctx, bi)).To(BeNil())

			By("waiting until all the installed CRDs have been deleted")
			selector := util.NewBundleInstanceLabelSelector(bi)
			Eventually(func() bool {
				crds := &apiextensionsv1.CustomResourceDefinitionList{}
				if err := c.List(ctx, crds, &client.ListOptions{
					LabelSelector: selector,
				}); err != nil {
					return false
				}
				return len(crds.Items) == 0
			}).Should(BeTrue())
		})
	})
})

func checkProvisionerBundle(object client.Object, provisionerPodName string) error {
	req := kubeClient.CoreV1().RESTClient().Post().
		Namespace(defaultSystemNamespace).
		Resource("pods").
		Name(provisionerPodName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: "manager",
			Command:   []string{"ls", filepath.Join(storage.DefaultBundleCacheDir, fmt.Sprintf("%s.tgz", object.GetName()))},
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, runtime.NewParameterCodec(c.Scheme()))

	exec, err := remotecommand.NewSPDYExecutor(cfg, http.MethodPost, req.URL())
	if err != nil {
		return err
	}

	return exec.Stream(remotecommand.StreamOptions{
		Stdin:  os.Stdin,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Tty:    false,
	})
}
