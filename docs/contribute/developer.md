
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

This will build a local container image of the operator-controller, create a new KIND cluster and then deploy onto that cluster. This will also deploy the catalogd and cert-manager dependencies.

### To Install Any Given Release

!!! warning
    Operator-Controller depends on [cert-manager](https://cert-manager.io/). Running the following command
    may affect an existing installation of cert-manager and cause cluster instability.

The latest version of Operator Controller can be installed with the following command:

```bash
curl -L -s https://github.com/operator-framework/operator-controller/releases/latest/download/install.sh | bash -s
```

### Manual Step-by-Step Installation
1. Install Instances of Custom Resources:

    ```sh
    kubectl apply -f config/samples/
    ```

2. Build and push your image to the location specified by `IMG`:

    ```sh
    make docker-build docker-push IMG=<some-registry>/operator-controller:tag
    ```

3. Deploy the controller to the cluster with the image specified by `IMG`:

    ```sh
    make deploy IMG=<some-registry>/operator-controller:tag
    ```

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
