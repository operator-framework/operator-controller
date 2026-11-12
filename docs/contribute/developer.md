
## Getting Started

The following `make run` starts a [KIND](https://sigs.k8s.io/kind) cluster for you to get a local cluster for testing, see the manual install steps below for how to run against a remote cluster.

!!! note
    You will need a container runtime environment like Docker to run Kind. Kind also has experimental support for Podman.

    If you are on MacOS, see [Special Setup for MacOS](#special-setup-for-macos).

### Quickstart Installation

First, you need to install the CRDs and the operator-controller into a new [KIND cluster](https://kind.sigs.k8s.io/). You can do this by running:

```sh
make run
```

This builds the controller images, creates a KIND cluster, and deploys OLMv1 with
cert-manager using the standard configuration.

To develop with the experimental configuration instead, use:

```sh
make run-experimental
```

The experimental configuration enables a collection of features, not just the
object-controller runtime. See the chart's
[standard defaults](https://github.com/operator-framework/operator-controller/blob/main/helm/olmv1/values.yaml)
and [experimental overrides](https://github.com/operator-framework/operator-controller/blob/main/helm/experimental.yaml)
for the current settings rather than relying on a separate feature list here.
For controller responsibilities, see [OLMv1 architecture](../project/olmv1_architecture.md).
For standalone object-controller development, see
[Manage standalone ClusterObjectDeployments](../draft/tutorials/manage-clusterobjectdeployments.md).

### To Install Any Given Release

!!! warning
    Operator-Controller depends on [cert-manager](https://cert-manager.io/). Running the following command
    may affect an existing installation of cert-manager and cause cluster instability.

The latest version of Operator Controller can be installed with the following command:

```bash
curl -L -s https://github.com/operator-framework/operator-controller/releases/latest/download/install.sh | bash -s
```

### Manual Step-by-Step Installation

Run these commands from the repository root against a test cluster with
cert-manager installed and ready. The chart defaults to Linux control-plane nodes;
customize `deployments.templateSpec.nodeSelector` if the target cluster does not
have schedulable nodes with that label. The cluster must be able to pull the images
from your registry.

The chart's `namespaces.certManager.name` must match cert-manager's **cluster
resource namespace** (its `--cluster-resource-namespace` setting), which is not
necessarily the namespace of its controller Deployment. The chart defaults to
`cert-manager` and creates its CA Certificate and Secret there. If cert-manager
uses another cluster resource namespace, set `CERT_MANAGER_NAMESPACE` in step 2
accordingly; otherwise the ClusterIssuer cannot find the CA Secret and controller
certificates will not be issued.

1. Build and push the standard controller images. Use a registry namespace you can
   push to, and choose a new tag when rebuilding.

    `make docker-build` builds Linux binaries for the build host's `GOARCH` and
    does not select a container target platform. Use a build host and container
    builder whose architecture matches the cluster nodes that will run the
    controllers (for example, `amd64` or `arm64`). If they differ, run this
    workflow on a matching build host; overriding `GOARCH` alone is not enough.

    ```sh
    export IMAGE_REGISTRY='<your-registry>/<your-namespace>'
    export IMAGE_TAG='dev-1'
    export CONTAINER_RUNTIME='docker' # Use podman on Podman-only hosts.
    export OPCON_IMAGE_REPO="${OPCON_IMAGE_REPO:-$IMAGE_REGISTRY/operator-controller}"
    export CATD_IMAGE_REPO="${CATD_IMAGE_REPO:-$IMAGE_REGISTRY/catalogd}"
    make docker-build CONTAINER_RUNTIME="$CONTAINER_RUNTIME" IMAGE_REGISTRY="$IMAGE_REGISTRY" IMAGE_TAG="$IMAGE_TAG"
    "$CONTAINER_RUNTIME" push "$OPCON_IMAGE_REPO:$IMAGE_TAG"
    "$CONTAINER_RUNTIME" push "$CATD_IMAGE_REPO:$IMAGE_TAG"
    ```

    Use the same container runtime for building and pushing so both commands use
    the same local image store. Export any repository overrides in your shell
    before running these steps; passing them only as `make` arguments does not
    make them available to the push and Helm commands.

    `make docker-build` also builds object-controller. If your configuration enables
    that component, push its image with the same runtime and configure
    `options.objectController.deployment.image` in addition to the other images.
    Its build repository can be overridden by exporting `OBJECT_CONTROLLER_IMAGE_REPO`.

2. Render and apply the standard chart using your images, then wait for the
   controllers to be ready:

    ```sh
    export CERT_MANAGER_NAMESPACE='cert-manager' # Match cert-manager's cluster resource namespace.
    go tool helm template olmv1 helm/olmv1 \
      --values helm/cert-manager.yaml \
      --set-string namespaces.certManager.name="$CERT_MANAGER_NAMESPACE" \
      --set-string options.operatorController.deployment.image="$OPCON_IMAGE_REPO:$IMAGE_TAG" \
      --set-string options.catalogd.deployment.image="$CATD_IMAGE_REPO:$IMAGE_TAG" \
      > /tmp/olmv1-standard.yaml
    kubectl apply -f /tmp/olmv1-standard.yaml
    kubectl -n olmv1-system rollout status deployment/catalogd-controller-manager --timeout=180s
    kubectl -n olmv1-system rollout status deployment/operator-controller-controller-manager --timeout=180s
    ```

3. Select the samples appropriate to the installed APIs:

    ```sh
    kubectl apply -f config/samples/catalogd_operatorcatalog.yaml
    kubectl wait --for=condition=Serving clustercatalog/operatorhubio --timeout=180s
    kubectl apply -f config/samples/olm_v1_clusterextension.yaml
    ```

    Do not apply the entire `config/samples/` directory. The COD sample requires
    the experimental COD/COS CRDs and a running object-controller, neither of
    which is part of the standard installation above. Follow the
    [standalone tutorial](../draft/tutorials/manage-clusterobjectdeployments.md)
    to install that controller and exercise the COD sample. See the
    [sample guidance](https://github.com/operator-framework/operator-controller/blob/main/config/README.md)
    for prerequisites and cleanup.

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

!!! note
    Run `make help` for more information on all potential `make` targets.

## Rapid Iterative Development with Tilt

If you are developing against the combined ecosystem of catalogd + operator-controller, you will want to take advantage of `tilt`:

[Tilt](https://tilt.dev) is a tool that enables rapid iterative development of containerized workloads.

Here is an example workflow without Tilt for modifying some source code and testing those changes in a cluster:

1. Modify the source code.
2. Build the container image.
3. Either push the image to a registry or load it into your kind cluster.
4. Deploy all the appropriate Kubernetes manifests for your application.

This process can take minutes, depending on how long each step takes.

Here is the same workflow with Tilt:

1. Run `tilt up`
2. Modify the source code
3. Wait for Tilt to update the container with your changes

This ends up taking a fraction of the time, sometimes on the order of a few seconds!

### Installing Tilt

Follow Tilt's [instructions](https://docs.tilt.dev/install.html) for installation.

### Installing catalogd

operator-controller requires
[catalogd](https://github.com/operator-framework/operator-controller/tree/main/catalogd). When you give a `tilt up` invocation, catalogd will be started along with operator-controller.

### Starting Tilt

This is typically as short as:

```shell
tilt up
```

!!! note
    If you are using Podman, at least as of v4.5.1, you need to do this:

    ```shell
    DOCKER_BUILDKIT=0 tilt up
    ```

    Otherwise, you'll see an error when Tilt tries to build your image that looks similar to:

    ```text
    Build Failed: ImageBuild: stat /var/tmp/libpod_builder2384046170/build/Dockerfile: no such file or directory
    ```

When Tilt starts, you'll see something like this in your terminal:

```text
Tilt started on http://localhost:10350/
v0.33.1, built 2023-06-28

(space) to open the browser
(s) to stream logs (--stream=true)
(t) to open legacy terminal mode (--legacy=true)
(ctrl-c) to exit
```

At the end of the installation process, the command output will prompt you to press the space bar to open the web UI, which provides a useful overview of all the installed components.

Shortly after starting, Tilt processes the `Tiltfile`, resulting in:

- Building the go binaries
- Building the images
- Loading the images into kind
- Running kustomize and applying everything except the Deployments that reference the images above
- Modifying the Deployments to use the just-built images
- Creating the Deployments

### Using the Claude local development environment sub-agent

The repository contains a configuration for a Claude sub-agent that can help walk
you through the process of setting up your local development environment. If you are
using Claude, simply ask it for assistance in setting up the local development environment
and it should suggest using the local development environment specialist sub-agent. For example:

```
> Can you help me set up a local dev environment?

● I'll help you set up the local development environment for this OLM v1 project. Since this project has a specialized sub-agent for
  local development setup, I'll use that to guide you through the complete process.

---

> I need help setting up a local dev env

● I'll help you set up a local development environment for the operator-controller project. Since this involves setting up a complete
  development environment with multiple components, I'll use the specialized OLM development environment agent to guide you through the
  process.
```

The sub-agent is designed to assist with:
- Checking for all the necessary tooling and installing missing binaries
- Helping with OS-specific configuration
- Running and monitoring the kind cluster and Tilt server

As with all LLM-based tooling, always use good judgement and verify its suggestions. This is not
intended as a substitute for the concrete instructions in the rest of the project documentation.

---

## Special Setup for MacOS

Some additional setup is necessary on Macintosh computers to install and configure compatible tooling.

### Install Homebrew and tools
Follow the instructions to [install Homebrew](https://docs.brew.sh/Installation), and then execute the following command to install the required tools:

```sh
brew install bash gnu-tar gsed coreutils
```

### Configure your shell
To configure your shell, either add this to your bash or zsh profile (e.g., in $HOME/.bashrc or $HOME/.zshrc), or run the following command in the terminal:

```sh
for bindir in `find $(brew --prefix)/opt -type d -follow -name gnubin -print -maxdepth 3`
do
  export PATH=$bindir:$PATH
done
```

---

## Making code changes

Any time you change any of the files listed in the `deps` section in the `<binary name>_binary` `local_resource`,
Tilt automatically rebuilds the go binary. As soon as the binary is rebuilt, Tilt pushes it (and only it) into the
appropriate running container, and then restarts the process.

---

## Contributing

Refer to [CONTRIBUTING.md](contributing.md) for more information.
