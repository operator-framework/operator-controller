{{/*
Expand the name of the chart.
*/}}
{{- define "olmv1.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "olmv1.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Return the name of the active component for a prefix, but _only_ if one is enabled
*/}}
{{- define "component.name.prefix" -}}
{{- if and (not .Values.options.operatorController.enabled) (not .Values.options.catalogd.enabled) (include "objectController.enabled" .) -}}
object-controller-
{{- else if and (.Values.options.operatorController.enabled) (not .Values.options.catalogd.enabled) -}}
operator-controller-
{{- else if and (not .Values.options.operatorController.enabled) (.Values.options.catalogd.enabled) -}}
catalogd-
{{- end -}}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "olmv1.labels" -}}
app.kubernetes.io/part-of: olm
{{- end }}

{{/*
Common annoations
*/}}
{{- define "olmv1.annotations" -}}
olm.operatorframework.io/feature-set: {{ .Values.options.featureSet -}}{{- if .Values.options.e2e.enabled -}}-e2e{{- end -}}
{{- end }}

{{/*
Insertion of additional rules for RBAC
*/}}

{{/*
Returns "operator-controller", "catalogd" or "olmv1" depending on enabled components
*/}}
{{- define "olmv1.label.name" -}}
{{- if and (not .Values.options.operatorController.enabled) (not .Values.options.catalogd.enabled) (include "objectController.enabled" .) -}}
object-controller
{{- else if (and .Values.options.operatorController.enabled (not .Values.options.catalogd.enabled)) -}}
operator-controller
{{- else if (and (not .Values.options.operatorController.enabled) .Values.options.catalogd.enabled) -}}
catalogd
{{- else -}}
olmv1
{{- end -}}
{{- end -}}

{{/*
Prepare component resources without enabling a second ClusterObjectSet reconciler.
The deployment and reconciliation handover will replace this activation guard.
*/}}
{{- define "objectController.enabled" -}}
{{- if hasKey .Values.options "objectController" -}}
{{- if .Values.options.objectController.enabled -}}
{{- fail "object-controller deployment support requires the reconciliation cutover" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
When rendering with OpenShift, only one of the main components (catalogd, operatorController)
should be enabled
*/}}
{{- if .Values.options.openshift.enabled -}}
{{- if and .Values.options.catalogd.enabled .Values.options.operatorController.enabled -}}
{{- fail "When rendering Openshift, only one of {catalogd, operatorController} should also be enabled" -}}
{{- end -}}
{{- end -}}
