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
Default to a separate object-controller whenever operator-controller uses Boxcutter.
An explicit enabled value also permits installing object-controller on its own.
Return an empty string when disabled so the helper can be used in conditionals.
*/}}
{{- define "objectController.enabled" -}}
{{- $enabled := .Values.options.objectController.enabled -}}
{{- if eq (toJson $enabled) "null" -}}
{{- $enabled = and .Values.options.operatorController.enabled (has "BoxcutterRuntime" .Values.options.operatorController.features.enabled) (not (has "BoxcutterRuntime" .Values.options.operatorController.features.disabled)) -}}
{{- end -}}
{{- if $enabled -}}
{{- if ne .Values.options.featureSet "experimental" -}}
{{- fail "objectController requires options.featureSet=experimental" -}}
{{- end -}}
true
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
