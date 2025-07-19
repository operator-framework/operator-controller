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
olm.operatorframework.io/feature-set: {{ .Values.featureSet }}
{{- end }}
