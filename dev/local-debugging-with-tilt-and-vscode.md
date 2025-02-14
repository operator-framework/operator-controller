# Local Debugging in VSCode with Tilt

This tutorial will show you how to connect the go debugger in VSCode to your running
kind cluster with Tilt for live debugging.

* Follow the instructions in [this document](podman/setup-local-env-podman.md) to set up your local kind cluster and image registry.
* Next, execute `tilt up` to start the Tilt service (if using podman, you might need to run `DOCKER_BUILDKIT=0 tilt up`).

Press space to open the web UI where you can monitor the current status of operator-controller and catalogd inside Tilt.

Create a `launch.json` file in your operator-controller repository if you do not already have one.
Add the following configurations:

```json
{
    "version": "0.2.0",
    "configurations": [
        {
            "name": "Debug operator-controller via Tilt",
            "type": "go",
            "request": "attach",
            "mode": "remote",
            "port": 30000,
            "host": "localhost",
            "cwd": "${workspaceFolder}",
            "trace": "verbose"
        },
        {
            "name": "Debug catalogd via Tilt",
            "type": "go",
            "request": "attach",
            "mode": "remote",
            "port": 20000,
            "host": "localhost",
            "cwd": "${workspaceFolder}",
            "trace": "verbose"
        },
    ]
}
```

This creates two "Run and debug" entries in the Debug panel of VSCode.

Now you can start either debug configuration depending on which component you want to debug.
VSCode will connect the debugger to the port exposed by Tilt.

Breakpoints should now be fully functional. The debugger can even maintain its
connection through live code updates.