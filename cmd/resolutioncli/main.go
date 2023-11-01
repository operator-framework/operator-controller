/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/deppy/pkg/deppy/solver"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	"github.com/operator-framework/operator-controller/internal/controllers"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

const pocMessage = `This command is a proof of concept for off-cluster resolution and is not intended for production use!

Please provide your feedback and ideas via https://github.com/operator-framework/operator-controller/discussions/262`

const (
	flagNamePackageName         = "package-name"
	flagNamePackageVersionRange = "package-version"
	flagNamePackageChannel      = "package-channel"
	flagNameIndexRef            = "index-ref"
	flagNameInputDir            = "input-dir"
)

var (
	scheme = runtime.NewScheme()

	codecs = serializer.NewCodecFactory(scheme)
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(operatorsv1alpha1.AddToScheme(scheme))
	utilruntime.Must(rukpakv1alpha1.AddToScheme(scheme))
	utilruntime.Must(catalogd.AddToScheme(scheme))
}

func main() {
	fmt.Fprintf(os.Stderr, "\033[0;31m%s\033[0m\n", pocMessage)

	ctx := context.Background()

	var packageName string
	var packageVersionRange string
	var packageChannel string
	var indexRef string
	var inputDir string
	flag.StringVar(&packageName, flagNamePackageName, "", "Name of the package to resolve")
	flag.StringVar(&packageVersionRange, flagNamePackageVersionRange, "", "Version or version range of the package")
	flag.StringVar(&packageChannel, flagNamePackageChannel, "", "Channel of the package")
	// TODO: Consider adding support of multiple refs
	flag.StringVar(&indexRef, flagNameIndexRef, "", "Index reference (FBC image or dir)")
	flag.StringVar(&inputDir, flagNameInputDir, "", "Directory containing Kubernetes manifests (such as Operator) to be used as an input for resolution")
	flag.Parse()

	if err := validateFlags(packageName, indexRef); err != nil {
		fmt.Fprintln(os.Stderr, err)
		flag.Usage()
		os.Exit(1)
	}

	err := run(ctx, packageName, packageChannel, packageVersionRange, indexRef, inputDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func validateFlags(packageName, indexRef string) error {
	if packageName == "" {
		return fmt.Errorf("missing required -%s flag", flagNamePackageName)
	}

	if indexRef == "" {
		return fmt.Errorf("missing required -%s flag", flagNameIndexRef)
	}

	return nil
}

func run(ctx context.Context, packageName, packageChannel, packageVersionRange, indexRef, inputDir string) error {
	// Using the fake Kubernetes client and creating objects
	// in it from manifests & CLI flags is fine for PoC.
	// But when/if we decide to proceed with CLI/offline resolution
	// we will need to come up with a better way to create inputs
	// for resolver when working with CLI.
	//
	// We will need to think about multiple types of inputs:
	//   - How to read required package (what we want to install/update)
	//   - How to read bundles from multiple catalogs
	//   - How to take into account cluster information. Some package
	//     will have constraints like "need Kubernetes version to be >= X"
	//     or "need >= 3 worker nodes").
	clientBuilder := fake.NewClientBuilder().WithScheme(scheme)

	if inputDir != "" {
		objects, err := readManifestFiles(inputDir)
		if err != nil {
			return err
		}

		clientBuilder.WithRuntimeObjects(objects...)
	}

	clientBuilder.WithRuntimeObjects(&operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{
			Name: "resolutioncli-input-operator",
		},
		Spec: operatorsv1alpha1.OperatorSpec{
			PackageName: packageName,
			Channel:     packageChannel,
			Version:     packageVersionRange,
		},
	})

	cl := clientBuilder.Build()
	catalogClient := newIndexRefClient(indexRef)

	resolver := solver.NewDeppySolver(
		controllers.NewVariableSource(cl, catalogClient),
	)

	bundleImage, err := resolve(ctx, resolver, packageName)
	if err != nil {
		return err
	}

	fmt.Println(bundleImage)
	return nil
}

func resolve(ctx context.Context, resolver *solver.DeppySolver, packageName string) (string, error) {
	solution, err := resolver.Solve(ctx)
	if err != nil {
		return "", err
	}

	bundle, err := bundleFromSolution(solution, packageName)
	if err != nil {
		return "", err
	}

	// Get the bundle image reference for the bundle
	return bundle.Image, nil
}

func bundleFromSolution(solution *solver.Solution, packageName string) (*catalogmetadata.Bundle, error) {
	for _, variable := range solution.SelectedVariables() {
		switch v := variable.(type) {
		case *olmvariables.BundleVariable:
			bundlePkgName := v.Bundle().Package
			if packageName == bundlePkgName {
				return v.Bundle(), nil
			}
		}
	}
	return nil, fmt.Errorf("bundle for package %q not found in solution", packageName)
}
