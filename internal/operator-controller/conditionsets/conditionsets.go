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
	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// ConditionTypes is the full set of ClusterExtension condition Types.
// ConditionReasons is the full set of ClusterExtension condition Reasons.
//
// NOTE: unit tests in clusterextension_types_test will enforce completeness.
var ConditionTypes = []string{
	ocv1.ClusterExtensionTypeInstalled,
	ocv1.ClusterExtensionTypeDeprecated,
	ocv1.ClusterExtensionTypePackageDeprecated,
	ocv1.ClusterExtensionTypeChannelDeprecated,
	ocv1.ClusterExtensionTypeBundleDeprecated,
	ocv1.ClusterExtensionTypeProgressing,
	ocv1.ClusterExtensionTypeAvailable,
}

var ConditionReasons = []string{
	ocv1.ClusterExtensionReasonSucceeded,
	ocv1.ClusterExtensionReasonDeprecated,
	ocv1.ClusterExtensionReasonNotDeprecated,
	ocv1.ClusterExtensionReasonDeprecationStatusUnknown,
	ocv1.ClusterExtensionReasonFailed,
	ocv1.ClusterExtensionReasonBlocked,
	ocv1.ClusterExtensionReasonInvalidConfiguration,
	ocv1.ClusterExtensionReasonRetrying,
	ocv1.ClusterExtensionReasonAbsent,
	ocv1.ClusterExtensionReasonRollingOut,
	ocv1.ClusterExtensionReasonProgressDeadlineExceeded,
	ocv1.ClusterExtensionReasonProbesSucceeded,
}
