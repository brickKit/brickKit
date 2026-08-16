{{/* 名字与标签 */}}

{{- define "market.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "market.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-market-api" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{/*
market.commonLabels 是**所有**资源共有的部分——**不含** app.kubernetes.io/name。

名字必须由每个资源自己给。集群内依赖曾经复用过带死名字的标签集，
结果 postgres 与 rustfs 的 Pod 也带上了 `name: market-api`：
`kubectl logs deployment/market-api` 会选中 rustfs 的 Pod，
`kubectl get pods -l app.kubernetes.io/name=market-api` 一次返回三个。
Service 当时只是**碰巧**没坏——targetPort 用的是命名端口 `http`，
而那两个 Pod 没有同名端口。改成数字端口就会立刻把流量转给数据库。
*/}}
{{- define "market.commonLabels" -}}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/part-of: brickkit-market
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "market.labels" -}}
app.kubernetes.io/name: market-api
{{ include "market.commonLabels" . }}
{{- end -}}

{{- define "market.selectorLabels" -}}
app.kubernetes.io/name: market-api
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "market.image" -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) -}}
{{- end -}}

{{/*
market.secretName 决定口令从哪个 Secret 来。

**两者都不给时直接 fail**，而不是渲染出一份缺变量的 Deployment。
理由与平台其他地方一致：一份能 apply、Pod 却起不来的清单，
现场只有 CrashLoopBackOff，看不出是配置没填。
*/}}
{{- define "market.secretName" -}}
{{- if .Values.auth.existingSecret -}}
{{- .Values.auth.existingSecret -}}
{{- else if and .Values.auth.databasePassword .Values.auth.rustfsAccessKey .Values.auth.rustfsSecretKey .Values.auth.adminPassword -}}
{{- printf "%s-secrets" (include "market.fullname" .) -}}
{{- else -}}
{{- fail "\n\n必须提供口令，二选一：\n  1) auth.existingSecret=<你自己创建的 Secret 名>（生产推荐）\n     该 Secret 需含 DATABASE_PASSWORD / RUSTFS_ACCESS_KEY / RUSTFS_SECRET_KEY / ADMIN_PASSWORD\n  2) 同时给出 auth.databasePassword / auth.rustfsAccessKey / auth.rustfsSecretKey / auth.adminPassword\n\n不给的话会生成一份能 apply、Pod 却起不来的清单——现场只有 CrashLoopBackOff，看不出是配置没填。\n" -}}
{{- end -}}
{{- end -}}

{{/*
数据库与对象存储的地址：打开 deps 时自动指向集群内那两个 Service。

不让使用者自己改是有意的——deps.enabled=true 却忘了改 database.host，
表现是市场连着一个不存在的外部地址反复重启，而集群里那个 postgres 好好地闲着。
*/}}
{{- define "market.databaseHost" -}}
{{- if .Values.deps.enabled -}}
{{- printf "%s-postgres" .Release.Name -}}
{{- else -}}
{{- .Values.database.host -}}
{{- end -}}
{{- end -}}

{{- define "market.storageEndpoint" -}}
{{- if .Values.deps.enabled -}}
{{- printf "http://%s-rustfs:9000" .Release.Name -}}
{{- else -}}
{{- .Values.storage.endpoint -}}
{{- end -}}
{{- end -}}
