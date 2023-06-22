package variablesources_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestVariableSources(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Variable Sources Suite")
}
