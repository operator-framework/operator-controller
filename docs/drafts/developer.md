
## Getting Started

You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.

> [!NOTE]
> Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).
> 
> If you are on MacOS, see [Special Setup for MacOS](#special-setup-for-macos).

### Test It Out

Install the CRDs and the operator-controller into a new [KIND cluster](https://kind.sigs.k8s.io/):

```sh
make run
```

This will build a local container image of the operator-controller, create a new KIND cluster and then deploy onto that cluster. This will also deploy the catalogd and cert-manager dependencies.

### Installation

> [!CAUTION]  
> Operator-Controller depends on [cert-manager](https://cert-manager.io/). Running the following command
> may affect an existing installation of cert-manager and cause cluster instability.

The latest version of Operator Controller can be installed with the following command:

```bash
curl -L -s https://github.com/operator-framework/operator-controller/releases/latest/download/install.sh | bash -s
```

### Running on the cluster -- the targets `make run` combines
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

### Uninstall CRDs
To delete the CRDs from the cluster:

```sh
make uninstall
```

### Undeploy controller
To undeploy the controller from the cluster:

```sh
make undeploy
```

---

## Iterative Development

If you are making code changes and want to iterate on them quickly, there are specific targets for this purpose.

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make help` for more information on all potential `make` targets.

### Rapid Iterative Development with Tilt

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
[catalogd](https://github.com/operator-framework/catalogd). Please make sure it's installed, either normally or via their own Tiltfiles, before proceeding. If you want to use Tilt, make sure you specify a unique `--port` flag to each `tilt up` invocation.

### Install tilt-support Repo

You must install the tilt-support repo at the directory level above this repo:

```bash
pushd ..
git clone https://github.com/operator-framework/tilt-support
popd
```

### Starting Tilt

This is typically as short as:

```shell
tilt up
```

**NOTE:** if you are using Podman, at least as of v4.5.1, you need to do this:

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

Typically, you'll want to press the space bar to have it open the UI in your web browser.

---

## Special Setup for MacOS

Some additional setup is necessary on Macintosh computers to install and configure compatible tooling.

### Install Homebrew and tools
Follow the instructions to [installing Homebrew](https://docs.brew.sh/Installation) and then execute the following to install tools:

```sh
brew install bash gnu-tar gsed
```

### Configure your shell
Modify your login shell's `PATH` to prefer the new tools over those in the existing environment. This example should work either with `zsh` (in $HOME/.zshrc) or `bash` (in $HOME/.bashrc):

```sh
for bindir in `find $(brew --prefix)/opt -type d -follow -name gnubin -print`
do
  export PATH=$bindir:$PATH
done
```

---

## How it works
This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/).

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/)
which provide a reconcile function responsible for synchronizing resources until the desired state is reached on the cluster.

---

## Contributing

Refer to [CONTRIBUTING.md](./CONTRIBUTING.md) for more information.
