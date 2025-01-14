package storage

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"hash"
	"hash/fnv"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/pkg/errors"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	clientcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/utils/ptr"
)

var _ driver.Driver = (*chunkedSecrets)(nil)

type ChunkedSecretsConfig struct {
	ChunkSize      int
	MaxReadChunks  int
	MaxWriteChunks int
	Log            func(string, ...interface{})
}

func NewChunkedSecrets(client clientcorev1.SecretInterface, owner string, config ChunkedSecretsConfig) driver.Driver {
	if config.Log == nil {
		config.Log = func(string, ...interface{}) {}
	}

	return &chunkedSecrets{
		client:               client,
		owner:                owner,
		ChunkedSecretsConfig: config,

		hashEncoding: base32.NewEncoding("abcdefghijklmnopqrstuvwxyz123456").WithPadding(base32.NoPadding),
		hash:         fnv.New64a(),
	}
}

type chunkedSecrets struct {
	client clientcorev1.SecretInterface
	owner  string
	ChunkedSecretsConfig

	hashMu       sync.Mutex
	hash         hash.Hash64
	hashEncoding *base32.Encoding
}

func (c *chunkedSecrets) Create(key string, rls *release.Release) error {
	c.Log("create: %q", key)
	defer c.Log("created: %q", key)

	chunks, err := c.encodeReleaseAsChunks(key, rls)
	if err != nil {
		return fmt.Errorf("create: failed to encode release %q: %w", rls.Name, err)
	}

	createdAt := time.Now()
	indexSecret := c.indexSecretFromChunks(key, rls, chunks)
	indexSecret.Labels["createdAt"] = strconv.Itoa(int(createdAt.Unix()))
	indexSecret, err = c.client.Create(context.Background(), indexSecret, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return driver.ErrReleaseExists
		}
		return fmt.Errorf("create: failed to create index and chunk %d of %d secret %q: %w", 1, len(chunks), key, err)
	}

	for i, ch := range chunks[1:] {
		chunkSecret := c.chunkSecretFromChunk(indexSecret, ch)
		chunkSecret.Labels["createdAt"] = strconv.Itoa(int(createdAt.Unix()))
		if _, err := c.client.Create(context.Background(), chunkSecret, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create: failed to create chunk secret %d of %d %q: %w", i+2, len(chunks), ch.name, err)
		}
	}
	return nil
}

type chunk struct {
	name string
	data []byte
}

type releaseWrapper struct {
	release.Release
	Labels map[string]string `json:"labels"`
}

func wrapRelease(rls *release.Release) *releaseWrapper {
	return &releaseWrapper{
		Release: *rls,
		Labels:  rls.Labels,
	}
}

// encodeRelease encodes a release returning a base64 encoded
// gzipped string representation, or error.
func (c *chunkedSecrets) encodeReleaseAsChunks(key string, rls *release.Release) ([]chunk, error) {
	buf := &bytes.Buffer{}

	if err := func() error {
		gzw, err := gzip.NewWriterLevel(buf, gzip.BestCompression)
		if err != nil {
			return err
		}
		defer gzw.Close()
		return json.NewEncoder(gzw).Encode(wrapRelease(rls))
	}(); err != nil {
		return nil, err
	}
	data := buf.Bytes()

	// Split the encoded release into chunks of chunkSize
	// and return the chunks.
	var chunks []chunk
	for i := 0; i < len(data); i += c.ChunkSize {
		end := i + c.ChunkSize
		if end > len(data) {
			end = len(data)
		}
		chunks = append(chunks, chunk{
			name: fmt.Sprintf("%s-%s", key, c.hashForData(data[i:end])),
			data: data[i:end],
		})
	}

	if c.MaxWriteChunks > 0 && len(chunks) > c.MaxWriteChunks {
		return nil, fmt.Errorf("release too large: %q requires %d chunks, which exceeds the maximum of %d", rls.Name, len(chunks), c.MaxWriteChunks)
	}

	return chunks, nil
}

const (
	SecretTypeChunkedIndex = corev1.SecretType("operatorframework.io/index.v1")
	SecretTypeChunkedChunk = corev1.SecretType("operatorframework.io/chunk.v1")
)

func (c *chunkedSecrets) indexSecretFromChunks(key string, rls *release.Release, chunks []chunk) *corev1.Secret {
	extraChunkNames := make([]string, 0, len(chunks)-1)
	for _, ch := range chunks[1:] {
		extraChunkNames = append(extraChunkNames, ch.name)
	}
	extraChunkNamesData, err := json.Marshal(extraChunkNames)
	if err != nil {
		panic(err)
	}

	indexLabels := newIndexLabels(c.owner, key, rls)
	indexSecret := &corev1.Secret{
		Type: SecretTypeChunkedIndex,
		ObjectMeta: metav1.ObjectMeta{
			Name:   key,
			Labels: indexLabels,
		},
		Immutable: ptr.To(false),
		Data: map[string][]byte{
			"extraChunks": extraChunkNamesData,
			"chunk":       chunks[0].data,
		},
	}
	return indexSecret
}

