package values

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

type Source interface {
	Values(context.Context) (map[string]interface{}, error)
}

func SourcesFromClusterExtension(ext *ocv1.ClusterExtension, kubeClient kubernetes.Interface) []Source {
	if ext.Spec.Template == nil {
		return nil
	}
	sources := make([]Source, 0, len(ext.Spec.Template.ValuesSources))
	for _, valuesSource := range ext.Spec.Template.ValuesSources {
		switch valuesSource.Type {
		case ocv1.ValuesSourceTypeInline:
			sources = append(sources, InlineSource{Data: valuesSource.Inline.Raw})
		case ocv1.ValuesSourceTypeConfigMap:
			sources = append(sources, ConfigMapSource{
				KubeClient: kubeClient,
				ObjectKey:  types.NamespacedName{Namespace: ext.Spec.Namespace, Name: valuesSource.ConfigMap.Name},
				ValuesKey:  valuesSource.ConfigMap.Key,
			})
		case ocv1.ValuesSourceTypeSecret:
			sources = append(sources, SecretSource{
				KubeClient: kubeClient,
				ObjectKey:  types.NamespacedName{Namespace: ext.Spec.Namespace, Name: valuesSource.Secret.Name},
				ValuesKey:  valuesSource.Secret.Key,
			})
		default:
			panic(fmt.Sprintf("unknown values source type %q", valuesSource.Type))
		}
	}
	return sources
}

func MergeAll(ctx context.Context, sources ...Source) (map[string]interface{}, error) {
	var (
		allValues map[string]interface{}
		errs      []error
	)
	for i, provider := range sources {
		providerValues, err := provider.Values(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to get values from valuesSource[%d]: %w", i, err))
			continue
		}
		allValues = mergeMaps(allValues, providerValues)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return allValues, nil
}

func mergeMaps(a, b map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(a))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if v, ok := v.(map[string]interface{}); ok {
			if bv, ok := out[k]; ok {
				if bv, ok := bv.(map[string]interface{}); ok {
					out[k] = mergeMaps(bv, v)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}

type InlineSource struct {
	Data []byte
}

func (p InlineSource) Values(_ context.Context) (map[string]interface{}, error) {
	values := map[string]interface{}{}
	if err := json.Unmarshal(p.Data, &values); err != nil {
		return nil, fmt.Errorf("failed to unmarshal inline values: %w", err)
	}
	return values, nil
}

type ConfigMapSource struct {
	KubeClient kubernetes.Interface
	ObjectKey  types.NamespacedName
	ValuesKey  string
}

func (p ConfigMapSource) Values(ctx context.Context) (map[string]interface{}, error) {
	cm, err := p.KubeClient.CoreV1().ConfigMaps(p.ObjectKey.Namespace).Get(ctx, p.ObjectKey.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get configmap %q: %w", p.ObjectKey, err)
	}
	valuesData, ok := cm.Data[p.ValuesKey]
	if !ok {
		return nil, fmt.Errorf("configmap %q does not have key %q", p.ObjectKey, p.ValuesKey)
	}
	values := map[string]interface{}{}
	if err := yaml.Unmarshal([]byte(valuesData), &values); err != nil {
		return nil, fmt.Errorf("failed to unmarshal values from configmap %q, key %q: %w", p.ObjectKey, p.ValuesKey, err)
	}
	return values, nil
}

type SecretSource struct {
	KubeClient kubernetes.Interface
	ObjectKey  types.NamespacedName
	ValuesKey  string
}

func (p SecretSource) Values(ctx context.Context) (map[string]interface{}, error) {
	secret, err := p.KubeClient.CoreV1().Secrets(p.ObjectKey.Namespace).Get(ctx, p.ObjectKey.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get secret %q: %w", p.ObjectKey, err)
	}
	valuesData, ok := secret.Data[p.ValuesKey]
	if !ok {
		return nil, fmt.Errorf("secret %q does not have key %q", p.ObjectKey, p.ValuesKey)
	}
	values := map[string]interface{}{}
	if err := yaml.Unmarshal(valuesData, &values); err != nil {
		return nil, fmt.Errorf("failed to unmarshal values from secret %q, key %q: %w", p.ObjectKey, p.ValuesKey, err)
	}
	return values, nil
}
