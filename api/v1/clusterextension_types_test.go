package v1_test

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/exp/slices" // replace with "slices" in go 1.21

	v1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/conditionsets"
)

// TODO Expand these tests to cover Types/Reasons/etc. from other APIs as well

func TestClusterExtensionTypeRegistration(t *testing.T) {
	types, err := parseConstants("Type")
	if err != nil {
		t.Fatalf("unable to parse Type constants %v", err)
	}

	for _, tt := range types {
		if !slices.Contains(conditionsets.ConditionTypes, tt) {
			t.Errorf("append Type%s to conditionsets.ConditionTypes in this package's init function", tt)
		}
	}

	for _, tt := range conditionsets.ConditionTypes {
		if !slices.Contains(types, tt) {
			t.Errorf("there must be a Type%[1]s string literal constant for type %[1]q (i.e. 'const Type%[1]s = %[1]q')", tt)
		}
	}
}

func TestClusterExtensionReasonRegistration(t *testing.T) {
	reasons, err := parseConstants("Reason")
	if err != nil {
		t.Fatalf("unable to parse Reason constants %v", err)
	}

	for _, r := range reasons {
		if !slices.Contains(conditionsets.ConditionReasons, r) {
			t.Errorf("append Reason%s to conditionsets.ConditionReasons in this package's init function.", r)
		}
	}
	for _, r := range conditionsets.ConditionReasons {
		if !slices.Contains(reasons, r) {
			t.Errorf("there must be a Reason%[1]s string literal constant for reason %[1]q (i.e. 'const Reason%[1]s = %[1]q')", r)
		}
	}
}

// parseConstants parses the values of the top-level constants that start with the given prefix,
// in the files clusterextension_types.go and common_types.go.
func parseConstants(prefix string) ([]string, error) {
	fset := token.NewFileSet()
	// An AST is a representation of the source code that can be traversed to extract information.
	// Converting files to AST representation to extract information.
	parseFiles, astFiles := []string{"clusterextension_types.go", "common_types.go"}, []*ast.File{}
	for _, file := range parseFiles {
		p, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			return nil, err
		}
		astFiles = append(astFiles, p)
	}

	var constValues []string

	// Iterate all of the top-level declarations in each file, looking
	// for constants that start with the prefix. When we find one, add
	// its value to the constValues list.
	for _, f := range astFiles {
		for _, d := range f.Decls {
			genDecl, ok := d.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, s := range genDecl.Specs {
				valueSpec, ok := s.(*ast.ValueSpec)
				if !ok || len(valueSpec.Names) != 1 || valueSpec.Names[0].Obj.Kind != ast.Con || !strings.HasPrefix(valueSpec.Names[0].String(), prefix) {
					continue
				}
				for _, val := range valueSpec.Values {
					lit, ok := val.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					v, err := strconv.Unquote(lit.Value)
					if err != nil {
						return nil, fmt.Errorf("unquote literal string %s: %v", lit.Value, err)
					}
					constValues = append(constValues, v)
				}
			}
		}
	}
	return constValues, nil
}

func TestServiceAccountMarshaling(t *testing.T) {
	tests := []struct {
		name          string
		spec          v1.ClusterExtensionSpec
		expectField   string
		unexpectField string
	}{
		{
			name: "ServiceAccount with name is marshaled",
			spec: v1.ClusterExtensionSpec{
				Namespace: "test-ns",
				ServiceAccount: v1.ServiceAccountReference{
					Name: "test-sa",
				},
				Source: v1.SourceConfig{
					SourceType: "Catalog",
					Catalog: &v1.CatalogFilter{
						PackageName: "test-package",
					},
				},
			},
			expectField: "serviceAccount",
		},
		{
			name: "ServiceAccount with empty name is omitted",
			spec: v1.ClusterExtensionSpec{
				Namespace:      "test-ns",
				ServiceAccount: v1.ServiceAccountReference{},
				Source: v1.SourceConfig{
					SourceType: "Catalog",
					Catalog: &v1.CatalogFilter{
						PackageName: "test-package",
					},
				},
			},
			unexpectField: "serviceAccount",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.spec)
			if err != nil {
				t.Fatalf("failed to marshal spec: %v", err)
			}

			jsonStr := string(data)

			if tt.expectField != "" && !strings.Contains(jsonStr, tt.expectField) {
				t.Errorf("expected field %q to be present in JSON output, got: %s", tt.expectField, jsonStr)
			}

			if tt.unexpectField != "" && strings.Contains(jsonStr, tt.unexpectField) {
				t.Errorf("expected field %q to be omitted from JSON output, got: %s", tt.unexpectField, jsonStr)
			}
		})
	}
}
