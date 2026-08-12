# Requirements

- Add `github.com/joelanford/orb-operator` as a direct dependency in `go.mod`
- Register `orbv1alpha1.AddToScheme` in the shared scheme package
- Add orb-operator installation to `scripts/install.tpl.sh`, gated on `ORB_OPERATOR_VERSION` being non-empty
- Pass `ORB_OPERATOR_VERSION` through the Makefile for experimental targets only
- The standard deployment variant must be unaffected

## Acceptance Criteria

- `go build ./...` succeeds with the new dependency
- `make test-unit` passes (no regressions from the new dependency)
- `make lint` passes
- `make verify` passes (generated code is up-to-date)
- `make manifests/experimental.yaml` produces a valid manifest
- The experimental install script includes the orb-operator install block
- The standard install script does not install orb-operator
