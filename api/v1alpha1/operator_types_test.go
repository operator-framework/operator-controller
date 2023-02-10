package v1alpha1_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	operatorutil "github.com/operator-framework/operator-controller/internal/util"
)

var _ = Describe("OperatorTypes", func() {
	Describe("Condition Type and Reason constants", func() {
		It("should register types in operatorutil.ConditionTypes", func() {
			types, err := parseConstants("Type")
			Expect(err).NotTo(HaveOccurred())

			for _, t := range types {
				Expect(t).To(BeElementOf(operatorutil.ConditionTypes), "Append Type%s to operatorutil.ConditionTypes in this package's init function.", t)
			}
			for _, t := range operatorutil.ConditionTypes {
				Expect(t).To(BeElementOf(types), "There must be a Type%[1]s string literal constant for type %[1]q (i.e. 'const Type%[1]s = %[1]q')", t)
			}
		})
		It("should register reasons in operatorutil.ConditionReasons", func() {
			reasons, err := parseConstants("Reason")
			Expect(err).NotTo(HaveOccurred())

			for _, r := range reasons {
				Expect(r).To(BeElementOf(operatorutil.ConditionReasons), "Append Reason%s to operatorutil.ConditionReasons in this package's init function.", r)
			}
			for _, r := range operatorutil.ConditionReasons {
				Expect(r).To(BeElementOf(reasons), "There must be a Reason%[1]s string literal constant for reason %[1]q (i.e. 'const Reason%[1]s = %[1]q')", r)
			}
		})
	})
})

// parseConstants parses the values of the top-level constants in the current
// directory whose names start with the given prefix. When running as part of a
// test, the current directory is the directory of the file that contains the
// test in which this function is called.
func parseConstants(prefix string) ([]string, error) {
	fset := token.NewFileSet()
	// ParseDir returns a map of package name to package ASTs. An AST is a representation of the source code
	// that can be traversed to extract information. The map is keyed by the package name.
	pkgs, err := parser.ParseDir(fset, ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		return nil, err
	}

	var constValues []string

	// Iterate all of the top-level declarations in each package's files,
	// looking for constants that start with the prefix. When we find one,
	// add its value to the constValues list.
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
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
	}
	return constValues, nil
}
