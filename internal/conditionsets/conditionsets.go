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

package conditionsets

import (
	"github.com/operator-framework/operator-controller/api/v1alpha1"
)

// ConditionTypes is the full set of ClusterExtension condition Types.
// ConditionReasons is the full set of ClusterExtension condition Reasons.
//
// NOTE: unit tests in clusterextension_types_test will enforce completeness.
var ConditionTypes = []string{
	v1alpha1.TypeInstalled,
	v1alpha1.TypeDeprecated,
	v1alpha1.TypePackageDeprecated,
	v1alpha1.TypeChannelDeprecated,
	v1alpha1.TypeBundleDeprecated,
	v1alpha1.TypeProgressing,
}

var ConditionReasons = []string{
	v1alpha1.ReasonSucceeded,
	v1alpha1.ReasonDeprecated,
	v1alpha1.ReasonFailed,
	v1alpha1.ReasonBlocked,
	v1alpha1.ReasonRetrying,
}
