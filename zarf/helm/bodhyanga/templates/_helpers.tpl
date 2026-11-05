{{/*
Expand the name of the chart.
*/}}
{{- define "bodhyanga.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "bodhyanga.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "bodhyanga.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels applied to all resources.
*/}}
{{- define "bodhyanga.labels" -}}
helm.sh/chart: {{ include "bodhyanga.chart" . }}
{{ include "bodhyanga.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: bodhyanga
app.kubernetes.io/component: care-api
{{- end }}

{{/*
Selector labels — used for pod selectors and service selectors.
These must be stable and never include mutable values like chart version.
*/}}
{{- define "bodhyanga.selectorLabels" -}}
app.kubernetes.io/name: {{ include "bodhyanga.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the ServiceAccount to use.
*/}}
{{- define "bodhyanga.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "bodhyanga.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Return the name of the secret that contains sensitive env vars.
If existingSecret is set, use that; otherwise use the chart-managed secret.
*/}}
{{- define "bodhyanga.secretName" -}}
{{- if .Values.existingSecret }}
{{- .Values.existingSecret }}
{{- else }}
{{- printf "%s-secret" (include "bodhyanga.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Return the ConfigMap name for non-secret runtime config.
*/}}
{{- define "bodhyanga.configMapName" -}}
{{- printf "%s-config" (include "bodhyanga.fullname" .) }}
{{- end }}

{{/*
Return true if any secret env var has a non-empty value (for dev mode secret rendering).
*/}}
{{- define "bodhyanga.hasSecretValues" -}}
{{- $hasValues := false }}
{{- range $k, $v := .Values.env }}
  {{- if $v }}
    {{- $hasValues = true }}
  {{- end }}
{{- end }}
{{- $hasValues }}
{{- end }}
