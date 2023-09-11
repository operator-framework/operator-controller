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

package variablesources

import (
	"context"
	"errors"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
)

var _ input.VariableSource = &SliceVariableSource{}
var _ input.VariableSource = &NestedVariableSource{}

type NestedVariableSource []func(inputVariableSource input.VariableSource) (input.VariableSource, error)

func (s NestedVariableSource) GetVariables(ctx context.Context, _ input.EntitySource) ([]deppy.Variable, error) {
	if len(s) == 0 {
		return nil, errors.New("empty nested variable sources")
	}

	var variableSource input.VariableSource
	var err error
	for _, constructor := range s {
		variableSource, err = constructor(variableSource)
		if err != nil {
			return nil, err
		}
	}

	return variableSource.GetVariables(ctx, nil)
}

type SliceVariableSource []input.VariableSource

func (s SliceVariableSource) GetVariables(ctx context.Context, _ input.EntitySource) ([]deppy.Variable, error) {
	var variables []deppy.Variable
	for _, variableSource := range s {
		inputVariables, err := variableSource.GetVariables(ctx, nil)
		if err != nil {
			return nil, err
		}
		variables = append(variables, inputVariables...)
	}

	return variables, nil
}
