# Configuring Podman for Tilt

The following tutorial explains how to set up a local development environment using Podman and Tilt on a Linux host.
A few notes on achieving the same result for MacOS are included at the end, but you will likely need to do some
tinkering on your own.

## Verify installed tools (install if needed)

Ensure you have installed [Podman](https://podman.io/), [Kind](https://github.com/kubernetes-sigs/kind/), and [Tilt](https://tilt.dev/).

```sh
$ podman --version
podman version 5.0.1
$ kind version
kind v0.26.0 go1.23.4 linux/amd64
$ tilt version
v0.33.15, built 2024-05-31
```

## Start Kind with a local registry

Use this [helper script](./kind-with-registry-podman.sh) to create a local single-node Kind cluster with an attached local image registry.


## Disable secure access on the local kind registry:

Verify the port used by the image registry:

```sh
podman inspect kind-registry --format '{{.NetworkSettings.Ports}}'
```

Edit `/etc/containers/registries.conf.d/100-kind.conf` so it contains the following, substituting 5001 if your registry is using a different port:

```ini
[[registry]]
location = "localhost:5001"
insecure = true
```

## Configure the Podman socket

Tilt needs to connect to the Podman socket to initiate image builds. The socket address can differ
depending on your host OS and whether you want to use rootful or rootless Podman. If you're not sure,
you should use rootless.

You can start the rootless Podman socket by running `podman --user start podman.socket`.
If you would like to automatically start the socket in your user session, you can run
`systemctl --user enable --now podman.socket`.

Find the location of your user socket with `systemctl --user status podman.socket`:

```sh
● podman.socket - Podman API Socket
     Loaded: loaded (/usr/lib/systemd/user/podman.socket; enabled; preset: disabled)
     Active: active (listening) since Tue 2025-01-28 11:40:50 CST; 7s ago
 Invocation: d9604e587f2a4581bc79cbe4efe9c7e7
   Triggers: ● podman.service
       Docs: man:podman-system-service(1)
     Listen: /run/user/1000/podman/podman.sock (Stream)
     CGroup: /user.slice/user-1000.slice/user@1000.service/app.slice/podman.socket
```

The location of the socket is shown in the `Listen` section, which in the example above
is `/run/user/1000/podman/podman.sock`.

Set `DOCKER_HOST` to a unix address at the socket location:

```sh
export DOCKER_HOST=unix:///run/user/1000/podman/podman.sock
```

Some systems might symlink the Podman socket to a docker socket, in which case
you might need to try something like:

```sh
export DOCKER_HOST=unix:///var/run/docker.sock
```

## Start Tilt

Running Tilt with a container engine other than Docker requires setting `DOCKER_BUILDKIT=0`.
You can export this, or just run:

```sh
DOCKER_BUILDKIT=0 tilt up
```

## MacOS Troubleshooting

The instructions above are written for use on a Linux system. You should be able to create
the same or a similar configuration on MacOS, but specific steps will differ.

In some cases you might need to run: 

```sh
sudo podman-mac-helper install

podman machine stop/start
```

When disabling secure access to the registry, you will need to first enter the Podman virtual machine:
`podman machine ssh`
