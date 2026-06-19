package testing

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
)

type FakeBundleSource func() (bundle.RegistryV1, error)

func (f FakeBundleSource) GetBundle() (bundle.RegistryV1, error) {
	return f()
}

func ToUnstructuredT(t *testing.T, obj client.Object) *unstructured.Unstructured {
	u, err := util.ToUnstructured(obj)
	require.NoError(t, err)
	return u
}
