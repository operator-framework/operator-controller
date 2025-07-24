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
{{- if and (.Values.components.operatorController.enabled) (not .Values.components.catalogd.enabled) -}}
operator-controller-
{{- else if and (not .Values.components.operatorController.enabled) (.Values.components.catalogd.enabled) -}}
catalogd-
{{- end -}}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "olmv1.labels" -}}
helm.sh/chart: {{ include "olmv1.chart" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: olm
{{- end }}

{{/*
Common annoations
*/}}
{{- define "olmv1.annotations" -}}
olm.operatorframework.io/feature-set: {{ .Values.featureSet -}}{{- if .Values.components.e2e.enabled -}}-e2e{{- end -}}
{{- end }}

{{/*
Insertion of additional rules for RBAC
*/}}
{{- define "olmv1.catalogd.role.rules" -}}
{{- with .Values.components.catalogd.rules }}
{{- toYamlPretty . }}
{{- end }}
{{- end }}

{{- define "olmv1.catalogd.clusterRole.rules" -}}
{{- with .Values.components.catalogd.clusterRole.rules }}
{{- toYamlPretty . }}
{{- end }}
{{- end }}

{{- define "olmv1.operatorController.role.rules" -}}
{{- with .Values.components.operatorController.role.rules }}
{{- toYamlPretty . }}
{{- end }}
{{- end }}

{{- define "olmv1.operatorController.clusterRole.rules" -}}
{{- with .Values.components.operatorController.clusterRole.rules }}
{{- toYamlPretty . }}
{{- end }}
{{- end }}
