package controllers

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/blang/semver/v4"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/operator-framework/operator-registry/v2/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/resolve"
	imageutil "github.com/operator-framework/operator-controller/internal/shared/util/image"
)

// TestSource is an in-memory policy, not a production API source. It provides
// no deprecation evidence: sources without deprecation data are reported
// exactly like sources whose evaluation found none.
type TestSource struct {
	result   *RevisionMetadata
	err      error
	maintain bool
	enqueue  bool
}

func (s *TestSource) ValidateSource(*ocv1.ClusterExtension) error {
	return nil
}

func (s *TestSource) Resolve(context.Context, *ocv1.ClusterExtension, *ocv1.BundleMetadata) (*RevisionMetadata, error) {
	return s.result, s.err
}

func (s *TestSource) RecoverFromResolutionError(_ context.Context, _ *ocv1.ClusterExtension, _ *RevisionStates, resolveErr error) (*RevisionMetadata, error) {
	return nil, resolveErr
}

func (s *TestSource) ReconcileOnCatalogChange(*ocv1.ClusterExtension, *ocv1.ClusterCatalog) bool {
	return s.enqueue
}

func (s *TestSource) CanMaintainWithoutContent(*ocv1.ClusterExtension, *RevisionStates, *RevisionMetadata) bool {
	return s.maintain
}

// deprecatingResolver is a resolver fake that also provides deprecation
// evidence, standing in for *resolve.CatalogResolver.
type deprecatingResolver struct {
	resolve.Func
	deprecation *declcfg.Deprecation
	queryErr    error
}

func (r deprecatingResolver) GetPackageDeprecation(context.Context, *ocv1.ClusterExtension) (*declcfg.Deprecation, error) {
	return r.deprecation, r.queryErr
}

func (r deprecatingResolver) GetChannelDeprecations(context.Context, *ocv1.ClusterExtension) (*declcfg.Deprecation, error) {
	return r.deprecation, r.queryErr
}

func (r deprecatingResolver) GetBundleDeprecations(context.Context, *ocv1.ClusterExtension, ...ocv1.BundleMetadata) (*declcfg.Deprecation, error) {
	return r.deprecation, r.queryErr
}

type forbiddenCatalogClient struct{ client.Client }

func (*forbiddenCatalogClient) List(context.Context, client.ObjectList, ...client.ListOption) error {
	panic("source isolation violated: catalog client used")
}

type successfulSourceApplier struct{}

func (successfulSourceApplier) Apply(context.Context, fs.FS, *ocv1.ClusterExtension, map[string]string, map[string]string) (bool, string, error) {
	return true, "", nil
}

func sourceExtension(sourceType string) *ocv1.ClusterExtension {
	ext := &ocv1.ClusterExtension{Spec: ocv1.ClusterExtensionSpec{Source: ocv1.SourceConfig{
		SourceType: sourceType,
	}}}
	if sourceType == ocv1.SourceTypeCatalog {
		ext.Spec.Source.Catalog = &ocv1.CatalogFilter{PackageName: "example"}
	}
	return ext
}

func sourceRevision(name, version string) *RevisionMetadata {
	return &RevisionMetadata{Package: "example", Image: "example/bundle:latest", BundleMetadata: ocv1.BundleMetadata{Name: name, Version: version}}
}

func forbiddenCatalogResolver(context.Context, *ocv1.ClusterExtension, *ocv1.BundleMetadata) (*declcfg.Bundle, *declcfg.VersionRelease, error) {
	panic("source isolation violated: catalog resolver used")
}

// TestSourceWithoutDeprecationDataManifestsAsNotDeprecated covers the contract
// for sources that implement none of the deprecation interfaces: absence of
// the capability manifests exactly like an evaluation that found no
// deprecations, on success and on resolution failure alike.
func TestSourceWithoutDeprecationDataManifestsAsNotDeprecated(t *testing.T) {
	resolutionErr := errors.New("test source failed")
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "success"},
		{name: "resolution error", err: resolutionErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			installed := sourceRevision("old", "1.0.0")
			state := &reconcileState{revisionStates: &RevisionStates{Installed: installed}}
			source := &TestSource{result: sourceRevision("selected", "2.0.0"), err: tc.err}
			ext := sourceExtension("TestSource")
			_, err := ResolveBundle(Resolvers{"TestSource": source})(t.Context(), state, ext)
			require.ErrorIs(t, err, tc.err)
			for _, condType := range []string{ocv1.TypeDeprecated, ocv1.TypePackageDeprecated, ocv1.TypeChannelDeprecated, ocv1.TypeBundleDeprecated} {
				condition := apimeta.FindStatusCondition(ext.Status.Conditions, condType)
				require.NotNil(t, condition)
				require.Equal(t, metav1.ConditionFalse, condition.Status)
				require.Equal(t, ocv1.ReasonNotDeprecated, condition.Reason)
			}
		})
	}
}

