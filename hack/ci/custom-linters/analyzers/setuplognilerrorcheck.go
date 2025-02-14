package analyzers

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

var SetupLogErrorCheck = &analysis.Analyzer{
	Name: "setuplogerrorcheck",
	Doc: "Detects and reports improper usages of logger.Error() calls to enforce good practices " +
		"and prevent silent failures.",
	Run: runSetupLogErrorCheck,
}

func runSetupLogErrorCheck(pass *analysis.Pass) (interface{}, error) {
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			callExpr, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			// Ensure function being called is logger.Error
			selectorExpr, ok := callExpr.Fun.(*ast.SelectorExpr)
			if !ok || selectorExpr.Sel.Name != "Error" {
				return true
			}

			// Ensure receiver (logger) is identified
			ident, ok := selectorExpr.X.(*ast.Ident)
			if !ok {
				return true
			}

			// Verify if the receiver is logr.Logger
			obj := pass.TypesInfo.ObjectOf(ident)
			if obj == nil {
				return true
			}

			named, ok := obj.Type().(*types.Named)
			if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != "github.com/go-logr/logr" || named.Obj().Name() != "Logger" {
				return true
			}

			if len(callExpr.Args) == 0 {
				return true
			}

			// Get the actual source code line where the issue occurs
			var srcBuffer bytes.Buffer
			if err := format.Node(&srcBuffer, pass.Fset, callExpr); err != nil {
				return true
			}
			sourceLine := srcBuffer.String()

			// Check if the first argument of the error log is nil
			firstArg, ok := callExpr.Args[0].(*ast.Ident)
			if ok && firstArg.Name == "nil" {
				suggestedError := "errors.New(\"kind error (i.e. configuration error)\")"
				suggestedMessage := "\"error message describing the failed operation\""

				if len(callExpr.Args) > 1 {
					if msgArg, ok := callExpr.Args[1].(*ast.BasicLit); ok && msgArg.Kind == token.STRING {
						suggestedMessage = msgArg.Value
					}
				}

				pass.Reportf(callExpr.Pos(),
					"Incorrect usage of 'logger.Error(nil, ...)'. The first argument must be a non-nil 'error'. "+
						"Passing 'nil' may hide error details, making debugging harder.\n\n"+
						"\U0001F41B **What is wrong?**\n   %s\n\n"+
						"\U0001F4A1 **How to solve? Return the error, i.e.:**\n   logger.Error(%s, %s, \"key\", value)\n\n",
					sourceLine, suggestedError, suggestedMessage)
				return true
			}

			// Ensure at least two arguments exist (error + message)
			if len(callExpr.Args) < 2 {
				pass.Reportf(callExpr.Pos(),
					"Incorrect usage of 'logger.Error(error, ...)'. Expected at least an error and a message string.\n\n"+
						"\U0001F41B **What is wrong?**\n   %s\n\n"+
						"\U0001F4A1 **How to solve?**\n   Provide a message, e.g. logger.Error(err, \"descriptive message\")\n\n",
					sourceLine)
				return true
			}

			// Ensure key-value pairs (if any) are valid
			if (len(callExpr.Args)-2)%2 != 0 {
				pass.Reportf(callExpr.Pos(),
					"Incorrect usage of 'logger.Error(error, \"msg\", ...)'. Key-value pairs must be provided after the message, but an odd number of arguments was found.\n\n"+
						"\U0001F41B **What is wrong?**\n   %s\n\n"+
						"\U0001F4A1 **How to solve?**\n   Ensure all key-value pairs are complete, e.g. logger.Error(err, \"msg\", \"key\", value, \"key2\", value2)\n\n",
					sourceLine)
				return true
			}

			for i := 2; i < len(callExpr.Args); i += 2 {
				keyArg := callExpr.Args[i]
				keyType := pass.TypesInfo.TypeOf(keyArg)
				if keyType == nil || keyType.String() != "string" {
					pass.Reportf(callExpr.Pos(),
						"Incorrect usage of 'logger.Error(error, \"msg\", key, value)'. Keys in key-value pairs must be strings, but got: %s.\n\n"+
							"\U0001F41B **What is wrong?**\n   %s\n\n"+
							"\U0001F4A1 **How to solve?**\n   Ensure keys are strings, e.g. logger.Error(err, \"msg\", \"key\", value)\n\n",
						keyType, sourceLine)
					return true
				}
			}

			return true
		})
	}
	return nil, nil
}
