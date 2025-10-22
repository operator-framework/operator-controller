package v1_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slices" // replace with "slices" in go 1.21
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
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

func Test_ExtensionConfigBytes(t *testing.T) {
	for _, tc := range []struct {
		name                string
		config              *ocv1.ClusterExtensionConfig
		expectedConfigBytes []byte
	}{
		{
			name:                "nil for no config",
			config:              nil,
			expectedConfigBytes: nil,
		},
		{
			name: "nil for unknown config type",
			config: &ocv1.ClusterExtensionConfig{
				ConfigType: "not-a-real-config-type",
			},
			expectedConfigBytes: nil,
		},
		{
			name: ".spec.config.inline.raw for Inline config",
			config: &ocv1.ClusterExtensionConfig{
				ConfigType: ocv1.ClusterExtensionConfigTypeInline,
				Inline: &apiextensionsv1.JSON{
					Raw: []byte(`{"key": "value"}`),
				},
			},
			expectedConfigBytes: []byte(`{"key": "value"}`),
		},
		{
			name: "nil when .spec.config.inline is nil",
			config: &ocv1.ClusterExtensionConfig{
				ConfigType: ocv1.ClusterExtensionConfigTypeInline,
			},
			expectedConfigBytes: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ext := &ocv1.ClusterExtension{
				Spec: ocv1.ClusterExtensionSpec{
					Config: tc.config,
				},
			}
			require.Equal(t, tc.expectedConfigBytes, ext.ExtensionConfigBytes())
		})
	}
}
