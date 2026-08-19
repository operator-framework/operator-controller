# Verification

## Implementation Correctness

- [ ] `go.mod` lists `github.com/joelanford/orb-operator` as a direct dependency
- [ ] `internal/operator-controller/scheme/scheme.go` registers `orbv1alpha1.AddToScheme`
- [ ] `scripts/install.tpl.sh` has a conditional orb-operator install block gated on `$orb_operator_version` being non-empty
- [ ] The install block uses the release URL pattern: `https://github.com/joelanford/orb-operator/releases/download/${orb_operator_version}/install.json`
- [ ] The install block waits for the orb-operator deployment to be ready
- [ ] Makefile derives `ORB_OPERATOR_VERSION` from `go list -m`
- [ ] Only experimental targets export `ORB_OPERATOR_VERSION`; standard targets do not

## Build Verification

- [ ] `make build` succeeds
- [ ] `make test-unit` passes
- [ ] `make lint` passes
- [ ] `make verify` passes

## Project Conventions

- [ ] Commit message uses `:seedling:` prefix (chore/dependency change)
- [ ] No unnecessary code changes beyond what is specified
- [ ] Install script follows existing patterns (idempotent check before install, `kubectl_wait` for readiness)