func TestSourceSelectionOwnsResolution(t *testing.T) {
	resolutionErr := errors.New("test source failed")
	for _, rolling := range []bool{false, true} {
		for _, sourceErr := range []error{nil, resolutionErr, reconcile.TerminalError(resolutionErr)} {
			t.Run(testingName(rolling, sourceErr), func(t *testing.T) {
				installed := sourceRevision("old", "1.0.0")
				target := sourceRevision("selected", "2.0.0")
				rollingRevision := sourceRevision("rolling", "1.1.0")
				state := &reconcileState{revisionStates: &RevisionStates{Installed: installed}}
				if rolling {
					state.revisionStates.RollingOut = []*RevisionMetadata{rollingRevision}
				}
				source := &TestSource{result: target, err: sourceErr}
				sources := Resolvers{
					"TestSource": source,
					ocv1.SourceTypeCatalog: &CatalogSource{
						Resolver: resolve.Func(forbiddenCatalogResolver), Client: &forbiddenCatalogClient{},
					},
				}
				ext := sourceExtension("TestSource")
				_, err := ResolveBundle(sources)(t.Context(), state, ext)
				if rolling {
					// Rollout continuation is selected without consulting resolution.
					require.NoError(t, err)
					require.Same(t, rollingRevision, state.resolvedRevisionMetadata)
					return
				}
				require.ErrorIs(t, err, sourceErr)
				if sourceErr != nil {
					require.Nil(t, state.resolvedRevisionMetadata)
					require.Equal(t, installed.BundleMetadata, ext.Status.Install.Bundle)
					return
				}
				require.Same(t, target, state.resolvedRevisionMetadata)
				_, err = ApplyBundle(successfulSourceApplier{})(t.Context(), state, ext)
				require.NoError(t, err)
			})
		}
	}
}

func testingName(rolling bool, err error) string {
	name := "fresh"
	if rolling {
		name = "rolling"
	}
	if err != nil {
		name += "/" + err.Error()
	}
	return name
}

func TestSourceLookupRejectsUnknownAndNilBeforeRollout(t *testing.T) {
	var typedNil *TestSource
	for _, sourceType := range []string{"Unknown", "Nil", "TypedNil"} {
		for _, rolling := range []bool{false, true} {
			t.Run(sourceType+"/"+testingName(rolling, nil), func(t *testing.T) {
				sources := Resolvers{
					"Nil": nil, "TypedNil": typedNil,
					ocv1.SourceTypeCatalog: &CatalogSource{Resolver: resolve.Func(forbiddenCatalogResolver), Client: &forbiddenCatalogClient{}},
				}
				state := &reconcileState{revisionStates: &RevisionStates{}}
				if rolling {
					state.revisionStates.RollingOut = []*RevisionMetadata{sourceRevision("rolling", "1.0.0")}
				}
				_, err := ResolveBundle(sources)(t.Context(), state, sourceExtension(sourceType))
				require.ErrorContains(t, err, "unsupported sourceType "+`"`+sourceType+`"`)
				require.Nil(t, state.resolvedRevisionMetadata)
			})
		}
	}
}

func TestCatalogSourcePreservesFirstRollingRevision(t *testing.T) {
	first, last := sourceRevision("first", "1.0.0"), sourceRevision("last", "2.0.0")
	state := &reconcileState{revisionStates: &RevisionStates{RollingOut: []*RevisionMetadata{first, last}}}
	ext := sourceExtension(ocv1.SourceTypeCatalog)
	sources := Resolvers{ocv1.SourceTypeCatalog: &CatalogSource{Resolver: resolve.Func(forbiddenCatalogResolver), Client: &forbiddenCatalogClient{}}}
	_, err := ResolveBundle(sources)(t.Context(), state, ext)
	require.NoError(t, err)
	require.Same(t, first, state.resolvedRevisionMetadata)
}

