package catalogmetadata

import (
	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

type Schemas interface {
	Package | Bundle | Channel
}

type Package struct {
	declcfg.Package
}

type Channel struct {
	declcfg.Channel
}

type Bundle struct {
	declcfg.Bundle
}
