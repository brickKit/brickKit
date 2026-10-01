# portal/user-frontend

## 组件定位

ERP 门户的静态前端：nginx 把页面直接发给浏览器，并把 `/api/` 下的请求反向代理到 `erp/backend`，后端地址由平台注入、前端不写死。它也是 BrickKit 平台的自测组件，覆盖三件事：被浏览器访问、因而要映射到宿主机的组件（`expose` / `exposePort`），反向代理使用平台注入的地址，以及没有业务代码、只有 nginx 配置与静态文件的组件。

**负责**

- 发出静态页面（`/` 以及任何不存在的路径都回落到 `index.html`）
- 把 `/api/` 下的请求连同原始 URI 原样转发给 `erp/backend`
- `GET /healthz`：只回一个常量，不探后端
- 在 Kubernetes 里把注入的后端地址补成完整域名，让 nginx 解析得到

**不负责（归谁）**

- 登录、订单、权限判定、人员数据：归 `erp/backend` 及它背后的组件，门户只转发
- 从集群外进来的流量入口（TLS、域名）：归部署方——Docker 下是宿主机端口，K8s 下是平台按部署文件生成的 Ingress
- 后端不可用时的重试或降级：不做，`/api/` 直接返回 502，页面本身照常发出
- 数据与持久化：没有，静态文件都在镜像里

## 部署前准备

- 镜像 `brickkit-demo/portal-user-frontend:1.0.0` 要先在本机存在：`brickkit up` 不会构建它。由跑自测的人构建——在 BrickKit 仓库根目录执行 `make demo-images`，或在本组件目录执行 `docker build -t brickkit-demo/portal-user-frontend:1.0.0 .`。用 `docker image inspect brickkit-demo/portal-user-frontend:1.0.0` 确认。
- 要从浏览器访问，部署文件里这个组件的条目要开 `expose`（写法见配置指南）；`target: k8s` 时还必须写 `hostname`，否则 `brickkit up` 直接报错。

除此之外无需准备：不需要数据库、缓存或任何外部系统。

## 依赖说明

- `erp/backend`（强依赖）：`/api/` 下的全部请求都转发给它，地址来自平台注入的 `ERP_BACKEND_ENDPOINT`。没有它这个前端什么也做不了，所以不是可选依赖。nginx 在每次请求时才解析后端地址，后端还没起来、停掉或重启换了 IP 都不影响门户本身：`GET /` 与 `/healthz` 照常 200，只有 `/api/` 在后端不可达期间返回 502（连接超时 5 秒，读超时 30 秒）。

## 配置指南

这个组件没有 `configSchema`，使用者不需要为它写 `config/` 里的任何值。它用到的两个环境变量都不归使用者管：

| 环境变量 | 来源 |
| --- | --- |
| `ERP_BACKEND_ENDPOINT` | 平台按强依赖注入；在 K8s 里容器启动时自动补成完整域名 |
| `NGINX_LOCAL_RESOLVERS` | 容器启动时从自己的 `/etc/resolv.conf` 生成，Docker 与 K8s 下自动不同 |

要做的选择只有部署文件里的暴露方式（版本号只写在 `brickkit.yaml`）：

```yaml
components:
  - id: portal/user-frontend
    expose: true          # Docker：ports 8080:8080
    exposePort: 18080     # 可选，仅 Docker：ports 18080:8080
```

- Docker 下宿主机的 8080 被占用，或同一项目里别的组件也映射了 8080 时，用 `exposePort` 换一个宿主机端口；容器里始终是 8080。
- `target: k8s` 时 `expose: true` 生成的是 Ingress，`exposePort` 被忽略；`hostname` 必填，要 HTTPS 再加 `tlsSecret`。

## 契约索引

没有契约文件：门户不对外提供自己的 API。浏览器能访问的只有：

- `GET /`：静态页面；不存在的路径也返回 `index.html`
- `GET /healthz`：`{"status":"ok"}`，不探后端
- `/api/…`：原样转发给 `erp/backend`，接口以 `erp/backend` 的 `openapi.json` 为准。页面自己用到两条：`POST /api/v1/login`（请求体 `{"username": …, "password": …}`，响应里的 `token` 被保存下来）和 `GET /api/v1/orders`（带 `Authorization: Bearer <token>`），都用相对路径，浏览器从头到尾只认识门户这一个地址

例：门户映射在宿主机 8080 时，`curl -X POST localhost:8080/api/v1/login -H 'Content-Type: application/json' -d '{"username":"zhangsan","password":"demo-password"}'` 走的就是页面上"登录"按钮的那条路。

不发布事件，也不消费事件。

## 外壳声明

不是外壳。
