package variablesources

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	masterminds "github.com/Masterminds/semver/v3"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/resolution/v2/store"
	"github.com/operator-framework/operator-controller/internal/resolution/v2/variables"
)

var _ input.VariableSource = &Operator{}

type Operator struct {
	client.Client
	store.Store
}

func (c Operator) GetVariables(ctx context.Context, _ input.EntitySource) ([]deppy.Variable, error) {
	catalogs, err := c.GetCatalogs(ctx)
	if err != nil {
		return nil, err
	}

	installedBundles, err := c.GetInstalledBundles(ctx)
	if err != nil {
		return nil, err
	}

	cluster, err := c.GetCluster(ctx)
	if err != nil {
		return nil, err
	}

	var opList operatorsv1alpha1.OperatorList
	if err := c.List(ctx, &opList); err != nil {
		return nil, err
	}
	vars := make([]deppy.Variable, 0, len(opList.Items))

	for _, op := range opList.Items {
		opVarID := deppy.IdentifierFromString("Operator/" + op.Name)
		pkgs := make([]*store.Package, 0)
		for _, c := range catalogs {
			catPkg := c.Packages[op.Spec.PackageName]
			if c.Packages[op.Spec.PackageName] != nil {
				pkgs = append(pkgs, catPkg)
			}
		}
		if len(pkgs) == 0 {
			vars = append(vars, input.NewSimpleVariable(opVarID,
				constraint.Mandatory(),
				constraint.NewUserFriendlyConstraint(constraint.Prohibited(),
					func(constraint deppy.Constraint, subject deppy.Identifier) string {
						return fmt.Sprintf("Package %q not found.", op.Spec.PackageName)
					},
				)),
			)
			continue
		}

		allBundles := make([]*store.Bundle, 0)
		for _, pkg := range pkgs {
			for _, bundle := range pkg.Bundles {
				allBundles = append(allBundles, bundle)
			}
		}

		notFoundMessage := fmt.Sprintf("no bundles found from package %q", op.Spec.PackageName)
		if len(allBundles) == 0 {
			vars = append(vars, input.NewSimpleVariable(opVarID,
				constraint.Mandatory(),
				constraint.NewUserFriendlyConstraint(constraint.Prohibited(),
					func(constraint deppy.Constraint, subject deppy.Identifier) string {
						return notFoundMessage
					},
				)),
			)
			continue
		}

		sort.Slice(allBundles, func(i, j int) bool {
			return allBundles[i].Version.GreaterThan(&allBundles[j].Version)
		})

		if op.Spec.Channel != "" {
			notFoundMessage = fmt.Sprintf("%s, channel %q", notFoundMessage, op.Spec.Channel)
			chBundles := allBundles[:0]
			for _, pkg := range pkgs {
				for _, ch := range pkg.Channels {
					if ch.Name == op.Spec.Channel {
						chBundles = append(chBundles, ch.Bundles...)
					}
				}
			}
			if len(chBundles) == 0 {
				vars = append(vars, input.NewSimpleVariable(opVarID,
					constraint.Mandatory(),
					constraint.NewUserFriendlyConstraint(constraint.Prohibited(),
						func(constraint deppy.Constraint, subject deppy.Identifier) string {
							return notFoundMessage
						},
					)),
				)
				continue
			}
			allBundles = chBundles
		}

		if op.Spec.Version != "" {
			notFoundMessage = fmt.Sprintf("%s, in version range %q", notFoundMessage, op.Spec.Version)

			verBundles := allBundles[:0]
			verConstraint, err := masterminds.NewConstraint(op.Spec.Version)
			if err != nil {
				return nil, err
			}
			for _, bundle := range allBundles {
				if verConstraint.Check(&bundle.Version) {
					verBundles = append(verBundles, bundle)
				}
			}
			if len(verBundles) == 0 {
				vars = append(vars, input.NewSimpleVariable(opVarID,
					constraint.Mandatory(),
					constraint.NewUserFriendlyConstraint(constraint.Prohibited(),
						func(constraint deppy.Constraint, subject deppy.Identifier) string {
							return notFoundMessage
						},
					),
				))
				continue
			}
			allBundles = verBundles
		}

		if op.Spec.UpgradeEdgeConstraintPolicy == operatorsv1alpha1.UpgradeEdgeConstraintPolicyEnforce {
			installedBundle := installedBundles[op.Spec.PackageName]
			if installedBundle != nil {
				successorRange := fmt.Sprintf("^%s", installedBundle.Version)
				notFoundMessage = fmt.Sprintf("%s, in version range %q as a successor to currently installed version %q", notFoundMessage, successorRange, installedBundle.Version)

				verBundles := allBundles[:0]
				verConstraint, err := masterminds.NewConstraint(successorRange)
				if err != nil {
					return nil, err
				}
				for _, bundle := range allBundles {
					if verConstraint.Check(&bundle.Version) {
						verBundles = append(verBundles, bundle)
					}
				}
				if len(verBundles) == 0 {
					vars = append(vars, input.NewSimpleVariable(opVarID,
						constraint.Mandatory(),
						constraint.NewUserFriendlyConstraint(constraint.Prohibited(),
							func(constraint deppy.Constraint, subject deppy.Identifier) string {
								return notFoundMessage
							},
						)),
					)
					continue
				}
				allBundles = verBundles
			}
		}

		if op.Spec.ClusterConstraintPolicy == operatorsv1alpha1.ClusterConstraintPolicyEnforce {
			compatibleWithClusterBundles := allBundles[:0]
			for _, b := range allBundles {
				k8sVersionRange := "*"
				minNodeCount := 0
				for _, p := range b.Properties {
					switch p.Type {
					case "olm.kubernetesVersionRange":
						var v string
						if err := json.Unmarshal(p.Value, &v); err != nil {
							return nil, err
						}
						k8sVersionRange = v
					case "olm.minimumNodeCount":
						var v int
						if err := json.Unmarshal(p.Value, &v); err != nil {
							return nil, err
						}
						minNodeCount = v
					}
				}
				clusterVersion, err := masterminds.StrictNewVersion(strings.TrimPrefix(cluster.VersionInfo.GitVersion, "v"))
				if err != nil {
					return nil, err
				}
				requiredRangeConstraint, err := masterminds.NewConstraint(k8sVersionRange)
				if err != nil {
					return nil, err
				}
				if requiredRangeConstraint.Check(clusterVersion) && minNodeCount <= len(cluster.Nodes) {
					compatibleWithClusterBundles = append(compatibleWithClusterBundles, b)
				}
			}
			notFoundMessage = fmt.Sprintf("%s, compatible with cluster version %q with %d nodes", notFoundMessage, cluster.VersionInfo.GitVersion, len(cluster.Nodes))
			if len(compatibleWithClusterBundles) == 0 {
				vars = append(vars, input.NewSimpleVariable(opVarID,
					constraint.Mandatory(),
					constraint.NewUserFriendlyConstraint(constraint.Prohibited(),
						func(constraint deppy.Constraint, subject deppy.Identifier) string {
							return notFoundMessage
						},
					)),
				)
				continue
			}
			allBundles = compatibleWithClusterBundles
		}

		opDependencies := make([]deppy.Identifier, 0, len(allBundles))
		for _, b := range allBundles {
			bundleID := deppy.IdentifierFromString(fmt.Sprintf("Catalog/%s/%s/%s/%s", b.CatalogName, b.Schema, b.Package, b.Name))
			vars = append(vars, variables.NewBundle(bundleID, b))
			opDependencies = append(opDependencies, bundleID)
		}
		vars = append(vars, input.NewSimpleVariable(opVarID, constraint.Mandatory(), constraint.Dependency(opDependencies...)))
	}
	return vars, nil
}
