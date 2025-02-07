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
	ocv1.TypeInstalled,
	ocv1.TypeDeprecated,
	ocv1.TypePackageDeprecated,
	ocv1.TypeChannelDeprecated,
	ocv1.TypeBundleDeprecated,
	ocv1.TypeProgressing,
}

var ConditionReasons = []string{
	ocv1.ReasonSucceeded,
	ocv1.ReasonDeprecated,
	ocv1.ReasonFailed,
	ocv1.ReasonBlocked,
	ocv1.ReasonRetrying,
}
