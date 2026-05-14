{{- define "aws-nlb-controller.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "aws-nlb-controller.fullname" -}}
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

{{- define "aws-nlb-controller.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "aws-nlb-controller.labels" -}}
helm.sh/chart: {{ include "aws-nlb-controller.chart" . }}
{{ include "aws-nlb-controller.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "aws-nlb-controller.selectorLabels" -}}
app.kubernetes.io/name: {{ include "aws-nlb-controller.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "aws-nlb-controller.serviceAccountName" -}}
{{- .Values.serviceAccount.name | default (include "aws-nlb-controller.fullname" .) }}
{{- end }}

{{- define "aws-nlb-controller.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end }}

{{- define "aws-nlb-controller.awsVPCTags" -}}
{{- $items := list -}}
{{- range $key, $value := .Values.controller.aws.vpcTags -}}
{{- $items = append $items (printf "%s=%s" $key $value) -}}
{{- end -}}
{{- join "," $items -}}
{{- end }}

{{- define "aws-nlb-controller.awsEndpointURLs" -}}
{{- $items := list -}}
{{- range $service, $url := .Values.controller.aws.endpointURLs -}}
{{- $items = append $items (printf "%s=%s" $service $url) -}}
{{- end -}}
{{- join "," $items -}}
{{- end }}
