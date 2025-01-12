package analyzers

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestSetupLogErrorCheck(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, SetupLogErrorCheck)
}
