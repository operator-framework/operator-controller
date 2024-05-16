## The following are Podman specific steps used to set up on a MacBook (Intel or Apple Silicon)

### Verify installed tools (install if needed)

```sh
$ podman --version
podman version 5.0.1
$ kind version
kind v0.23.0 go1.22.3 darwin/arm64

(optional)
$ tilt version
v0.33.12, built 2024-03-28
```

### Start Kind with a local registry
Use this [helper script](./kind-with-registry-podman.sh) to create a local single-node Kind cluster with an attached local image registry.

#### Disable secure access on the local kind registry:

`podman inspect kind-registry --format '{{.NetworkSettings.Ports}}'`

With the port you find for 127.0.0.1 edit the Podman machine's config file:

`podman machine ssh`

`sudo vi /etc/containers/registries.conf.d/100-kind.conf`

Should look like:

```ini
[[registry]]
location = "localhost:5001"
insecure = true
```

### export DOCKER_HOST

`export DOCKER_HOST=unix:///var/run/docker.sock`


### Optional - Start tilt with the tilt file in the parent directory

`DOCKER_BUILDKIT=0 tilt up`

### Optional troubleshooting

In some cases it may be needed to do
```
sudo podman-mac-helper install
```
```
podman machine stop/start
```
