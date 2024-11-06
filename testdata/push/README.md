# Test Registry Image Push

This tool builds our test bundle and catalog images via crane. It accepts two command line arguments:
```
Usage of push:
      --images-path string        Image directory path (default "/images")
      --registry-address string   The address of the registry. (default ":12345")
```

`--registry-address` is the address of the registry to be pushed to.

`--images-path` should point to the root directory of the images tree structure. The tool expects a particular directory format in order to work properly. Bundles should be placed in `<images-path>/bundles`, and catalogs in `<images-path>/catalogs`. From these directories the same convention should be followed: folders within `[catalogs|bundles]` are image names i.e. `test-catalog`. Within these folders is where each tag for that image should be placed. What that ends up looking like is:
```bash
$ tree ./testdata/images/
./testdata/images/
├── bundles
│   └── prometheus-operator
│       └── v1.0.0
│           ├── metadata
│           │   └── annotations.yaml
│           └── manifests
│               └── example.yaml
└── catalogs
    └── test-catalog
        ├── v1
        │   └── configs
        │       └── catalog.yaml
        └── v2
            └── configs
                └── catalog.yaml
```
The inside of each tag folder will be placed directly into `/` of the built container i.e. `test-catalog:v1` will have `/configs/catalog.yaml`. 

To add a new image or tag for the tool, create the folders required and populate them with the files to be mounted. Bundle images requiring metadata should contain a `metadata` folder with `annotations.yaml`. Example content:
```yaml
annotations:
  # Core bundle annotations.
  operators.operatorframework.io.bundle.mediatype.v1: registry+v1
  operators.operatorframework.io.bundle.manifests.v1: manifests/
  operators.operatorframework.io.bundle.metadata.v1: metadata/
  operators.operatorframework.io.bundle.package.v1: prometheus
  operators.operatorframework.io.bundle.channels.v1: beta
  operators.operatorframework.io.metrics.builder: operator-sdk-v1.28.0
  operators.operatorframework.io.metrics.mediatype.v1: metrics+v1
  operators.operatorframework.io.metrics.project_layout: unknown
```
