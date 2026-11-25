{{- define "auto-netviz.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "auto-netviz.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "auto-netviz.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: {{ include "auto-netviz.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "auto-netviz.serverTag" -}}
{{- default (printf "v%s" .Chart.AppVersion) .Values.server.image.tag -}}
{{- end -}}

{{- define "auto-netviz.webTag" -}}
{{- default (printf "v%s" .Chart.AppVersion) .Values.web.image.tag -}}
{{- end -}}

{{- define "auto-netviz.collectorTag" -}}
{{- default (printf "v%s" .Chart.AppVersion) .Values.collector.image.tag -}}
{{- end -}}

{{- define "auto-netviz.serviceAccountName" -}}
{{- printf "%s-collector" (include "auto-netviz.fullname" .) -}}
{{- end -}}
