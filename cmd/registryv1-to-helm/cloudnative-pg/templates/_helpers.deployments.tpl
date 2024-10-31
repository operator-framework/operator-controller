{{- define "deployment.cnpg-controller-manager.affinity" -}}
null
{{- end -}}

{{- define "deployment.cnpg-controller-manager.manager" -}}
{"name":"manager","image":"ghcr.io/cloudnative-pg/cloudnative-pg@sha256:22fd4647a25a4a97bfa36f322b7188b7fdbce1db28f4197d4d2c84422bebdc08","command":["/manager"],"args":["controller","--leader-elect","--config-map-name=cnpg-controller-manager-config","--secret-name=cnpg-controller-manager-config","--webhook-port=9443"],"ports":[{"name":"metrics","containerPort":8080,"protocol":"TCP"},{"name":"webhook-server","containerPort":9443,"protocol":"TCP"}],"env":[{"name":"WATCH_NAMESPACE","valueFrom":{"fieldRef":{"fieldPath":"metadata.annotations['olm.targetNamespaces']"}}},{"name":"WEBHOOK_CERT_DIR","value":"/apiserver.local.config/certificates"},{"name":"RELATED_IMAGE_CNPG","value":"ghcr.io/cloudnative-pg/cloudnative-pg@sha256:22fd4647a25a4a97bfa36f322b7188b7fdbce1db28f4197d4d2c84422bebdc08"},{"name":"OPERATOR_IMAGE_NAME","value":"ghcr.io/cloudnative-pg/cloudnative-pg@sha256:22fd4647a25a4a97bfa36f322b7188b7fdbce1db28f4197d4d2c84422bebdc08"},{"name":"OPERATOR_NAMESPACE","valueFrom":{"fieldRef":{"fieldPath":"metadata.namespace"}}},{"name":"MONITORING_QUERIES_CONFIGMAP","value":"cnpg-default-monitoring"}],"resources":{},"volumeMounts":[{"name":"scratch-data","mountPath":"/controller"},{"name":"webhook-certificates","mountPath":"/run/secrets/cnpg.io/webhook"},{"name":"apiservice-cert","mountPath":"/apiserver.local.config/certificates"},{"name":"webhook-cert","mountPath":"/tmp/k8s-webhook-server/serving-certs"}],"livenessProbe":{"httpGet":{"path":"/readyz","port":9443,"scheme":"HTTPS"}},"readinessProbe":{"httpGet":{"path":"/readyz","port":9443,"scheme":"HTTPS"}},"securityContext":{"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true,"allowPrivilegeEscalation":false,"seccompProfile":{"type":"RuntimeDefault"}}}
{{- end -}}

{{- define "deployment.cnpg-controller-manager.nodeSelector" -}}
null
{{- end -}}

{{- define "deployment.cnpg-controller-manager.selector" -}}
{"matchLabels":{"app.kubernetes.io/name":"cloudnative-pg"}}
{{- end -}}

{{- define "deployment.cnpg-controller-manager.tolerations" -}}
null
{{- end -}}

{{- define "deployment.cnpg-controller-manager.volumes" -}}
[{"name":"scratch-data","emptyDir":{}},{"name":"webhook-certificates","secret":{"secretName":"cnpg-webhook-cert","defaultMode":420,"optional":true}},{"name":"apiservice-cert","secret":{"secretName":"cloudnative-pg.v1.24.1-cnpg-controller-manager-cert","items":[{"key":"tls.crt","path":"apiserver.crt"},{"key":"tls.key","path":"apiserver.key"}]}},{"name":"webhook-cert","secret":{"secretName":"cloudnative-pg.v1.24.1-cnpg-controller-manager-cert","items":[{"key":"tls.crt","path":"tls.crt"},{"key":"tls.key","path":"tls.key"}]}}]
{{- end -}}

{{- define "deployment.cnpg-controller-manager.spec.overrides" -}}
  {{- $overrides := dict -}}

  {{- $templateMetadataOverrides := dict
    "annotations" (dict
      "olm.targetNamespaces" (include "olm.targetNamespaces" .)
    )
  -}}

  {{- $templateSpecOverrides := dict -}}
  {{- $origAffinity := fromYaml (include "deployment.cnpg-controller-manager.affinity" .) -}}
  {{- if .Values.affinity -}}
    {{- $_ := set $templateSpecOverrides "affinity" .Values.affinity -}}
  {{- else if $origAffinity -}}
    {{- $_ := set $templateSpecOverrides "affinity" $origAffinity -}}
  {{- end -}}

  {{- $origNodeSelector := fromYaml (include "deployment.cnpg-controller-manager.nodeSelector" .) -}}
  {{- if .Values.nodeSelector -}}
    {{- $_ := set $templateSpecOverrides "nodeSelector" .Values.nodeSelector -}}
  {{- else if $origNodeSelector -}}
    {{- $_ := set $templateSpecOverrides "nodeSelector" $origNodeSelector -}}
  {{- end -}}

  {{- $origSelector := fromYaml (include "deployment.cnpg-controller-manager.selector" .) -}}
  {{- if .Values.selector -}}
    {{- $_ := set $overrides "selector" .Values.selector -}}
  {{- else if $origSelector -}}
    {{- $_ := set $overrides "selector" $origSelector -}}
  {{- end -}}

  {{- $origTolerations := fromYamlArray (include "deployment.cnpg-controller-manager.tolerations" .) -}}
  {{- if and $origTolerations .Values.tolerations -}}
    {{- $_ := set $templateSpecOverrides "tolerations" (concat $origTolerations .Values.tolerations | uniq) -}}
  {{- else if .Values.tolerations -}}
    {{- $_ := set $templateSpecOverrides "tolerations" .Values.tolerations -}}
  {{- else if $origTolerations -}}
    {{- $_ := set $templateSpecOverrides "tolerations" $origTolerations -}}
  {{- end -}}

  {{- $origVolumes := fromYamlArray (include "deployment.cnpg-controller-manager.volumes" .) -}}
  {{- if and $origVolumes .Values.volumes -}}
    {{- $volumes := .Values.volumes -}}
    {{- $volumeNames := list -}}
    {{- range $volumes -}}{{- $volumeNames = append $volumeNames .name -}}{{- end -}}
    {{- range $origVolumes -}}
      {{- if not (has .name $volumeNames) -}}
        {{- $volumes = append $volumes . -}}
        {{- $volumeNames = append $volumeNames .name -}}
      {{- end -}}
    {{- end -}}
    {{- $_ := set $templateSpecOverrides "volumes" $volumes -}}
  {{- else if .Values.volumes -}}
    {{- $_ := set $templateSpecOverrides "volumes" .Values.volumes -}}
  {{- else if $origVolumes -}}
    {{- $_ := set $templateSpecOverrides "volumes" $origVolumes -}}
  {{- end -}}

  {{- $containers := list -}}


  {{- $origContainer0 := fromYaml (include "deployment.cnpg-controller-manager.manager" .) -}}

  {{- $origEnv0 := $origContainer0.env -}}
  {{- if and $origEnv0 .Values.env -}}
    {{- $env := .Values.env -}}
    {{- $envNames := list -}}
    {{- range $env -}}{{- $envNames = append $envNames .name -}}{{- end -}}
    {{- range $origEnv0 -}}
      {{- if not (has .name $envNames) -}}
        {{- $env = append $env . -}}
        {{- $envNames = append $envNames .name -}}
      {{- end -}}
    {{- end -}}
    {{- $_ := set $origContainer0 "env" $env -}}
  {{- else if .Values.env -}}
    {{- $_ := set $origContainer0 "env" .Values.env -}}
  {{- end -}}

  {{- $origEnvFrom0 := $origContainer0.envFrom -}}
  {{- if and $origEnvFrom0 .Values.envFrom -}}
    {{- $_ := set $origContainer0 "envFrom" (concat $origEnvFrom0 .Values.envFrom | uniq) -}}
  {{- else if .Values.envFrom -}}
    {{- $_ := set $origContainer0 "envFrom" .Values.envFrom -}}
  {{- end -}}

  {{- $origResources0 := $origContainer0.resources -}}
  {{- if .Values.resources -}}
    {{- $_ := set $origContainer0 "resources" .Values.resources -}}
  {{- end -}}

  {{- $origVolumeMounts0 := $origContainer0.volumeMounts -}}
  {{- if and $origVolumeMounts0 .Values.volumeMounts -}}
    {{- $volumeMounts := .Values.volumeMounts -}}
    {{- $volumeMountNames := list -}}
    {{- range $volumeMounts -}}{{- $volumeMountNames = append $volumeMountNames .name -}}{{- end -}}
    {{- range $origVolumeMounts0 -}}
      {{- if not (has .name $volumeMountNames) -}}
        {{- $volumeMounts = append $volumeMounts . -}}
        {{- $volumeMountNames = append $volumeMountNames .name -}}
      {{- end -}}
    {{- end -}}
    {{- $_ := set $origContainer0 "volumeMounts" $volumeMounts -}}
  {{- else if .Values.volumeMounts -}}
    {{- $_ := set $origContainer0 "volumeMounts" .Values.volumeMounts -}}
  {{- end -}}

  {{- $containers = append $containers $origContainer0 -}}
  {{- $templateSpecOverrides := merge $templateSpecOverrides (dict "containers" $containers) -}}

  {{- $overrides = merge $overrides (dict "template" (dict "metadata" $templateMetadataOverrides)) -}}
  {{- $overrides = merge $overrides (dict "template" (dict "spec" $templateSpecOverrides)) -}}
  {{- dict "spec" $overrides | toYaml -}}
{{- end -}}