# Derive minimal ServiceAccount required for ClusterExtension Installation and Management

!!! note "No longer applicable"
    This guide is no longer relevant. Starting with the single-tenant simplification changes,
    operator-controller runs with its own `cluster-admin` ServiceAccount and manages all extension
    lifecycle operations directly. Users no longer need to create or configure installer ServiceAccounts.

    See the [Permission Model](../concepts/permission-model.md) documentation for details on the
    current security model.
