/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package variablesources_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"

	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
)

func TestNestedVariableSource(t *testing.T) {
	for _, tt := range []struct {
		name       string
		varSources []*mockVariableSource

		wantVariables []deppy.Variable
		wantErr       string
	}{
		{
			name: "multiple nested sources",
			varSources: []*mockVariableSource{
				{fakeVariables: []deppy.Variable{mockVariable("fake-var-1"), mockVariable("fake-var-2")}},
				{fakeVariables: []deppy.Variable{mockVariable("fake-var-3")}},
			},
			wantVariables: []deppy.Variable{mockVariable("fake-var-1"), mockVariable("fake-var-2"), mockVariable("fake-var-3")},
		},
		{
			name:    "error when no nested sources provided",
			wantErr: "empty nested variable sources",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			mockEntitySource := input.NewCacheQuerier(map[deppy.Identifier]input.Entity{})

			nestedSource := variablesources.NestedVariableSource{}
			for i := range tt.varSources {
				i := i // Same reason as https://go.dev/doc/faq#closures_and_goroutines
				nestedSource = append(nestedSource, func(inputVariableSource input.VariableSource) (input.VariableSource, error) {
					if i == 0 {
						assert.Nil(t, inputVariableSource)
					} else {
						assert.Equal(t, tt.varSources[i-1], inputVariableSource)

						tt.varSources[i].inputVariableSource = inputVariableSource
					}

					return tt.varSources[i], nil
				})
			}

			variables, err := nestedSource.GetVariables(ctx, mockEntitySource)
			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.wantVariables, variables)
		})
	}

	t.Run("error from a nested constructor", func(t *testing.T) {
		ctx := context.Background()
		mockEntitySource := input.NewCacheQuerier(map[deppy.Identifier]input.Entity{})

		nestedSource := variablesources.NestedVariableSource{
			func(inputVariableSource input.VariableSource) (input.VariableSource, error) {
				return nil, errors.New("fake error from a constructor")
			},
		}

		variables, err := nestedSource.GetVariables(ctx, mockEntitySource)
		assert.EqualError(t, err, "fake error from a constructor")
		assert.Nil(t, variables)
	})
}

func TestSliceVariableSource(t *testing.T) {
	for _, tt := range []struct {
		name       string
		varSources []input.VariableSource

		wantVariables []deppy.Variable
		wantErr       string
	}{
		{
			name: "multiple sources in the slice",
			varSources: []input.VariableSource{
				&mockVariableSource{fakeVariables: []deppy.Variable{mockVariable("fake-var-1"), mockVariable("fake-var-2")}},
				&mockVariableSource{fakeVariables: []deppy.Variable{mockVariable("fake-var-3")}},
			},
			wantVariables: []deppy.Variable{mockVariable("fake-var-1"), mockVariable("fake-var-2"), mockVariable("fake-var-3")},
		},
		{
			name: "error from GetVariables",
			varSources: []input.VariableSource{
				&mockVariableSource{fakeVariables: []deppy.Variable{mockVariable("fake-var-1"), mockVariable("fake-var-2")}},
				&mockVariableSource{fakeError: errors.New("fake error from GetVariables")},
			},
			wantErr: "fake error from GetVariables",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			mockEntitySource := input.NewCacheQuerier(map[deppy.Identifier]input.Entity{})

			sliceSource := variablesources.SliceVariableSource(tt.varSources)
			variables, err := sliceSource.GetVariables(ctx, mockEntitySource)
			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.wantVariables, variables)
		})
	}
}

var _ input.VariableSource = &mockVariableSource{}

type mockVariableSource struct {
	inputVariableSource input.VariableSource
	fakeVariables       []deppy.Variable
	fakeError           error
}

func (m *mockVariableSource) GetVariables(ctx context.Context, entitySource input.EntitySource) ([]deppy.Variable, error) {
	if m.fakeError != nil {
		return nil, m.fakeError
	}

	if m.inputVariableSource == nil {
		return m.fakeVariables, nil
	}

	nestedVars, err := m.inputVariableSource.GetVariables(ctx, entitySource)
	if err != nil {
		return nil, err
	}

	return append(nestedVars, m.fakeVariables...), nil
}

var _ deppy.Variable = mockVariable("")

type mockVariable string

func (m mockVariable) Identifier() deppy.Identifier {
	return deppy.IdentifierFromString(string(m))
}

func (m mockVariable) Constraints() []deppy.Constraint {
	return nil
}
