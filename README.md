# rukpak-packageserver

A Kubernetes native way to make [FBC](https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs) content available on cluster.


## Summary

This repository is a prototype for a custom apiserver that uses a (dedicated ectd instance)[configs/etcd] to serve FBC on cluster. 


## Motivation. 

The current implemantation of (CatalogSource)[https://github.com/operator-framework/api/blob/master/pkg/operators/v1alpha1/catalogsource_types.go] creates (`registry Pods`)[https://github.com/operator-framework/operator-lifecycle-manager/blob/master/pkg/controller/operators/catalog/operator.go#L704-L769], which are pods running (`opm serve`)[https://github.com/operator-framework/operator-registry/blob/master/cmd/opm/serve/serve.go] exposing a grpc endpoint that serves content (over a set of distinct endpoints)[https://github.com/operator-framework/operator-registry#using-the-catalog-locally].

This has a few disadvantages:

* (Creating and managing Pods)[https://github.com/operator-framework/operator-lifecycle-manager/blob/caab6c52ec532dc82c7178eebb0377bd80d1e82a/pkg/controller/registry/reconciler/grpc.go#L125] instead of using `Deployments` is highly discouranged. OLM has had to deal with high maintaince burden in trying to maintain this architecture. Some of the effort already put into making sure the Pods are effectively managed include (but are not limited to): 
  1) [CatalogSource Pods consuming high memory](https://bugzilla.redhat.com/show_bug.cgi?id=2015814)
  2) [Inability to customize configurations to allow customers to schedule/lifecycle pods in their cluster (eg adding tolerations for tainted nodes)](https://bugzilla.redhat.com/show_bug.cgi?id=1927478). This ultimately led to the need for introducing the spec.grpcPodConfig field, which has set the stage for a long cycle of API surface expansion, one field at a time. 
  3) [Pods crashlooping without saving termination logs making it difficult to debug urgent customer cases](https://bugzilla.redhat.com/show_bug.cgi?id=1952238). Customer had no way to customize the Pod config to set termination log path, and had to wait for long-winded release cycle for the fix to reach their cluster before they could finally start debugging crashlooping pods. Meanwhile, the Alerts kept firing for the cluster while they waited for the fix to land. 
  4) A (~30 story points)[https://issues.redhat.com/browse/OLM-2600] epic to address Openshift changing the default Pod Security Admission level to `restricted`. 
  . 
  .
  . 
  (and many more....here's a list of bugzillas related to the current CatalogSource implementation that has `Assignee: anbhatta@redhat.com` on them: (list)[https://bugzilla.redhat.com/buglist.cgi?bug_status=__closed__&columnlist=product%2Ccomponent%2Cassigned_to%2Cbug_status%2Cresolution%2Cshort_desc%2Cdelta_ts%2Cbug_severity%2Cpriority&component=OLM&email1=anbhatta%40redhat.com&emailassigned_to1=1&emaillongdesc1=1&emailtype1=substring&list_id=12960194&order=bug_status%2C%20priority%2C%20assigned_to%2C%20bug_id%2C%20&product=OpenShift%20Container%20Platform&query_format=advanced&rh_sub_components=OLM&rh_sub_components=OperatorHub&short_desc=catalog%20source%20operatorhub&short_desc_type=anywordssubstr])

 * Since (the old way of) adding operator metadata to a sqllite database and serving that database over a grpc endpoint wasn't a kube native way to make the content available on cluster, a (custom apiserver)[https://github.com/operator-framework/operator-lifecycle-manager/tree/master/pkg/package-server] was needed to parse the content and re-create Package level metadata for making the (`PackageManifests`)[https://github.com/operator-framework/operator-lifecycle-manager/blob/master/pkg/package-server/apis/operators/packagemanifest.go] available.

Additionally, Openshit needed a [packageserver-manager](https://github.com/openshift/operator-framework-olm/tree/master/pkg/package-server-manager), increasing the fork between what's available upstream vs downstream, thereby increasing the effort needed to maintain the architecture.  List of bugs related to the packagerserver/packageserver-manager: (list)[https://issues.redhat.com/browse/OCPBUGS-1104?jql=project%20in%20(OCPBUGS%2C%20OLM)%20AND%20text%20~%20packageserver%20order%20by%20lastViewed%20DESC]

 * Similarly, any client inside/outside the cluster who wanted access to the content of the database inside the pod needed to hook into the `Service` first, needing to build caches like [this](https://github.com/operator-framework/operator-lifecycle-manager/blob/master/pkg/controller/registry/resolver/cache/cache.go) to avoid the expensive round tripping to the service endpoint (i.e needed to recreate the same content avaialable in the cluster, duplicating the memory requirement for accessing the content.)


 ## What does rukpak-packageserver do differently? 

 The rukpak-packageserver can be thought of as packageserver 2.0, which takes advantage of the FBC format (and the underlying library https://github.com/operator-framework/operator-registry/tree/master/alpha/declcfg) with a dedicated etcd instance, to efficiently expose the content of an index image inside a cluster (eliminating the need to connect to expensive services or build caches expanding memory surface for clients.)

![DEMO](./docs/static_includes/demo.gif)