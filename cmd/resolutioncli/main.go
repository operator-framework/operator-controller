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

	"github.com/operator-framework/deppy/pkg/deppy/solver"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/controllers"
	olmentity "github.com/operator-framework/operator-controller/internal/resolution/entities"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
)

const pocMessage = `This command is a proof of concept for off-cluster resolution and is not intended for production use!

Please provide your feedback and ideas via https://github.com/operator-framework/operator-controller/discussions/262`

const (
	flagNamePackageName    = "package-name"
	flagNamePackageVersion = "package-version"
	flagNamePackageChannel = "package-channel"
	flagNameIndexRef       = "index-ref"
)

var (
	scheme = runtime.NewScheme()
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
	var packageVersion string
	var packageChannel string
	var indexRef string
	flag.StringVar(&packageName, flagNamePackageName, "", "Name of the package to resolve")
	flag.StringVar(&packageVersion, flagNamePackageVersion, "", "Version of the package")
	flag.StringVar(&packageChannel, flagNamePackageChannel, "", "Channel of the package")
	// TODO: Consider adding support of multiple refs
	flag.StringVar(&indexRef, flagNameIndexRef, "", "Index reference (FBC image or dir)")
	flag.Parse()

	if err := validateFlags(packageName, indexRef); err != nil {
		fmt.Fprintln(os.Stderr, err)
		flag.Usage()
		os.Exit(1)
	}

	err := run(ctx, packageName, packageVersion, packageChannel, indexRef)
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

func run(ctx context.Context, packageName, packageVersion, packageChannel, catalogRef string) error {
	client, err := client.New(config.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	resolver := solver.NewDeppySolver(
		newIndexRefEntitySourceEntitySource(catalogRef),
		append(
			variablesources.NestedVariableSource{newPackageVariableSource(packageName, packageVersion, packageChannel)},
			controllers.NewVariableSource(client)...,
		),
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

	bundleEntity, err := getBundleEntityFromSolution(solution, packageName)
	if err != nil {
		return "", err
	}

	// Get the bundle image reference for the bundle
	bundleImage, err := bundleEntity.BundlePath()
	if err != nil {
		return "", err
	}

	return bundleImage, nil
}

func getBundleEntityFromSolution(solution *solver.Solution, packageName string) (*olmentity.BundleEntity, error) {
	for _, variable := range solution.SelectedVariables() {
		switch v := variable.(type) {
		case *olmvariables.BundleVariable:
			entityPkgName, err := v.BundleEntity().PackageName()
			if err != nil {
				return nil, err
			}
			if packageName == entityPkgName {
				return v.BundleEntity(), nil
			}
		}
	}
	return nil, fmt.Errorf("entity for package %q not found in solution", packageName)
}
