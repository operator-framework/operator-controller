package pullsecretcache

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/cache"
)

func SetupPullSecretCache(cacheOptions *cache.Options, globalPullSecretKey *types.NamespacedName, saKey types.NamespacedName) error {
	cacheOptions.ByObject[&corev1.ServiceAccount{}] = cache.ByObject{
		Namespaces: map[string]cache.Config{
			saKey.Namespace: {
				LabelSelector: labels.Everything(),
				FieldSelector: fields.SelectorFromSet(map[string]string{
					"metadata.name": saKey.Name,
				}),
			},
		},
	}

	secretCache := cache.ByObject{}
	secretCache.Namespaces = make(map[string]cache.Config, 2)
	secretCache.Namespaces[saKey.Namespace] = cache.Config{
		LabelSelector: labels.Everything(),
		FieldSelector: fields.Everything(),
	}
	if globalPullSecretKey != nil {
		secretCache.Namespaces[globalPullSecretKey.Namespace] = cache.Config{
			LabelSelector: labels.Everything(),
			FieldSelector: fields.SelectorFromSet(map[string]string{
				"metadata.name": globalPullSecretKey.Name,
			}),
		}
	}
	cacheOptions.ByObject[&corev1.Secret{}] = secretCache

	return nil
}
