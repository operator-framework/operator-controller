{{- define "olm.targetNamespaces" -}}
{{- $targetNamespaces := .Values.watchNamespaces -}}
{{- if not $targetNamespaces -}}
  {{- $targetNamespaces = (list "") -}}
{{- end -}}
{{- join "," $targetNamespaces -}}
{{- end -}}
