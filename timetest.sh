#!/bin/bash

## Create the sample CatalogSource CR
kubectl apply -f config/samples/rukpak_catalogsource.yaml

## Loop until all packages are created (170)
packages=0

until [ $packages -eq 170 ]
do
    packages=$(kubectl get packages | wc -l)
done

## Loop until all the bundlemetadata are created (1301)
bundles=0

until [ $bundles -eq 1301 ]
do
    bundles=$(kubectl get bundlemetadata | wc -l)
done

echo "all child resources created!"