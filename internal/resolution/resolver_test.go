package resolution_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/deppy/pkg/deppy/solver"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/olm"
)

func TestOperatorResolver(t *testing.T) {

	testEntityCache := map[deppy.Identifier]input.Entity{ "operatorhub/prometheus/0.37.0": *input.NewEntity("operatorhub/prometheus/0.37.0" , map[string]string{
			"olm.bundle.path": `"quay.io/operatorhubio/prometheus@sha256:3e281e587de3d03011440685fc4fb782672beab044c1ebadc42788ce05a21c35"`,
			"olm.channel":     "{\"channelName\":\"beta\",\"priority\":0,\"replaces\":\"prometheusoperator.0.32.0\"}",
			"olm.gvk":         "[{\"group\":\"monitoring.coreos.com\",\"kind\":\"Alertmanager\",\"version\":\"v1\"}, {\"group\":\"monitoring.coreos.com\",\"kind\":\"Prometheus\",\"version\":\"v1\"}]",
			"olm.package":     "{\"packageName\":\"prometheus\",\"version\":\"0.37.0\"}",
		}),
		"operatorhub/prometheus/0.47.0": *input.NewEntity("operatorhub/prometheus/0.47.0", map[string]string{
			"olm.bundle.path": `"quay.io/operatorhubio/prometheus@sha256:5b04c49d8d3eff6a338b56ec90bdf491d501fe301c9cdfb740e5bff6769a21ed"`,
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
	
	errorClient := NewFailClientWithError(fmt.Errorf("something bad happened"))

	for _, tt := range []struct {
		Name				string
		Client				client.Client
		EntitySource		input.EntitySource
		SelectedVariableCnt	int
		ExpectedError		error
	}{	
		{
			Name: "should resolve the packages described by the available Operator resources",
			Client: FakeClient(testResource...),
			EntitySource: testEntitySource,
			SelectedVariableCnt: 4,
			ExpectedError: nil,
		},
		{
			Name: "should not return an error if there are no Operator resources",
			Client: FakeClient([]client.Object{}...),
			EntitySource: testEntitySource,
			SelectedVariableCnt: 0,
			ExpectedError: nil,
		},
		{
			Name: "should return an error if the entity source throws an error",
			Client: FakeClient(testResource...),
			EntitySource: FailEntitySource{},
			ExpectedError: fmt.Errorf("error calling filter in entity source"),
		}, 
		{
			Name: "should return an error if the client throws an error",
			Client: errorClient,
			EntitySource: testEntitySource,
			ExpectedError: fmt.Errorf("something bad happened"),
		}, 
	} {
		t.Run(tt.Name, func(t *testing.T)  {
			client :=  tt.Client
			entitySource := tt.EntitySource 
			resolver := resolution.NewOperatorResolver(client, entitySource)
			solution, err := resolver.Resolve(context.Background())	

			if err != nil {
				assert.Equal(t, err, tt.ExpectedError)
				assert.Nil(t, solution)
			} else {
				assert.Len(t, solution.SelectedVariables(), tt.SelectedVariableCnt)
				if len(solution.SelectedVariables()) == 4 {
					assert.True(t, solution.IsSelected("operatorhub/packageA/2.0.0"))
					assert.True(t, solution.IsSelected("operatorhub/prometheus/0.47.0"))
					assert.True(t, solution.IsSelected("required package packageA"))
					assert.True(t, solution.IsSelected("required package prometheus"))
					assert.False(t, solution.IsSelected("operatorhub/prometheus/0.37.0"))
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
