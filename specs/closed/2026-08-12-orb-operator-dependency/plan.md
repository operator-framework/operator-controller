# Implementation Plan

1. Add Go dependency and register scheme
   - `go get github.com/joelanford/orb-operator@latest`
   - Add `orbv1alpha1` import and `AddToScheme` call in `internal/operator-controller/scheme/scheme.go`
   - `go mod tidy`
   - Verify `go build ./...` succeeds

2. Update Makefile
   - Add `ORB_OPERATOR_VERSION` variable derived from `go list -m -f '{{.Version}}'`
   - Update the `install-sh` macro to accept an optional third argument (`$(3)`) for extra env vars, prepended before the envsubst call
   - Experimental `install-sh` call passes `ORB_OPERATOR_VERSION=$$(ORB_OPERATOR_VERSION)` as the third arg; standard call passes empty
   - Export `ORB_OPERATOR_VERSION` in `test-experimental-e2e`, `experimental-e2e-setup`, and `run-experimental` targets
   - Add `$$ORB_OPERATOR_VERSION` to the envsubst variable list in the `kind-deploy-%` recipe (the one that pipes to `bash -s`)
   - In the `release` target, add `$$ORB_OPERATOR_VERSION` to envsubst lists for both standard and experimental release install scripts, but only set the env var (`ORB_OPERATOR_VERSION=$(ORB_OPERATOR_VERSION)`) for the experimental one

3. Update install script
   - Add `orb_operator_version=$ORB_OPERATOR_VERSION` variable alongside the other version variables
   - After the cert-manager install/wait block, add a conditional orb-operator install block:
     - Gate on `[[ -n "$orb_operator_version" ]]`
     - Idempotency check: if the CRD and deployment already exist, skip with a message
     - Otherwise `kubectl apply -f` the release install.json URL
     - `kubectl_wait` for the orb-operator deployment

4. Verify
   - `make build`
   - `make test-unit`
   - `make lint`
   - `make verify`