func (c *chunkedSecrets) chunkSecretFromChunk(indexSecret *corev1.Secret, ch chunk) *corev1.Secret {
	chunkLabels := newChunkLabels(c.owner, indexSecret.Name)
	chunkSecret := &corev1.Secret{
		Type: SecretTypeChunkedChunk,
		ObjectMeta: metav1.ObjectMeta{
			Name:   ch.name,
			Labels: chunkLabels,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         corev1.SchemeGroupVersion.String(),
					Kind:               "Secret",
					Name:               indexSecret.Name,
					UID:                indexSecret.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(false),
				},
			},
		},
		Immutable: ptr.To(true),
		Data: map[string][]byte{
			"chunk": ch.data,
		},
	}
	return chunkSecret
}

func (c *chunkedSecrets) getIndex(ctx context.Context, key string) (*corev1.Secret, error) {
	indexSecret, err := c.client.Get(ctx, key, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, driver.ErrReleaseNotFound
		}
		return nil, fmt.Errorf("failed to get secret for key %q: %w", key, err)
	}
	return indexSecret, nil
}

func (c *chunkedSecrets) Update(key string, rls *release.Release) error {
	c.Log("update: %q", key)
	defer c.Log("updated: %q", key)

	// Get the existing index secret to make sure it exists
	existingIndex, err := c.getIndex(context.Background(), key)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}

	// Delete the existing chunk secrets
	if err := c.client.DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: newListChunksForKeySelector(c.owner, existingIndex.Name).String()}); err != nil {
		return fmt.Errorf("update: failed to delete previous chunk secrets for key %q: %w", key, err)
	}

	// Generate new chunks
	chunks, err := c.encodeReleaseAsChunks(key, rls)
	if err != nil {
		return fmt.Errorf("create: failed to encode release %q: %w", rls.Name, err)
	}

	modifiedAt := time.Now()

	// Update the index secret
	updatedIndexSecret := c.indexSecretFromChunks(key, rls, chunks)
	updatedIndexSecret.Labels["createdAt"] = existingIndex.Labels["createdAt"]
	updatedIndexSecret.Labels["modifiedAt"] = strconv.Itoa(int(modifiedAt.Unix()))
	updatedIndexSecret, err = c.client.Update(context.Background(), updatedIndexSecret, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("create: failed to create index and chunk %d of %d secret %q: %w", 1, len(chunks), key, err)
	}

	// Create the new chunks
	for i, ch := range chunks[1:] {
		chunkSecret := c.chunkSecretFromChunk(updatedIndexSecret, ch)
		if _, err := c.client.Create(context.Background(), chunkSecret, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create: failed to create chunk secret %d of %d %q: %w", i+2, len(chunks), ch.name, err)
		}
	}
	return nil
}

func (c *chunkedSecrets) Delete(key string) (*release.Release, error) {
	c.Log("delete: %q", key)
	defer c.Log("deleted: %q", key)

	indexSecret, rls, err := c.getIndexAndRelease(key)
	if err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			return nil, driver.ErrReleaseNotFound
		}
		return nil, fmt.Errorf("delete: %w", err)
	}
	if err := c.client.DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: newListAllForKeySelector(c.owner, key).String()}); err != nil {
		return nil, fmt.Errorf("delete: failed to delete index secret %q: %w", indexSecret.Name, err)
	}
	return rls, nil
}

func (c *chunkedSecrets) getIndexAndRelease(key string) (*corev1.Secret, *release.Release, error) {
	indexSecret, err := c.getIndex(context.Background(), key)
	if err != nil {
		return nil, nil, err
	}

	rls, err := c.decodeRelease(context.Background(), indexSecret)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode release from index secret %q: %w", indexSecret.Name, err)
	}
	return indexSecret, rls, nil
}

func (c *chunkedSecrets) Get(key string) (*release.Release, error) {
	c.Log("get: %q", key)
	defer c.Log("got: %q", key)

	_, rls, err := c.getIndexAndRelease(key)
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}
	return rls, nil
}

func (c *chunkedSecrets) List(filter func(*release.Release) bool) ([]*release.Release, error) {
	c.Log("list")
	defer c.Log("listed")

	indexSecrets, err := c.client.List(context.Background(), metav1.ListOptions{LabelSelector: newListIndicesLabelSelector(c.owner).String()})
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}

	var results []*release.Release
	for _, indexSecret := range indexSecrets.Items {
		indexSecret := indexSecret
		rls, err := c.decodeRelease(context.Background(), &indexSecret)
		if err != nil {
			return nil, fmt.Errorf("list: failed to decode release for key %q: %w", indexSecret.Labels["key"], err)
		}
		rls.Labels = indexSecret.Labels
		if filter(rls) {
			results = append(results, rls)
		}
	}
	return results, nil
}

