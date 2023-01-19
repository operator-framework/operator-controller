package variable_sources

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestVariableSources(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "VariableSource Suite")
}