// TestDeprecationEvidenceCollectedDuringRollout covers deprecation evidence
// collection while a rollout is in progress: resolution is not consulted
// (the resolver panics if called), and evidence still populates conditions.
func TestDeprecationEvidenceCollectedDuringRollout(t *testing.T) {
	deprecation := &declcfg.Deprecation{Entries: []declcfg.DeprecationEntry{{
		Reference: declcfg.PackageScopedReference{Schema: declcfg.SchemaPackage, Name: "example"}, Message: "deprecated package",
	}}}
	rolling := sourceRevision("rolling", "1.1.0")
	state := &reconcileState{revisionStates: &RevisionStates{RollingOut: []*RevisionMetadata{rolling}}}
	ext := sourceExtension(ocv1.SourceTypeCatalog)
	r := deprecatingResolver{
		Func:        resolve.Func(forbiddenCatalogResolver),
		deprecation: deprecation,
	}
	_, err := ResolveBundle(Resolvers{ocv1.SourceTypeCatalog: &CatalogSource{Resolver: r, Client: &forbiddenCatalogClient{}}})(t.Context(), state, ext)
	require.NoError(t, err)
	require.Same(t, rolling, state.resolvedRevisionMetadata)
	condition := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypePackageDeprecated)
	require.Equal(t, metav1.ConditionTrue, condition.Status)
	require.Equal(t, "deprecated package", condition.Message)
}

func TestCatalogPolicyRequiresCatalogConfiguration(t *testing.T) {
	for _, sourceType := range []string{ocv1.SourceTypeCatalog, "TestSource"} {
		t.Run(sourceType, func(t *testing.T) {
			ext := sourceExtension(sourceType)
			ext.Spec.Source.Catalog = nil
			exists, err := CheckCatalogsExist(t.Context(), &forbiddenCatalogClient{}, ext)
			require.ErrorContains(t, err, "require a Catalog source")
			require.False(t, exists)
		})
	}
	ext := sourceExtension(ocv1.SourceTypeCatalog)
	ext.Spec.Source.Catalog = nil
	source := &CatalogSource{Resolver: resolve.Func(forbiddenCatalogResolver), Client: &forbiddenCatalogClient{}}
	for _, rolling := range []bool{false, true} {
		t.Run(testingName(rolling, nil), func(t *testing.T) {
			state := &reconcileState{revisionStates: &RevisionStates{}}
			if rolling {
				state.revisionStates.RollingOut = []*RevisionMetadata{sourceRevision("rolling", "1.0.0")}
			}
			_, err := ResolveBundle(Resolvers{ocv1.SourceTypeCatalog: source})(t.Context(), state, ext)
			require.ErrorContains(t, err, "catalog source requires spec.source.catalog")
			require.Nil(t, state.resolvedRevisionMetadata)
		})
	}
}

func TestCatalogSourceDeprecationEvidence(t *testing.T) {
	resolutionErr := errors.New("catalog resolution failed")
	deprecation := &declcfg.Deprecation{Entries: []declcfg.DeprecationEntry{{
		Reference: declcfg.PackageScopedReference{Schema: declcfg.SchemaPackage, Name: "example"}, Message: "deprecated package",
	}}}
	for _, tc := range []struct {
		name        string
		err         error
		deprecation *declcfg.Deprecation
		queryErr    error
		status      metav1.ConditionStatus
	}{
		{name: "success empty", status: metav1.ConditionFalse},
		{name: "error without evidence", err: resolutionErr, queryErr: errors.New("catalog data unavailable"), status: metav1.ConditionUnknown},
		{name: "error with evidence", err: resolutionErr, deprecation: deprecation, status: metav1.ConditionTrue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ext := sourceExtension(ocv1.SourceTypeCatalog)
			state := &reconcileState{revisionStates: &RevisionStates{}}
			r := deprecatingResolver{
				Func: resolve.Func(func(context.Context, *ocv1.ClusterExtension, *ocv1.BundleMetadata) (*declcfg.Bundle, *declcfg.VersionRelease, error) {
					return &declcfg.Bundle{Name: "example.v1", Package: "example", Image: "example/bundle:v1"}, &declcfg.VersionRelease{Version: semver.MustParse("1.0.0")}, tc.err
				}),
				deprecation: tc.deprecation,
				queryErr:    tc.queryErr,
			}
			_, err := ResolveBundle(Resolvers{ocv1.SourceTypeCatalog: &CatalogSource{Resolver: r, Client: &forbiddenCatalogClient{}}})(t.Context(), state, ext)
			require.ErrorIs(t, err, tc.err)
			require.Equal(t, tc.status, apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypePackageDeprecated).Status)
		})
	}
}

