# Public API
The public API of OLM v1 is as follows:

- Kubernetes APIs. For more information on these APIs, see:
    - [OLMv1 API reference](../api-reference/olmv1-api-reference.md)
- `Catalogd` web server. For more information on what this includes, see the [catalogd web server documentation](../api-reference/catalogd-webserver.md)

!!! warning

    Only the stable APIs described in the above references carry SemVer
    compatibility guarantees. Anything not mentioned in those references is prone
    to breaking changes.

## Experimental APIs and fields

The API reference documents both standard and experimental functionality. Inclusion
in that reference does not make an experimental API or field stable, even when it
uses the `olm.operatorframework.io/v1` API version.

Experimental APIs, including `ClusterObjectDeployment` and `ClusterObjectSet`, and
fields marked as experimental may change incompatibly between releases. Migration
support is not provided for experimental API changes. Stable APIs retain their
compatibility guarantees, including for supported production upgrades and migrations.
