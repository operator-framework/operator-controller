# This loads a helper function that isn't part of core Tilt that simplifies restarting the process in the container
# when files changes.
load('ext://restart_process', 'docker_build_with_restart')

# Treat the main binary as a local resource, so we can automatically rebuild it when any of the deps change. This
# builds it locally, targeting linux, so it can run in a linux container.
local_resource(
    'manager_binary',
    cmd='''
mkdir -p .tiltbuild/bin
CGO_ENABLED=0 GOOS=linux go build -o .tiltbuild/bin/manager ./cmd/manager
''',
    deps=['api', 'cmd/manager', 'internal', 'pkg', 'go.mod', 'go.sum']
)

# Configure our image build. If the file in live_update.sync (.tiltbuild/bin/manager) changes, Tilt
# copies it to the running container and restarts it.
docker_build_with_restart(
    # This has to match an image in the k8s_yaml we call below, so Tilt knows to use this image for our Deployment,
    # instead of the actual image specified in the yaml.
    ref='quay.io/operator-framework/catalogd:devel',
    # This is the `docker build` context, and because we're only copying in the binary we've already had Tilt build
    # locally, we set the context to the directory containing the binary.
    context='.tiltbuild/bin',
    # We use a slimmed-down Dockerfile that only has $binary in it.
    dockerfile_contents='''
FROM gcr.io/distroless/static:debug
EXPOSE 8080
WORKDIR /
COPY manager manager
''',
    # The set of files Tilt should include in the build. In this case, it's just the binary we built above.
    only='manager',
    # If .tiltbuild/bin/manager changes, Tilt will copy it into the running container and restart the process.
    live_update=[
        sync('.tiltbuild/bin/manager', '/manager'),
    ],
    # The command to run in the container.
    entrypoint="/manager",
)

# Tell Tilt what to deploy by running kustomize and then doing some manipulation to make things work for Tilt.
objects = decode_yaml_stream(kustomize('config/default'))
for o in objects:
    # For Tilt's live_update functionality to work, we have to run the container as root. Remove any PSA labels to allow
    # this.
    if o['kind'] == 'Namespace' and 'labels' in o['metadata']:
        labels_to_delete = [label for label in o['metadata']['labels'] if label.startswith('pod-security.kubernetes.io')]
        for label in labels_to_delete:
            o['metadata']['labels'].pop(label)

    if o['kind'] != 'Deployment':
        # We only need to modify Deployments, so we can skip this
        continue

    # For Tilt's live_update functionality to work, we have to run the container as root. Otherwise, Tilt won't
    # be able to untar the updated binary in the container's file system (this is how live update
    # works). If there are any securityContexts, remove them.
    if "securityContext" in o['spec']['template']['spec']:
        o['spec']['template']['spec'].pop('securityContext')
    for c in o['spec']['template']['spec']['containers']:
        if "securityContext" in c:
            c.pop('securityContext')

# Now apply all the yaml
k8s_yaml(encode_yaml_stream(objects))
