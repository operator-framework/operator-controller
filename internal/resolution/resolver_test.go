package resolution_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/deppy/pkg/deppy/solver"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/olm"
)

func TestOperatorResolver(t *testing.T) {

	testEntityCache := map[deppy.Identifier]input.Entity{"operatorhub/prometheus/0.37.0": *input.NewEntity(
		"operatorhub/prometheus/0.37.0", map[string]string{
			"olm.bundle.path": `"foo.io/bar/baz"`,
			"olm.channel":     "{\"channelName\":\"beta\",\"priority\":0,\"replaces\":\"prometheusoperator.0.32.0\"}",
			"olm.gvk":         "[{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"}, {\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1\"}]",
			"olm.package":     "{\"packageName\":\"prometheus\",\"version\":\"0.37.0\"}",
		}),
		"operatorhub/prometheus/0.47.0": *input.NewEntity("operatorhub/prometheus/0.47.0", map[string]string{
			"olm.bundle.path": `"foo.io/bar/baz"`,
			"olm.channel":     "{\"channelName\":\"beta\",\"priority\":0,\"replaces\":\"prometheusoperator.0.37.0\"}",
			"olm.gvk":         "[{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"}, {\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1alpha1\"}]",
			"olm.package":     "{\"packageName\":\"prometheus\",\"version\":\"0.47.0\"}",
		}),
		"operatorhub/packageA/2.0.0": *input.NewEntity("operatorhub/packageA/2.0.0", map[string]string{
			"olm.bundle.path": `"foo.io/packageA/packageA:v2.0.0"`,
			"olm.channel":     "{\"channelName\":\"stable\",\"priority\":0}",
			"olm.gvk":         "[{\"group\":\"foo.io\",\"kind\":\"Foo\",\"version\":\"v1\"}]",
			"olm.package":     "{\"packageName\":\"packageA\",\"version\":\"2.0.0\"}",
		}),
	}

	testEntitySource := input.NewCacheQuerier(testEntityCache)

	testResource := []client.Object{
		&v1alpha1.Operator{
			ObjectMeta: metav1.ObjectMeta{
				Name: "prometheus",
			},
			Spec: v1alpha1.OperatorSpec{
				PackageName: "prometheus",
			},
		},
		&v1alpha1.Operator{
			ObjectMeta: metav1.ObjectMeta{
				Name: "packageA",
			},
			Spec: v1alpha1.OperatorSpec{
				PackageName: "packageA",
			},
		},
	}

	for _, tt := range []struct {
		name                      string
		client                    client.Client
		entitySource              input.EntitySource
		expectedSelectedVariables []deppy.Identifier
		expectedError             error
	}{
		{
			name:         "should resolve the packages described by the available Operator resources",
			client:       FakeClient(testResource...),
			entitySource: testEntitySource,
			expectedSelectedVariables: []deppy.Identifier{
				"operatorhub/packageA/2.0.0",
				"operatorhub/prometheus/0.47.0",
				"required package packageA",
				"required package prometheus"},
			expectedError: nil,
		},
		{
			name:                      "should not return an error if there are no Operator resources",
			client:                    FakeClient(),
			entitySource:              testEntitySource,
			expectedSelectedVariables: []deppy.Identifier{},
			expectedError:             nil,
		},
		{
			name:          "should return an error if the entity source throws an error",
			client:        FakeClient(testResource...),
			entitySource:  FailEntitySource{},
			expectedError: fmt.Errorf("error calling filter in entity source"),
		},
		{
			name:          "should return an error if the client throws an error",
			client:        NewFailClientWithError(fmt.Errorf("something bad happened")),
			entitySource:  testEntitySource,
			expectedError: fmt.Errorf("something bad happened"),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resolver := resolution.NewOperatorResolver(tt.client, tt.entitySource)
			solution, err := resolver.Resolve(context.Background())

			if tt.expectedError != nil {
				assert.Equal(t, tt.expectedError, err)
				assert.Nil(t, solution)
			} else {
				assert.Len(t, solution.SelectedVariables(), len(tt.expectedSelectedVariables))
				for _, identifier := range tt.expectedSelectedVariables {
					assert.True(t, solution.IsSelected(identifier))
				}
				assert.NoError(t, err)
			}

		})
	}
}

func FakeClient(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		panic(fmt.Sprintf("error creating fake client: %s", err))
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

var _ input.EntitySource = &FailEntitySource{}

type FailEntitySource struct{}

func (f FailEntitySource) Get(ctx context.Context, id deppy.Identifier) (*input.Entity, error) {
	return nil, fmt.Errorf("error calling get in entity source")
}

func (f FailEntitySource) Filter(ctx context.Context, filter input.Predicate) (input.EntityList, error) {
	return nil, fmt.Errorf("error calling filter in entity source")
}

func (f FailEntitySource) GroupBy(ctx context.Context, fn input.GroupByFunction) (input.EntityListMap, error) {
	return nil, fmt.Errorf("error calling group by in entity source")
}

func (f FailEntitySource) Iterate(ctx context.Context, fn input.IteratorFunction) error {
	return fmt.Errorf("error calling iterate in entity source")
}

var _ client.Client = &FailClient{}

type FailClient struct {
	client.Client
	err error
}

func NewFailClientWithError(err error) client.Client {
	return &FailClient{
		Client: FakeClient(),
		err:    err,
	}
}

func (f FailClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return f.err
}

func (f FailClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return f.err
}

func (f FailClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	return f.err
}

func (f FailClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	return f.err
}

func (f FailClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return f.err
}

func (f FailClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return f.err
}

func (f FailClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	return f.err
}
