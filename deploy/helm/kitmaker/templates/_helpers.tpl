{{- define "kitmaker.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "kitmaker.fullname" -}}
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

{{- define "kitmaker.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{ include "kitmaker.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "kitmaker.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kitmaker.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "kitmaker.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "kitmaker.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "kitmaker.image" -}}
{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}
{{- end }}

{{- define "kitmaker.databaseSecretName" -}}
{{- if .Values.database.existingSecret }}
{{- .Values.database.existingSecret }}
{{- else }}
{{- printf "%s-database" (include "kitmaker.fullname" .) }}
{{- end }}
{{- end }}

{{- define "kitmaker.secretsSecretName" -}}
{{- if .Values.secrets.existingSecret }}
{{- .Values.secrets.existingSecret }}
{{- else }}
{{- printf "%s-secrets" (include "kitmaker.fullname" .) }}
{{- end }}
{{- end }}

{{- define "kitmaker.baseURL" -}}
{{- if .Values.config.baseURL }}
{{- .Values.config.baseURL }}
{{- else if and .Values.route.enabled .Values.route.host }}
{{- printf "https://%s" .Values.route.host }}
{{- else if .Values.ingress.enabled }}
{{- printf "%s://%s" (ternary "https" "http" (gt (len .Values.ingress.tls) 0)) .Values.ingress.host }}
{{- end }}
{{- end }}

{{/* Env shared by serve, worker and migrate. */}}
{{- define "kitmaker.env" -}}
- name: KITMAKER_DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ include "kitmaker.databaseSecretName" . }}
      key: {{ .Values.database.existingSecretKey }}
- name: KITMAKER_STORAGE_URL
  value: {{ required "storage.url is required" .Values.storage.url | quote }}
- name: KITMAKER_SECRETS_URL
  valueFrom:
    secretKeyRef:
      name: {{ include "kitmaker.secretsSecretName" . }}
      key: {{ .Values.secrets.existingSecretKey }}
- name: KITMAKER_LOG_LEVEL
  value: {{ .Values.config.logLevel | quote }}
{{- with (include "kitmaker.baseURL" .) }}
- name: KITMAKER_BASE_URL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.config.bootstrapAdmin }}
- name: KITMAKER_BOOTSTRAP_ADMIN
  value: {{ . | quote }}
{{- end }}
{{- with .Values.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/* Pod-level fields shared by every workload. */}}
{{- define "kitmaker.podSpecCommon" -}}
{{- with .Values.imagePullSecrets }}
imagePullSecrets:
  {{- toYaml . | nindent 2 }}
{{- end }}
securityContext:
  {{- toYaml .Values.podSecurityContext | nindent 2 }}
volumes:
  - name: tmp
    emptyDir: {}
{{- end }}

{{/* Container-level fields shared by every workload. */}}
{{- define "kitmaker.containerCommon" -}}
image: {{ include "kitmaker.image" . }}
imagePullPolicy: {{ .Values.image.pullPolicy }}
securityContext:
  {{- toYaml .Values.securityContext | nindent 2 }}
env:
  {{- include "kitmaker.env" . | nindent 2 }}
{{- with .Values.extraEnvFrom }}
envFrom:
  {{- toYaml . | nindent 2 }}
{{- end }}
volumeMounts:
  - name: tmp
    mountPath: /tmp
{{- end }}
