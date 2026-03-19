package migration

import (
	"testing"

	"github.com/stretchr/testify/assert"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
)

func countFailed(checks []CheckResult) int {
	n := 0
	for _, c := range checks {
		if !c.Passed {
			n++
		}
	}
	return n
}

func TestCheckNoDependencies(t *testing.T) {
	tests := []struct {
		name       string
		properties string
		wantFailed int
	}{
		{
			name:       "empty properties",
			properties: "",
			wantFailed: 0,
		},
		{
			name:       "no dependencies",
			properties: `[{"type":"olm.package","value":{"packageName":"test","version":"1.0.0"}}]`,
			wantFailed: 0,
		},
		{
			name:       "has package required dependency",
			properties: `[{"type":"olm.package.required","value":{"packageName":"dep","versionRange":">=1.0.0"}}]`,
			wantFailed: 1,
		},
		{
			name:       "has gvk required dependency",
			properties: `[{"type":"olm.gvk.required","value":{"group":"example.com","kind":"Foo","version":"v1"}}]`,
			wantFailed: 1,
		},
		{
			name:       "has both dependency types",
			properties: `[{"type":"olm.package.required","value":{"packageName":"dep1"}},{"type":"olm.gvk.required","value":{"group":"example.com","kind":"Foo","version":"v1"}}]`,
			wantFailed: 2,
		},
		{
			name:       "wrapped object format - no dependencies",
			properties: `{"properties":[{"type":"olm.gvk","value":{"group":"example.com","kind":"Foo","version":"v1"}},{"type":"olm.package","value":{"packageName":"test","version":"1.0.0"}}]}`,
			wantFailed: 0,
		},
		{
			name:       "wrapped object format - has dependency",
			properties: `{"properties":[{"type":"olm.package.required","value":{"packageName":"dep","versionRange":">=1.0.0"}}]}`,
			wantFailed: 1,
		},
		{
			name:       "invalid JSON",
			properties: `not-json`,
			wantFailed: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checks := checkNoDependencies(tt.properties)
			assert.Equal(t, tt.wantFailed, countFailed(checks))
		})
	}
}

func TestCheckNoAPIServices(t *testing.T) {
	tests := []struct {
		name       string
		csv        *operatorsv1alpha1.ClusterServiceVersion
		wantPassed bool
	}{
		{
			name: "no api service definitions",
			csv: &operatorsv1alpha1.ClusterServiceVersion{
				Spec: operatorsv1alpha1.ClusterServiceVersionSpec{},
			},
			wantPassed: true,
		},
		{
			name: "has owned api services",
			csv: &operatorsv1alpha1.ClusterServiceVersion{
				Spec: operatorsv1alpha1.ClusterServiceVersionSpec{
					APIServiceDefinitions: operatorsv1alpha1.APIServiceDefinitions{
						Owned: []operatorsv1alpha1.APIServiceDescription{
							{Name: "test"},
						},
					},
				},
			},
			wantPassed: false,
		},
		{
			name: "has required api services",
			csv: &operatorsv1alpha1.ClusterServiceVersion{
				Spec: operatorsv1alpha1.ClusterServiceVersionSpec{
					APIServiceDefinitions: operatorsv1alpha1.APIServiceDefinitions{
						Required: []operatorsv1alpha1.APIServiceDescription{
							{Name: "test"},
						},
					},
				},
			},
			wantPassed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checkNoAPIServices(tt.csv)
			assert.Equal(t, tt.wantPassed, result.Passed)
		})
	}
}
