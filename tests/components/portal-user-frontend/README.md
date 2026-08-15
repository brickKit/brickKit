# portal/user-frontend

静态前端，nginx 反向代理到 `erp/backend`。BrickKit Phase 5 的第六个测试组件。

## 它验证平台的什么

| 验证点 | 为什么前面的组件覆盖不了 |
| --- | --- |
| **`expose: true` 端口映射** | 前面六个组件全是 `expose: false`，端口映射这条路径没走过 |
| **`exposePort` 自定义** | 同上 |
| **没有业务代码的组件** | 只有 nginx 配置与静态文件，验证平台不假设组件是什么语言 |
| **反向代理拿注入地址** | 后端地址来自 `${ERP_BACKEND_ENDPOINT}`，前端不硬编码 |

## 两条配套的 nginx 配置，缺一不可

```nginx
resolver ${NGINX_LOCAL_RESOLVERS} valid=10s ipv6=off;
set $backend "${ERP_BACKEND_ENDPOINT}";
proxy_pass $backend$request_uri;
```

**为什么不直接 `proxy_pass ${ERP_BACKEND_ENDPOINT};`：**
nginx 对写死在 `proxy_pass` 里的主机名**只在启动时解析一次**。后端还没起来时，
nginx 会以 `host not found in upstream` 直接退出 —— 整个前端起不来，
而它本该照常把页面发出去，只是 `/api/` 暂时 502。

把地址放进变量就变成**每次请求时解析**：启动顺序不再要紧，后端重启换了 IP 也跟得上。
代价是必须显式声明 `resolver`，而且 nginx 不再自动转发原始 URI，得自己带 `$request_uri`。

真容器验证过：**停掉 erp/backend 再启动前端**，`GET /` 返回 200、`/healthz` 返回 200、
`/api/` 返回 502 —— 前端本身完全正常。

### DNS 地址不写死

`NGINX_LOCAL_RESOLVERS` 由官方镜像自带的 `15-local-resolvers.envsh` 从 `/etc/resolv.conf`
生成（还处理了 IPv6 方括号与多个 nameserver）。这个开关**默认是关的**，
必须在 Dockerfile 里 `ENV NGINX_ENTRYPOINT_LOCAL_RESOLVERS=1` 打开 ——
漏了这一行，`resolver` 拿到空值，nginx 直接语法错误起不来。

Docker 的内嵌 DNS 与 K8s 的 CoreDNS 是两个完全不同的地址，写死任何一个另一个环境就废。

## 非 root 的 nginx 需要三处改动

002 §1.4 要求容器不以 root 运行。官方 nginx 镜像默认 master 进程是 root，
只有 worker 降权。要整个进程非 root：

| 改动 | 不改会怎样 |
| --- | --- |
| `pid /tmp/nginx.pid` | 默认的 `/var/run/nginx.pid` 非 root 写不了 |
| `chown` `/etc/nginx/conf.d` | 入口脚本要把模板渲染进去 |
| `chown` `/var/cache/nginx` | nginx 的临时目录 |
| 监听 8080 而非 80 | 1024 以下的端口需要特权 |

最后一条也是 `component.yaml` 里 `deployment.port: 8080` 的原因。

## 健康检查

```nginx
location = /healthz {
    return 200 '{"status":"ok"}';
}
```

用**精确匹配**（`location =`），避免被 `location /api/` 之类的规则捞去代理到后端 ——
那样后端一抖，编排系统就会把这个本身完全正常的前端容器杀掉重启（002 §9.4）。

## 配置

| 环境变量 | 来源 | 必需 |
| --- | --- | --- |
| `ERP_BACKEND_ENDPOINT` | 平台按强依赖注入（003 §4.5） | ✅ |
| `NGINX_LOCAL_RESOLVERS` | 容器自己从 `/etc/resolv.conf` 生成 | 自动 |

没有 `configSchema`：这个组件没有任何需要使用者调的东西。

## 怎么暴露到宿主机

```yaml
components:
  - id: portal/user-frontend
    version: 1.0.0
    expose: true          # → ports: 8080:8080
    exposePort: 18080     # → ports: 18080:8080（可选，仅 Docker 环境）
```

K8s 环境下 `expose: true` 生成的是 Ingress，`exposePort` 被忽略（003 §4.7）。

## 本地运行

```bash
go test ./...    # 会用真 nginx 跑一遍 `nginx -t`，没装 Docker 时跳过
```

那条测试是这个组件最有价值的一个：nginx 配置写错时容器**启动失败**，
平台看到的只是"容器起不来"，真正有用的报错还得进容器才看得到。

## 设计依据

002 组件规范（§1.4 组件约束、§9.4 健康检查）、003（§4.5 地址注入、§4.7 expose）、
开发计划 §0.2（前端组件用 nginx:alpine）。
