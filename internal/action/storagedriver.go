package action

import (
	"context"
	"fmt"

	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	clientcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	helmclient "github.com/operator-framework/operator-controller/internal/helm/client"
	"github.com/operator-framework/operator-controller/internal/storage"
)

func ChunkedStorageDriverMapper(secretsGetter clientcorev1.SecretsGetter, reader client.Reader, namespace string) helmclient.ObjectToStorageDriverMapper {
	secretsClient := newSecretsDelegatingClient(secretsGetter, reader, namespace)
	return func(ctx context.Context, object client.Object, config *rest.Config) (driver.Driver, error) {
		log := logf.FromContext(ctx).V(2)
		ownerRefs := []metav1.OwnerReference{*metav1.NewControllerRef(object, object.GetObjectKind().GroupVersionKind())}
		ownerRefSecretClient := helmclient.NewOwnerRefSecretClient(secretsClient, ownerRefs, func(secret *corev1.Secret) bool {
			return secret.Type == storage.SecretTypeChunkedIndex
		})
		return storage.NewChunkedSecrets(ownerRefSecretClient, "operator-controller", storage.ChunkedSecretsConfig{
			ChunkSize:      1024 * 1024,
			MaxReadChunks:  10,
			MaxWriteChunks: 10,
			Log:            func(format string, args ...interface{}) { log.Info(fmt.Sprintf(format, args...)) },
		}), nil
	}
}

var _ clientcorev1.SecretInterface = &secretsDelegatingClient{}

type secretsDelegatingClient struct {
	clientcorev1.SecretInterface
	reader    client.Reader
	namespace string
}

func newSecretsDelegatingClient(secretsGetter clientcorev1.SecretsGetter, reader client.Reader, namespace string) clientcorev1.SecretInterface {
	return &secretsDelegatingClient{
		SecretInterface: secretsGetter.Secrets(namespace),
		namespace:       namespace,
		reader:          reader,
	}
}

func (s secretsDelegatingClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.Secret, error) {
	var secret corev1.Secret
	if err := s.reader.Get(ctx, client.ObjectKey{Namespace: s.namespace, Name: name}, &secret, &client.GetOptions{Raw: &opts}); err != nil {
		return nil, err
	}
	return &secret, nil
}

func (s secretsDelegatingClient) List(ctx context.Context, opts metav1.ListOptions) (*corev1.SecretList, error) {
	listOpts, err := metaOptionsToClientOptions(s.namespace, opts)
	if err != nil {
		return nil, err
	}

	var secrets corev1.SecretList
	if err := s.reader.List(ctx, &secrets, listOpts); err != nil {
		return nil, err
	}
	return &secrets, nil
}

func (s secretsDelegatingClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	panic("intentionally not implemented: watch is not intended to be called")
}

func metaOptionsToClientOptions(namespace string, opts metav1.ListOptions) (*client.ListOptions, error) {
	clientListOptions := &client.ListOptions{
		Namespace: namespace,
		Limit:     opts.Limit,
		Continue:  opts.Continue,
	}

	if opts.LabelSelector != "" {
		labelSelector, err := k8slabels.Parse(opts.LabelSelector)
		if err != nil {
			return nil, err
		}
		clientListOptions.LabelSelector = labelSelector
	}

	if opts.FieldSelector != "" {
		fieldSelector, err := fields.ParseSelector(opts.FieldSelector)
		if err != nil {
			return nil, err
		}
		clientListOptions.FieldSelector = fieldSelector
	}

	opts.LabelSelector = ""
	opts.FieldSelector = ""
	opts.Limit = 0
	opts.Continue = ""
	clientListOptions.Raw = &opts

	return clientListOptions, nil
}
