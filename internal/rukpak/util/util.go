package util

import (
	"io"

	"k8s.io/cli-runtime/pkg/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func MergeMaps(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func ManifestObjects(r io.Reader, name string) ([]client.Object, error) {
	result := resource.NewLocalBuilder().Flatten().Unstructured().Stream(r, name).Do()
	if err := result.Err(); err != nil {
		return nil, err
	}
	infos, err := result.Infos()
	if err != nil {
		return nil, err
	}
	return infosToObjects(infos), nil
}

func infosToObjects(infos []*resource.Info) []client.Object {
	objects := make([]client.Object, 0, len(infos))
	for _, info := range infos {
		clientObject := info.Object.(client.Object)
		objects = append(objects, clientObject)
	}
	return objects
}
