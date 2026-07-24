{{/*
차트 이름 (nodevitals `_helpers.tpl` 관례 미러 — 고정 리소스명 기반).
*/}}
{{- define "nodevitals-observatory.name" -}}
nodevitals-observatory
{{- end -}}

{{/*
릴리스에 걸친 고유 fullname. release 이름이 이미 차트명을 포함하면 중복을 피한다.
63자 DNS 라벨 상한을 지킨다.
*/}}
{{- define "nodevitals-observatory.fullname" -}}
{{- if contains "nodevitals-observatory" .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-nodevitals-observatory" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{/*
공통 라벨 — app.kubernetes.io/name·instance·version·managed-by (설계 계약 고정).
*/}}
{{- define "nodevitals-observatory.labels" -}}
{{ include "nodevitals-observatory.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
셀렉터 라벨 — Deployment/Service 셀렉터와 파드 템플릿 라벨이 반드시 공유해야
하는 부분집합만 (버전 등 가변 라벨을 셀렉터에 넣으면 롤아웃마다 셀렉터가
바뀌어 파드가 유실된다).
*/}}
{{- define "nodevitals-observatory.selectorLabels" -}}
app.kubernetes.io/name: {{ include "nodevitals-observatory.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
사용할 ServiceAccount 이름 — create=false 면 사용자가 준 이름(또는 default)을 그대로 쓴다.
*/}}
{{- define "nodevitals-observatory.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "nodevitals-observatory.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
이미지 참조: repository:tag (tag 기본값 = Chart.appVersion).
*/}}
{{- define "nodevitals-observatory.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{ .Values.image.repository }}:{{ $tag }}
{{- end -}}
