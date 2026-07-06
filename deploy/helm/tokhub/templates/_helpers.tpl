{{- define "tokhub.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "tokhub.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "tokhub.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "tokhub.labels" -}}
app.kubernetes.io/name: {{ include "tokhub.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}
