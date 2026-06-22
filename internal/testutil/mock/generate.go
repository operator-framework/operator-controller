/*
Copyright 2022.

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

package mock

// External interfaces
//go:generate mockgen -destination=helmclient/mock_actionclient.go -package=helmclient github.com/operator-framework/helm-operator-plugins/pkg/client ActionInterface,ActionClientGetter
//go:generate mockgen -destination=helmclient/mock_composite.go -package=helmclient github.com/operator-framework/operator-controller/internal/testutil/mock/helmclient ActionClientGetterAndInterface
//go:generate mockgen -destination=ctrlclient/mock_client.go -package=ctrlclient sigs.k8s.io/controller-runtime/pkg/client Client,StatusWriter,SubResourceWriter
//go:generate mockgen -destination=crdclient/mock_crdinterface.go -package=crdclient k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1 CustomResourceDefinitionInterface
//go:generate mockgen -destination=logrsink/mock_logsink.go -package=logrsink github.com/go-logr/logr LogSink
//go:generate mockgen -destination=machinery/mock_results.go -package=machinery pkg.package-operator.run/boxcutter/machinery RevisionResult,PhaseResult,ObjectResult,RevisionTeardownResult
//go:generate mockgen -destination=httputil/mock_roundtripper.go -package=httputil net/http RoundTripper

// Internal interfaces — catalogd
//go:generate mockgen -destination=storage/mock_instance.go -package=storage github.com/operator-framework/operator-controller/internal/catalogd/storage Instance
//go:generate mockgen -destination=catalogdserver/mock_catalogstore.go -package=catalogdserver github.com/operator-framework/operator-controller/internal/catalogd/server CatalogStore
//go:generate mockgen -destination=catalogdservice/mock_graphqlservice.go -package=catalogdservice github.com/operator-framework/operator-controller/internal/catalogd/service GraphQLService

// Internal interfaces — operator-controller applier
//go:generate mockgen -destination=applier/mock_applier.go -package=applier github.com/operator-framework/operator-controller/internal/operator-controller/applier Preflight,HelmReleaseToObjectsConverterInterface,HelmChartProvider,ClusterObjectSetGenerator,ManifestProvider

// Internal interfaces — operator-controller catalogmetadata
//go:generate mockgen -destination=catalogclient/mock_cache.go -package=catalogclient github.com/operator-framework/operator-controller/internal/operator-controller/catalogmetadata/client Cache

// Internal interfaces — operator-controller config
//go:generate mockgen -destination=config/mock_schemaprovider.go -package=config github.com/operator-framework/operator-controller/internal/operator-controller/config SchemaProvider

// Internal interfaces — operator-controller contentmanager
//go:generate mockgen -destination=contentmanager/mock_contentmanager.go -package=contentmanager github.com/operator-framework/operator-controller/internal/operator-controller/contentmanager Manager
//go:generate mockgen -destination=cmcache/mock_cache.go -package=cmcache github.com/operator-framework/operator-controller/internal/operator-controller/contentmanager/cache Cache,Watcher,CloserSyncingSource

// Internal interfaces — operator-controller controllers
//go:generate mockgen -destination=controllers/mock_controllers.go -package=controllers github.com/operator-framework/operator-controller/internal/operator-controller/controllers CatalogCache,CatalogCachePopulator,RevisionStatesGetter,Applier,RevisionEngine,RevisionEngineFactory

// Internal interfaces — rukpak render
//go:generate mockgen -destination=render/mock_certprovider.go -package=render github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render CertificateProvider
