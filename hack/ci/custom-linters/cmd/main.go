package main

import (
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/unitchecker"

	"github.com/operator-framework/operator-controller/hack/ci/custom-linters/analyzers"
)

// Define the custom Linters implemented in the project
var customLinters = []*analysis.Analyzer{
	analyzers.SetupLogErrorCheck,
}

func main() {
	unitchecker.Main(customLinters...)
}
