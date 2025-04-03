## registry+v1 bundle generation regression tests

This directory includes test cases for the rukpak/convert package based on real bundle data.
The manifests are generated and manually/visually verified for correctness.

The `generate-manifests.go` tool is used to generate the tests cases by calling convert.Convert on bundles
in the `testdata` directory. 
