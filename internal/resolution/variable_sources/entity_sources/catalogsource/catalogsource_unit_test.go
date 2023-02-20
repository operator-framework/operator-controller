package catalogsource_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDeppy(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Deppy Suite")
}