func (c *chunkedSecrets) Query(queryLabels map[string]string) ([]*release.Release, error) {
	for k, v := range queryLabels {
		if k == "owner" && v == "helm" {
			// Helm hardcodes some queries with owner=helm. We'll translate this
			// to use our owner value
			queryLabels[k] = c.owner
		}
	}
	c.Log("query: labels=%v", queryLabels)
	defer c.Log("queried: labels=%v", queryLabels)

	// The only labels that get stored on the index secret are system labels, so we'll do a two-pass
	// query. First, we'll request index secrets from the API server that match the query labels that
	// are system labels. From there, we decode the releases that match, and then further filter those
	// based on the rest of the query labels that are not system labels.
	serverSelectorSet := labels.Set{}
	clientSelectorSet := labels.Set{}
	for k, v := range queryLabels {
		if isSystemLabel(k) {
			serverSelectorSet[k] = v
		} else {
			clientSelectorSet[k] = v
		}
	}

	// Pass 1: build the server selector and query for index secrets
	serverSelector := newListIndicesLabelSelector(c.owner)
	if queryRequirements, selectable := serverSelectorSet.AsSelector().Requirements(); selectable {
		serverSelector = serverSelector.Add(queryRequirements...)
	}

	indexSecrets, err := c.client.List(context.Background(), metav1.ListOptions{LabelSelector: serverSelector.String()})
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	// Pass 2: decode the releases that matched the server selector and filter based on the client selector
	results := make([]*release.Release, 0, len(indexSecrets.Items))
	clientSelector := clientSelectorSet.AsSelector()
	for _, indexSecret := range indexSecrets.Items {
		indexSecret := indexSecret
		rls, err := c.decodeRelease(context.Background(), &indexSecret)
		if err != nil {
			return nil, fmt.Errorf("query: failed to decode release: %w", err)
		}

		if !clientSelector.Matches(labels.Set(rls.Labels)) {
			continue
		}
		results = append(results, rls)
	}

	if len(results) == 0 {
		return nil, driver.ErrReleaseNotFound
	}

	return results, nil
}

func (c *chunkedSecrets) Name() string {
	return fmt.Sprintf("%s/chunkedSecrets", c.owner)
}

func (c *chunkedSecrets) decodeRelease(ctx context.Context, indexSecret *corev1.Secret) (*release.Release, error) {
	extraChunkNamesData, ok := indexSecret.Data["extraChunks"]
	if !ok {
		return nil, fmt.Errorf("index secret %q missing chunks data: %#v", indexSecret.Name, indexSecret)
	}

	var extraChunkNames []string
	if err := json.Unmarshal(extraChunkNamesData, &extraChunkNames); err != nil {
		return nil, fmt.Errorf("failed to parse chunk names from index: %w", err)
	}

	if c.MaxReadChunks > 0 && 1+len(extraChunkNames) > c.MaxReadChunks {
		return nil, fmt.Errorf("release too large: %q consists of %d chunks, which exceeds the maximum of %d", indexSecret.Name, 1+len(extraChunkNames), c.MaxReadChunks)
	}

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		firstChunkData, ok := indexSecret.Data["chunk"]
		if !ok {
			pw.CloseWithError(fmt.Errorf("index secret %q missing chunk %d data", indexSecret.Name, 1))
			return
		}
		if _, err := pw.Write(firstChunkData); err != nil {
			pw.CloseWithError(fmt.Errorf("failed to write chunk %d data from %q: %w", 1, indexSecret.Name, err))
			return
		}
		for i, chunkName := range extraChunkNames {
			chunkSecret, err := c.client.Get(ctx, chunkName, metav1.GetOptions{})
			if err != nil {
				pw.CloseWithError(fmt.Errorf("failed to get chunk %d secret %q: %w", i+2, chunkName, err))
				return
			}
			chunkData, ok := chunkSecret.Data["chunk"]
			if !ok {
				pw.CloseWithError(fmt.Errorf("chunk %d secret %q missing chunk data", i+2, chunkName))
				return
			}
			if _, err := pw.Write(chunkData); err != nil {
				pw.CloseWithError(fmt.Errorf("failed to write chunk %d data from %q: %w", i+2, chunkName, err))
				return
			}
		}
	}()

	gzr, err := gzip.NewReader(pr)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	releaseDecoder := json.NewDecoder(gzr)
	var wrappedRelease releaseWrapper
	if err := releaseDecoder.Decode(&wrappedRelease); err != nil {
		return nil, fmt.Errorf("failed to decode release: %w", err)
	}

	r := wrappedRelease.Release
	r.Labels = filterSystemLabels(wrappedRelease.Labels)
	return &r, nil
}

func (c *chunkedSecrets) hashForData(data []byte) string {
	c.hashMu.Lock()
	defer c.hashMu.Unlock()

	c.hash.Reset()
	_, _ = c.hash.Write(data)
	return c.hashEncoding.EncodeToString(c.hash.Sum(nil))
}
