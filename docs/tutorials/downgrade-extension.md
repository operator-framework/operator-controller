---
hide:
  - toc
---

# Downgrade a ClusterExtension

## Introduction

Downgrading a `ClusterExtension` involves reverting the extension to a previously available version. This process may be necessary due to compatibility issues, unexpected behavior in the newer version, or specific feature requirements only available in an earlier release. However, downgrading carries inherent risks, such as potential data loss, issues with new CRD versions, and possible breakage of clients that rely on the newer version. Users should carefully consider these risks and be confident in their decision to proceed with the downgrade. This guide provides step-by-step instructions for performing a downgrade, including overrides to bypass default constraints and disable CRD safety checks.

## Prerequisites

Before initiating the downgrade process, ensure the following prerequisites are met:

- **Backup Configurations:** Always back up your current configurations and data to prevent potential loss during the downgrade.
- **Access Rights:** Ensure you have the necessary permissions to modify `ClusterExtension` resources and perform administrative tasks.
- **Version Availability:** Verify that the target downgrade version is available in your catalogs.
- **Compatibility Check:** Ensure that the target version is compatible with your current system and other dependencies.

## Steps to Downgrade

### 1. Disabling the CRD Upgrade Safety Check

Custom Resource Definitions (CRDs) ensure that the resources used by the `ClusterExtension` are valid and consistent. During a downgrade, the CRD Upgrade Safety check might prevent reverting to an incompatible version. Disabling the CRD Upgrade Safety check allows the downgrade to proceed without these validations.

**Disable CRD Safety Check Configuration:**

Add the `crdUpgradeSafety` field and set its `enforcement` to `None` in the `ClusterExtension` resource. This configuration disables CRD safety checks during the downgrade process.

**Example:**

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: example-extension
spec:
  namespace: argocd
  serviceAccount:
    name: argocd-installer
  install:
    preflight:
      crdUpgradeSafety:
        enforcement: None
  source:
    sourceType: Catalog
    catalog:
      packageName: argocd-operator
      version: 0.6.0
```

**Command Example:**

If you prefer using the command line, you can use `kubectl` to modify the upgrade CRD safety check configuration.

```bash
kubectl patch clusterextension example-extension --patch '{"spec":{"install":{"preflight":{"crdUpgradeSafety":{"enforcement":"None"}}}}}' --type=merge
```

### 2. Ignoring Catalog Provided Upgrade Constraints

By default, Operator Lifecycle Manager (OLM) enforces upgrade constraints based on semantic versioning and catalog definitions. To allow downgrades, you need to override these constraints.

**Override Configuration:**

Set the `upgradeConstraintPolicy` to `SelfCertified` in the `ClusterExtension` resource. This configuration permits downgrades, sidegrades, and any version changes without adhering to the predefined upgrade paths.

**Example:**

```yaml
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: example-extension
spec:
  namespace: argocd
  serviceAccount:
    name: argocd-installer
  install:
    preflight:
      crdUpgradeSafety:
        enforcement: None
  source:
    sourceType: Catalog
    catalog:
      packageName: argocd-operator
      version: 0.6.0
      upgradeConstraintPolicy: SelfCertified
```

**Command Example:**

If you prefer using the command line, you can use `kubectl` to modify the upgrade constraint policy.

```bash
kubectl patch clusterextension example-extension --patch '{"spec":{"source": {"catalog":{"upgradeConstraintPolicy":"SelfCertified"}}}}' --type=merge
```

### 3. Executing the Downgrade

Once the CRD safety checks are disabled and upgrade constraints are set, you can proceed with the actual downgrade.

1. **Edit the ClusterExtension Resource:**

    Modify the `ClusterExtension` custom resource to specify the target version and adjust the upgrade constraints.

    ```bash
    kubectl edit clusterextension example-extension
    ```

2. **Update the Version:**

    Within the YAML editor, update the `spec` section as follows:

    ```yaml
    apiVersion: olm.operatorframework.io/v1
    kind: ClusterExtension
    metadata:
      name: example-extension
    spec:
      namespace: argocd
      serviceAccount:
        name: argocd-installer
      install:
        preflight:
          crdUpgradeSafety:
            enforcement: None
      source:
        sourceType: Catalog
        catalog:
          packageName: argocd-operator
          version: <target_version>
          upgradeConstraintPolicy: SelfCertified
    ```

    `target_version`
    : Specify the target version you wish to downgrade to.

3. **Apply the Changes:**

    Save and exit the editor. Kubernetes will apply the changes and initiate the downgrade process.

### 4. Post-Downgrade Verification

After completing the downgrade, verify that the `ClusterExtension` is functioning as expected.

**Verification Steps:**

1. **Check the Status of the ClusterExtension:**

    ```bash
    kubectl get clusterextension example-extension -o yaml
    ```

    Ensure that the `status` reflects the target version and that there are no error messages.

2. **Validate CRD Integrity:**

    Confirm that all CRDs associated with the `ClusterExtension` are correctly installed and compatible with the downgraded version.

    ```bash
    kubectl get crd | grep <extension_crd>
    ```

3. **Test Extension Functionality:**

    Perform functional tests to ensure that the extension operates correctly in its downgraded state.

4. **Monitor Logs:**

    Check the logs of the operator managing the `ClusterExtension` for any warnings or errors.

    ```bash
    kubectl logs deployment/<operator_deployment> -n <operator_namespace>
    ```

## Troubleshooting

During the downgrade process, you might encounter issues. Below are common problems and their solutions:

### Downgrade Fails Due to Version Constraints

**Solution:**

- Ensure that the `upgradeConstraintPolicy` is set to `SelfCertified`.
- Verify that the target version exists in the catalog.
- Check for typos or incorrect version numbers in the configuration.

### CRD Compatibility Issues

**Solution:**

- Review the changes in CRDs between versions to ensure compatibility.
- If disabling the CRD safety check, ensure that the downgraded version can handle the existing CRDs without conflicts.
- Consider manually reverting CRDs if necessary, but proceed with caution to avoid data loss.

### Extension Becomes Unresponsive After Downgrade

**Solution:**

- Restore from the backup taken before the downgrade.
- Investigate logs for errors related to the downgraded version.
- Verify that all dependencies required by the downgraded version are satisfied.

## Additional Resources

- [Semantic Versioning Specification](https://semver.org/)
- [Manually Verified Upgrades and Downgrades](https://github.com/operator-framework/operator-controller/blob/main/docs/drafts/upgrade-support.md#manually-verified-upgrades-and-downgrades)
