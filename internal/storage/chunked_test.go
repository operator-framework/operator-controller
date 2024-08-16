package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	clientcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("chunkedSecrets", func() {
	const chunkSize = 1000
	var (
		secretInterface clientcorev1.SecretInterface
		chunkedDriver   driver.Driver
	)

	BeforeEach(func() {
		secretInterface = clientcorev1.NewForConfigOrDie(cfg).Secrets("default")
		chunkedDriver = NewChunkedSecrets(secretInterface, "test-owner", ChunkedSecretsConfig{
			ChunkSize:      chunkSize,
			MaxReadChunks:  2,
			MaxWriteChunks: 2,
		})
	})

	AfterEach(func() {
		Expect(secretInterface.DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{})).To(Succeed())
	})

	var _ = Describe("Create", func() {
		It("should create a large release with multiple secrets", func() {
			rel := genRelease("test-release", 1, release.StatusPendingInstall, nil, chunkSize*2)
			Expect(chunkedDriver.Create(releaseKey(rel), rel)).To(Succeed())
			verifySecrets(secretInterface, 2)
		})
		It("should create a small release with a single secret", func() {
			rel := genRelease("test-release", 1, release.StatusPendingInstall, nil, chunkSize/2)
			Expect(chunkedDriver.Create(releaseKey(rel), rel)).To(Succeed())
			verifySecrets(secretInterface, 1)
		})
		It("should fail if the release already exists", func() {
			rel := genRelease("test-release", 1, release.StatusPendingInstall, nil, chunkSize/2)
			Expect(chunkedDriver.Create(releaseKey(rel), rel)).To(Succeed())

			// Change the status to produce a release with the same key, but different content.
			rel.Info.Status = release.StatusDeployed
			Expect(chunkedDriver.Create(releaseKey(rel), rel)).To(MatchError(driver.ErrReleaseExists))
		})
		It("should fail if the release is too large", func() {
			rel := genRelease("test-release", 1, release.StatusPendingInstall, nil, chunkSize*4)
			Expect(chunkedDriver.Create(releaseKey(rel), rel)).To(MatchError(ContainSubstring("release too large")))
		})
	})

	var _ = Describe("Get", func() {
		It("should get a large release that is chunked", func() {
			expected := genRelease("test-release", 1, release.StatusPendingInstall, nil, chunkSize*2)
			Expect(chunkedDriver.Create(releaseKey(expected), expected)).To(Succeed())
			actual, err := chunkedDriver.Get(releaseKey(expected))
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(Equal(expected))
		})
		It("should get a small release that is not chunked", func() {
			expected := genRelease("test-release", 1, release.StatusPendingInstall, nil, chunkSize/2)
			Expect(chunkedDriver.Create(releaseKey(expected), expected)).To(Succeed())
			actual, err := chunkedDriver.Get(releaseKey(expected))
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(Equal(expected))
		})
		It("should fail if the release does not exist", func() {
			rel := genRelease("test-release", 1, release.StatusPendingInstall, nil, chunkSize/2)
			_, err := chunkedDriver.Get(releaseKey(rel))
			Expect(err).To(MatchError(driver.ErrReleaseNotFound))
		})
		It("should fail if the release is too large", func() {
			rel := genRelease("test-release", 1, release.StatusPendingInstall, nil, chunkSize*2)
			Expect(chunkedDriver.Create(releaseKey(rel), rel)).To(Succeed())

			maxReadDriver := NewChunkedSecrets(secretInterface, "test-owner", ChunkedSecretsConfig{
				ChunkSize:      chunkSize,
				MaxReadChunks:  1,
				MaxWriteChunks: 2,
			})
			_, err := maxReadDriver.Get(releaseKey(rel))
			Expect(err).To(MatchError(ContainSubstring("release too large")))
		})
	})

	var _ = Describe("Update", func() {
		It("should update a single-secret release to a multi-secret release", func() {
			rel := genRelease("test-release", 1, release.StatusPendingInstall, nil, chunkSize/2)
			Expect(chunkedDriver.Create(releaseKey(rel), rel)).To(Succeed())
			verifySecrets(secretInterface, 1)

			// Change the status to produce a release with the same key, but different content.
			rel = genRelease("test-release", 1, release.StatusDeployed, nil, chunkSize*2)
			Expect(chunkedDriver.Update(releaseKey(rel), rel)).To(Succeed())
			verifySecrets(secretInterface, 2)
		})
		It("should update a multi-secret release to a single-secret release", func() {
			rel := genRelease("test-release", 1, release.StatusPendingInstall, nil, chunkSize*2)
			Expect(chunkedDriver.Create(releaseKey(rel), rel)).To(Succeed())
			verifySecrets(secretInterface, 2)

			// Change the status to produce a release with the same key, but different content.
			rel = genRelease("test-release", 1, release.StatusDeployed, nil, chunkSize/2)
			Expect(chunkedDriver.Update(releaseKey(rel), rel)).To(Succeed())
			verifySecrets(secretInterface, 1)
		})
		It("should fail if the release does not exist", func() {
			rel := genRelease("test-release", 1, release.StatusPendingInstall, nil, chunkSize/2)
			Expect(chunkedDriver.Update(releaseKey(rel), rel)).To(MatchError(driver.ErrReleaseNotFound))
		})

		It("should fail if the release is too large", func() {
			rel := genRelease("test-release", 1, release.StatusPendingInstall, nil, chunkSize*2)
			Expect(chunkedDriver.Create(releaseKey(rel), rel)).To(Succeed())
			verifySecrets(secretInterface, 2)

			// Change the status to produce a release with the same key, but different content.
			rel = genRelease("test-release", 1, release.StatusDeployed, nil, chunkSize*4)
			Expect(chunkedDriver.Update(releaseKey(rel), rel)).To(MatchError(ContainSubstring("release too large")))
		})
	})

	var _ = Describe("Delete", func() {
		It("should delete a multi-secret release", func() {
			expected := genRelease("test-release", 1, release.StatusPendingInstall, nil, chunkSize*2)
			Expect(chunkedDriver.Create(releaseKey(expected), expected)).To(Succeed())
			verifySecrets(secretInterface, 2)

			actual, err := chunkedDriver.Delete(releaseKey(expected))
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(Equal(expected))
			verifySecrets(secretInterface, 0)
		})
		It("should delete a single-secret release", func() {
			expected := genRelease("test-release", 1, release.StatusPendingInstall, nil, chunkSize/2)
			Expect(chunkedDriver.Create(releaseKey(expected), expected)).To(Succeed())
			verifySecrets(secretInterface, 1)

			actual, err := chunkedDriver.Delete(releaseKey(expected))
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(Equal(expected))
			verifySecrets(secretInterface, 0)
		})
		It("should fail if the release does not exist", func() {
			rel := genRelease("test-release", 1, release.StatusPendingInstall, nil, chunkSize/2)
			_, err := chunkedDriver.Delete(releaseKey(rel))
			Expect(err).To(MatchError(driver.ErrReleaseNotFound))
		})
		It("should fail if the release is too large", func() {
			rel := genRelease("test-release", 1, release.StatusPendingInstall, nil, chunkSize*2)
			Expect(chunkedDriver.Create(releaseKey(rel), rel)).To(Succeed())

			maxReadDriver := NewChunkedSecrets(secretInterface, "test-owner", ChunkedSecretsConfig{
				ChunkSize:      chunkSize,
				MaxReadChunks:  1,
				MaxWriteChunks: 2,
			})

			_, err := maxReadDriver.Delete(releaseKey(rel))
			Expect(err).To(MatchError(ContainSubstring("release too large")))
		})
	})

	var _ = Describe("List", func() {
		BeforeEach(func() {
			releases := []*release.Release{
				genRelease("a", 1, release.StatusSuperseded, nil, chunkSize/2),
				genRelease("a", 2, release.StatusSuperseded, nil, chunkSize/2),
				genRelease("a", 3, release.StatusSuperseded, nil, chunkSize*2),
				genRelease("a", 4, release.StatusDeployed, nil, chunkSize*2),

				genRelease("b", 1, release.StatusSuperseded, nil, chunkSize*2),
				genRelease("b", 2, release.StatusSuperseded, nil, chunkSize*2),
				genRelease("b", 3, release.StatusDeployed, nil, chunkSize/2),
			}
			for _, rel := range releases {
				Expect(chunkedDriver.Create(releaseKey(rel), rel)).To(Succeed())
			}
		})
		It("should list releases by name", func() {
			aReleases, err := chunkedDriver.List(func(rel *release.Release) bool {
				return rel.Name == "a"
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(aReleases).To(HaveLen(4))
		})

		It("should list releases by status", func() {
			deployedReleases, err := chunkedDriver.List(func(rel *release.Release) bool {
				return rel.Info.Status == release.StatusDeployed
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(deployedReleases).To(HaveLen(2))
		})

		It("should return an empty list if no releases match", func() {
			cReleases, err := chunkedDriver.List(func(rel *release.Release) bool {
				return rel.Name == "c"
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(cReleases).To(BeEmpty())
		})

		It("should fail if any release is too large", func() {
			maxReadDriver := NewChunkedSecrets(secretInterface, "test-owner", ChunkedSecretsConfig{
				ChunkSize:      chunkSize,
				MaxReadChunks:  1,
				MaxWriteChunks: 2,
			})
			actual, err := maxReadDriver.List(func(rel *release.Release) bool { return true })
			Expect(err).To(MatchError(ContainSubstring("release too large")))
			Expect(actual).To(BeNil())
		})
	})

	var _ = Describe("Query", func() {
		BeforeEach(func() {
			releases := []*release.Release{
				genRelease("a", 1, release.StatusSuperseded, nil, chunkSize/2),
				genRelease("a", 2, release.StatusSuperseded, nil, chunkSize/2),
				genRelease("a", 3, release.StatusSuperseded, nil, chunkSize*2),
				genRelease("a", 4, release.StatusDeployed, nil, chunkSize*2),

				genRelease("b", 1, release.StatusSuperseded, nil, chunkSize*2),
				genRelease("b", 2, release.StatusSuperseded, nil, chunkSize*2),
				genRelease("b", 3, release.StatusDeployed, map[string]string{"key1": "val1"}, chunkSize/2),
			}
			for _, rel := range releases {
				Expect(chunkedDriver.Create(releaseKey(rel), rel)).To(Succeed())
			}
		})

		It("should query releases by custom labels", func() {
			key1Releases, err := chunkedDriver.Query(map[string]string{"key1": "val1"})
			Expect(err).ToNot(HaveOccurred())
			Expect(key1Releases).To(HaveLen(1))
		})

		It("should return ErrReleaseNotFound when there is no match", func() {
			_, err := chunkedDriver.Query(map[string]string{"nonexistentKey": "nonexistentVal"})
			Expect(err).To(MatchError(driver.ErrReleaseNotFound))
		})

		It("should succeed if no matched release is too large", func() {
			maxReadDriver := NewChunkedSecrets(secretInterface, "test-owner", ChunkedSecretsConfig{
				ChunkSize:      chunkSize,
				MaxReadChunks:  1,
				MaxWriteChunks: 2,
			})
			actual, err := maxReadDriver.Query(map[string]string{"name": "a", "version": "1"})
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(HaveLen(1))
		})

		It("should fail if any matched release is too large", func() {
			maxReadDriver := NewChunkedSecrets(secretInterface, "test-owner", ChunkedSecretsConfig{
				ChunkSize:      chunkSize,
				MaxReadChunks:  1,
				MaxWriteChunks: 2,
			})
			actual, err := maxReadDriver.Query(map[string]string{"name": "a"})
			Expect(err).To(MatchError(ContainSubstring("release too large")))
			Expect(actual).To(BeNil())
		})

		// This test is necessary because the helm storage library hardcodes the owner to "helm".
		// We have no way to configure the release storage implementation to use a different owner
		// when we set it up as part of the action.Configuration.
		It("should translate owner=helm to owner=test-owner", func() {
			allReleases, err := chunkedDriver.Query(map[string]string{"owner": "helm"})
			Expect(err).ToNot(HaveOccurred())
			Expect(allReleases).To(HaveLen(7))
		})
	})
})

func verifySecrets(secretInterface clientcorev1.SecretInterface, expected int) {
	GinkgoHelper()

	items, err := secretInterface.List(context.Background(), metav1.ListOptions{})
	Expect(err).ToNot(HaveOccurred())
	Expect(items.Items).To(HaveLen(expected))

	if expected == 0 {
		return
	}
	var indexSecrets, chunkSecrets []corev1.Secret
	for _, s := range items.Items {
		switch s.Type {
		case SecretTypeChunkedIndex:
			indexSecrets = append(indexSecrets, s)
		case SecretTypeChunkedChunk:
			chunkSecrets = append(chunkSecrets, s)
		}
	}
	Expect(indexSecrets).To(HaveLen(1), "expected exactly one index secret")

	Expect(indexSecrets[0].Data).To(HaveKey("extraChunks"))
	Expect(indexSecrets[0].Data).To(HaveKey("chunk"))
	Expect(indexSecrets[0].Immutable).To(Equal(ptr.To(false)))

	var expectedExtraChunkNames []string
	Expect(json.Unmarshal(indexSecrets[0].Data["extraChunks"], &expectedExtraChunkNames)).To(Succeed())

	Expect(chunkSecrets).To(HaveLen(expected-1), "expected multiple chunk secrets")
	actualExtraChunkNames := make([]string, 0, len(chunkSecrets))
	for _, sec := range chunkSecrets {
		actualExtraChunkNames = append(actualExtraChunkNames, sec.Name)
		Expect(sec.Data).To(HaveKey("chunk"))
		Expect(sec.Immutable).To(Equal(ptr.To(true)))
	}

	Expect(actualExtraChunkNames).To(ConsistOf(expectedExtraChunkNames))
}

func releaseKey(rel *release.Release) string {
	return fmt.Sprintf("%s.v%d", rel.Name, rel.Version)
}

func genRelease(name string, version int, status release.Status, extraLabels map[string]string, minSize int) *release.Release {
	lbls := map[string]string{
		"globalKey": "globalValue",
	}
	maps.Copy(lbls, extraLabels)
	return &release.Release{
		Name:    name,
		Version: version,
		Config: map[string]interface{}{
			"takingUpSpace": rand.String(minSize),
		},
		Info:   &release.Info{Status: status},
		Labels: lbls,
	}
}