func TestCatalogSourceFallbackPreservesVersionStringPolicy(t *testing.T) {
	sch := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(sch))
	resolutionErr := errors.New("catalog unavailable")
	for _, version := range []string{"", "1.0.0", ">=1.0.0"} {
		for _, catalogExists := range []bool{false, true} {
			t.Run(version+"/"+testingName(catalogExists, nil), func(t *testing.T) {
				builder := fake.NewClientBuilder().WithScheme(sch)
				if catalogExists {
					builder.WithObjects(&ocv1.ClusterCatalog{ObjectMeta: metav1.ObjectMeta{Name: "catalog"}})
				}
				ext := sourceExtension(ocv1.SourceTypeCatalog)
				ext.Spec.Source.Catalog = &ocv1.CatalogFilter{PackageName: "example", Version: version}
				installed := sourceRevision("installed", "1.0.0")
				state := &reconcileState{revisionStates: &RevisionStates{Installed: installed}}
				r := resolve.Func(func(context.Context, *ocv1.ClusterExtension, *ocv1.BundleMetadata) (*declcfg.Bundle, *declcfg.VersionRelease, error) {
					return nil, nil, resolutionErr
				})
				_, err := ResolveBundle(Resolvers{ocv1.SourceTypeCatalog: &CatalogSource{Resolver: r, Client: builder.Build()}})(t.Context(), state, ext)
				if catalogExists || version == ">=1.0.0" {
					require.ErrorIs(t, err, resolutionErr)
					require.Nil(t, state.resolvedRevisionMetadata)
				} else {
					require.NoError(t, err)
					require.Same(t, installed, state.resolvedRevisionMetadata)
				}
			})
		}
	}
}

func TestUnpackUsesSelectedSourceMaintenancePolicy(t *testing.T) {
	pullErr := errors.New("content unavailable")
	installed := sourceRevision("same", "1.0.0")
	target := sourceRevision("same", "1.0.0")
	target.Image, target.Package = "changed/image:v1", "changed-package"
	for _, tc := range []struct {
		name       string
		sourceType string
		source     Resolver
		wantError  bool
	}{
		{name: "catalog legacy name version", sourceType: ocv1.SourceTypeCatalog, source: &CatalogSource{Resolver: resolve.Func(forbiddenCatalogResolver), Client: &forbiddenCatalogClient{}}},
		{name: "test rejects maintenance", sourceType: "TestSource", source: &TestSource{result: target}, wantError: true},
		{name: "test permits maintenance", sourceType: "TestSource", source: &TestSource{result: target, maintain: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := &reconcileState{
				revisionStates: &RevisionStates{Installed: installed, RollingOut: []*RevisionMetadata{target}},
			}
			ext := sourceExtension(tc.sourceType)
			_, err := ResolveBundle(Resolvers{tc.sourceType: tc.source})(t.Context(), state, ext)
			require.NoError(t, err)
			_, err = UnpackBundle(&imageutil.FakePuller{Error: pullErr}, nil)(t.Context(), state, ext)
			if tc.wantError {
				require.ErrorIs(t, err, pullErr)
			} else {
				require.NoError(t, err)
				require.Nil(t, state.imageFS)
			}
		})
	}
}

func TestCatalogEventUsesSourcePolicy(t *testing.T) {
	sch := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(sch))
	objects := make([]client.Object, 0, 5)
	for _, name := range []string{ocv1.SourceTypeCatalog, "TestSource", "InterestedTestSource", "Unknown", "Nil"} {
		ext := sourceExtension(name)
		ext.Name = name
		objects = append(objects, ext)
	}
	c := fake.NewClientBuilder().WithScheme(sch).WithObjects(objects...).Build()
	sources := Resolvers{
		ocv1.SourceTypeCatalog: &CatalogSource{Client: &forbiddenCatalogClient{}},
		"TestSource":           &TestSource{}, "InterestedTestSource": &TestSource{enqueue: true}, "Nil": nil,
	}
	requests := clusterExtensionRequestsForCatalog(c, logr.Discard(), sources)(t.Context(), &ocv1.ClusterCatalog{})
	names := make([]string, 0, len(requests))
	for _, request := range requests {
		names = append(names, request.Name)
	}
	require.ElementsMatch(t, []string{ocv1.SourceTypeCatalog, "InterestedTestSource"}, names)
	_, err := sources.For(sourceExtension("TestSource"))
	require.NoError(t, err)
}

func TestCatalogEventPredicatePreservesResolvedRefBehavior(t *testing.T) {
	catalog := &ocv1.ClusterCatalog{Status: ocv1.ClusterCatalogStatus{ResolvedSource: &ocv1.ResolvedCatalogSource{
		Image: &ocv1.ResolvedImageSource{Ref: "example/catalog@sha256:old"},
	}}}
	updated := catalog.DeepCopy()
	updated.Labels = map[string]string{"changed": "label"}
	require.False(t, catalogResolutionInputsChanged(event.UpdateEvent{ObjectOld: catalog, ObjectNew: updated}))
	updated.Status.ResolvedSource.Image.Ref = "example/catalog@sha256:new"
	require.True(t, catalogResolutionInputsChanged(event.UpdateEvent{ObjectOld: catalog, ObjectNew: updated}))
	require.True(t, catalogResolutionInputsChanged(event.UpdateEvent{ObjectOld: &ocv1.ClusterCatalog{}, ObjectNew: updated}))
}
