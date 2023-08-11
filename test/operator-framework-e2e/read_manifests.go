package operatore2e

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

var (
	scheme = runtime.NewScheme()

	codecs = serializer.NewCodecFactory(scheme)
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

// / collectKubernetesObjects collects the Kubernetes objects present in the bundle manifest folder for a particular package and its version
func collectKubernetesObjects(bundlePath, packageName, version string) ([]runtime.Object, error) {
	var objects []runtime.Object

	bundleManifestPath := filepath.Join(bundlePath, packageName+".v"+version, "manifests")
	err := filepath.Walk(bundleManifestPath, func(filePath string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("error walking path %s: %v", filePath, err)
		}

		if fileInfo.IsDir() {
			return nil
		}

		fileContent, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("error reading file %s: %v", filePath, err)
		}

		decoder := codecs.UniversalDecoder(scheme.PrioritizedVersionsAllGroups()...)
		yamlObjects := bytes.Split(fileContent, []byte("\n---\n"))
		for _, yamlObject := range yamlObjects {
			object, _, err := decoder.Decode(yamlObject, nil, nil)
			if err != nil {
				return fmt.Errorf("failed to decode file %s: %w", filePath, err)
			}
			objects = append(objects, object)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return objects, nil
}
