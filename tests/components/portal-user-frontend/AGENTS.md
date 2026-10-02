# portal/user-frontend

没有业务代码的组件：官方 `nginx:1.27-alpine` 镜像加一份配置模板、一个入口钩子和一个静态页面；Go 只用来写测试。怎么用、边界和契约见 `BRICKKIT.md`；依赖、部署见 `component.yaml`。

## 代码地图

| 路径 | 管什么 |
| --- | --- |
| `templates/default.conf.template` | nginx 配置模板：监听 8080、静态资源、/healthz、/api/ 反向代理；官方入口脚本用 envsubst 把它渲染进 /etc/nginx/conf.d/ |
| `docker-entrypoint.d/15-cluster-fqdn.envsh` | 入口钩子：在 K8s 里把 `ERP_BACKEND_ENDPOINT` 的裸服务名补成完整域名 |
| `html/index.html` | 静态页面：登录与订单两块，一律用相对路径 /api/v1/… 请求 |
| `html/` | 原样拷进镜像的 /usr/share/nginx/html/ |
| `Dockerfile` | 镜像：拷入上面三样、打开 resolver 探测、让整个 nginx 以 uid 101 非 root 运行、暴露 8080 |
| `component_test.go` | 全部测试：静态检查配置与 Dockerfile，并用真 nginx 跑 `nginx -t`、检查渲染结果、跑钩子脚本 |
| `component.yaml` | 组件清单：强依赖 erp/backend、端口 8080、健康检查；没有 `artifacts`、`configSchema`、`migration` |
| `go.mod` | 只为 `go test` 存在的 Go module，没有任何第三方依赖 |

| 功能 | 从这里开始 | 然后 |
| --- | --- | --- |
| 反向代理到后端 | `templates/default.conf.template` 的 `location /api/` | `TestProxyResolvesAtRequestTime`、`TestConfigUsesInjectedBackendEndpoint` |
| DNS 地址 | `templates/default.conf.template` 的 `resolver` 行 | `Dockerfile` 的 `NGINX_ENTRYPOINT_LOCAL_RESOLVERS=1`，`TestResolverIsNotHardcoded` |
| K8s 下的后端域名 | `docker-entrypoint.d/15-cluster-fqdn.envsh` | `TestClusterFQDNHookBehaviour` 及另两条 `TestClusterFQDNHook*` |
| 健康检查 | `templates/default.conf.template` 的 `location = /healthz` | `TestHealthzDoesNotProxy` |
| 页面 | `html/index.html` | `TestIndexHasNoHardcodedBackend` |
| 非 root 运行 | `Dockerfile` 的 `RUN sed …` 与 `USER 101` | `TestDockerfileRunsAsNonRoot`、`TestPortsAgree` |

## 构建与测试

```bash
go vet ./... && go test ./...
# 成功：ok  github.com/brickkit/components/portal-user-frontend（有 Docker 时测试自己构建镜像，约 4 秒）
docker build -t brickkit-demo/portal-user-frontend:1.0.0 .
docker run --rm -p 8080:8080 -e ERP_BACKEND_ENDPOINT=http://erp-backend-1-0-0:8080 brickkit-demo/portal-user-frontend:1.0.0
# 另开终端：curl localhost:8080/healthz 返回 {"status":"ok"}；没有后端时 curl localhost:8080/api/v1/orders 返回 502
brickkit lint --strict                  # 组件清单与这几份文档；成功时 0 warnings
```

- 最有价值的是 `TestNginxConfigIsValid`：nginx 配置写错时容器启动失败，平台看到的只是"容器起不来"，真正有用的报错还得进容器才看得到。它和 `TestRenderedConfigHasNoPlaceholders` 会用 `docker build` 构建 `brickkit-test/portal-user-frontend:test`，没装 Docker 时跳过；`TestClusterFQDNHookBehaviour` 没有 `sh` 时跳过。其余测试只读文件，不需要任何外部。
- 在 BrickKit 仓库根目录，`make test-components` 连同其他自测组件一起跑 vet 与测试，`make demo-images` 构建镜像。

## 设计取舍

- **依赖 erp/backend 而且是强依赖**：页面上所有数据都来自它，没有它这个前端什么也做不了；平台据此注入 `ERP_BACKEND_ENDPOINT`，nginx 模板直接引用它。不需要数据库或缓存：静态文件在镜像里。
- **没有 `configSchema`**：DNS 地址由容器自己从 `/etc/resolv.conf` 取（Docker 与 K8s 的 DNS 地址不同），后端地址由平台按强依赖注入，两者都不该让使用者操心。也没有 `migration`（没有自己的库）、没有 `artifacts`（不对外提供 API）。
- **在平台自测里它负责的验证点**：唯一需要被浏览器访问、因而要映射到宿主机的组件（`expose` 与 `exposePort` 这条路径只有它走）；反向代理拿注入的地址；没有业务代码、只有 nginx 配置与静态文件的组件，验证平台不假设组件是什么语言。
- **后端地址放进变量、每次请求时解析，而不是直接 `proxy_pass ${ERP_BACKEND_ENDPOINT};`**：nginx 对写死在 `proxy_pass` 里的主机名只在启动时解析一次，后端还没起来时 nginx 以 `host not found in upstream` 直接退出，整个前端起不来——而它本该照常发页面、只是 `/api/` 暂时 502。放进变量后启动顺序不再要紧，后端重启换了 IP 也跟得上。代价是必须显式写 `resolver`，而且 nginx 不再自动转发原始 URI，得自己带 `$request_uri`。真容器验证过：停掉 erp/backend 再启动前端，`GET /` 200、`/healthz` 200、`/api/` 502。
- **DNS 地址用官方镜像自带的 `15-local-resolvers.envsh` 生成，不写死**：Docker 的内嵌 DNS（127.0.0.11）与 K8s 的 CoreDNS 是两个完全不同的地址，且各集群不同，写死任何一个另一个环境就废。那个脚本还处理了 IPv6 方括号与多个 nameserver。
- **K8s 下的完整域名在组件里补，而不是让平台注入 FQDN**：nginx 的 `resolver` 不使用 `/etc/resolv.conf` 的 search 域，只拿 nameserver 地址，而 K8s 靠 search 域让裸服务名可解析。裸服务名对其余所有组件都是对的（Go / Python 的解析会走 search 域），只有 nginx 绕开了 search，这是 nginx 特有的行为，该由用 nginx 的组件自己处理。这是真部署到 minikube 才撞出来的。
- **整个 nginx 以非 root 运行**：平台要求容器不以 root 运行，而官方镜像默认 master 进程是 root、只有 worker 降权。所以 pid 文件挪到 `/tmp/nginx.pid`，`/etc/nginx/conf.d` 与 `/var/cache/nginx` 交给 uid 101，监听 8080 而不是 80——这也是 `deployment.port: 8080` 的原因。
- **只声明 `resources.requests`，不声明 `limits`**：理由写在 `component.yaml` 的注释里——"允许涨到多少"是部署方的判断，逐字段合并下组件写了 `limits.cpu` 项目就删不掉了。

## 易错点

| 不许 | 症状 | 原因 |
| --- | --- | --- |
| 直接 `proxy_pass ${ERP_BACKEND_ENDPOINT};`，或删掉 `resolver` / `set $backend` 其中一行 | 后端没起来时 nginx 以 `host not found in upstream` 退出；有 `set` 没 `resolver` 则启动时语法错误；有 `resolver` 没 `set` 等于没改 | 写死在 `proxy_pass` 里的主机名只在启动时解析一次；这两行配套，缺一不可 |
| 把 `resolver` 写死成某个 DNS 地址 | 只在一种环境里能用：写 127.0.0.11 则 K8s 下解析失败，反之 Docker 下失败 | Docker 与 K8s（以及各集群）的 DNS 地址都不同 |
| 删掉 `Dockerfile` 里的 `ENV NGINX_ENTRYPOINT_LOCAL_RESOLVERS=1` | `resolver` 拿到空值，nginx 语法错误起不来 | 官方镜像的探测脚本默认关闭，不打开就直接 `return 0` |
| 让 `/healthz` 走 `location /api/` 之类的规则、或去探后端 | 后端一抖，编排系统就把这个本身正常的前端容器杀掉重启 | 健康检查只查本进程；所以用精确匹配 `location = /healthz` 直接返回常量 |
| 在模板或 `html/index.html` 里写死后端地址（`localhost`、`127.0.0.1`、`erp-backend-1-0-0`……） | 换个部署环境就废；JS 里写死时浏览器直连后端还会撞上跨域，看起来像另一种故障 | 后端地址只能来自平台注入，页面一律用相对路径 |
| 把钩子改名成 `.sh` | 钩子跑了，但后端地址没被改，K8s 里照样 502 | 官方入口对 `.envsh` 是 source、对 `.sh` 是执行；执行时变量留在子 shell 里 |
| 钩子里去掉 `export ERP_BACKEND_ENDPOINT` | 同上：补好的地址进不了模板 | 随后的 envsubst 只看得到导出的变量 |
| 去掉钩子里"只在 search 域带 `.svc.` 时才动手"的判断 | Docker 环境被误伤：把网络名拼到服务名后面反而解析不了 | Docker 里容器也可能有 search 域；`.svc.` 是 K8s 的通用标志 |
| 改端口只改其中一处（模板的 `listen`、`Dockerfile` 的 `EXPOSE`、`component.yaml` 的 `deployment.port`） | 容器起来了但连不上，而每一处单看都没问题 | 三处必须都是 8080；改回 80 还会因非 root 绑不了特权端口而起不来 |
| 去掉 `pid /tmp/nginx.pid` 或对 `/etc/nginx/conf.d`、`/var/cache/nginx` 的 `chown` | 非 root 的 nginx 起不来：pid 写不进 `/var/run/`，模板渲染不进 `conf.d/`，临时目录不可写 | 以 uid 101 运行时这三处默认都不可写 |
| 测试里用别的命令代替 `nginx` 作为入口的第一个参数 | 测试照样通过，但校验的是镜像自带的那份 `default.conf`，什么也没验到 | 官方入口只在 `$1` 为 `nginx` / `nginx-debug` 时才跑 `/docker-entrypoint.d/` 下的钩子（含模板渲染） |

## 改代码前自查

1. 改了 `templates/default.conf.template` 后，有 Docker 的机器上跑过 `go test ./...`，`TestNginxConfigIsValid` 与 `TestRenderedConfigHasNoPlaceholders` 是真跑了而不是跳过？
2. `resolver`、`set $backend`、`proxy_pass $backend$request_uri` 三行是否仍然齐全？
3. `/healthz` 是否仍是精确匹配、直接返回常量？
4. 模板、页面里是否没有任何写死的后端或 DNS 地址？
5. 端口是否仍在模板、`Dockerfile`、`component.yaml` 三处都是 8080？
6. 改了钩子脚本后，`TestClusterFQDNHookBehaviour` 的三种输入（K8s 补全、Docker 不动、已是完整域名不动）是否都还对？
7. 页面新用到的后端接口是否确实在 erp/backend 的 `openapi.json` 里，并同步写进了 `BRICKKIT.md` 的契约索引？
8. `brickkit lint --strict` 是否 0 warnings？

<!-- brickkit:managed:begin lang=zh -->
<!-- 由 brickkit 维护（init、add、remove、upgrade、skills update）：这对标记之间的改动会被覆盖 -->

## BrickKit

这是一个 BrickKit 组件：平台只读 `component.yaml`。它依赖的规则：

- `configSchema` 的键就是代码读的环境变量名。不能用保留名：`COMPONENT_ID`、`COMPONENT_VERSION`、`PORT`、`BRICKKIT_SERVED_MEMBERS`、`BRICKKIT_SERVED_MEMBERS_CONFIG`，以及任何 `*_ENDPOINT`。
- 依赖写精确版本。依赖的地址以 `<ID>_ENDPOINT` 注入；缺席的可选依赖根本没有这个变量，读的时候要带兜底。
- `/healthz` 只查本进程，不查依赖。迁移命令用同一个镜像跑，遇到不认识的参数必须直接失败。
- `BRICKKIT.md` 会随版本进入每个使用它的项目，在那里是脱离仓库单独读的：跟代码一起改，不放相对链接。
- 发版：改 `metadata.version`，提交、推送，`brickkit release`。`brickkit lint` 会检查清单和这些文档——在项目里、在这个目录下跑，只查这个组件（`--all` 查整个项目）。
- 完整规则在 `brickkit-component` 技能里（装了技能的项目或仓库根目录下的 `.claude/skills/brickkit-component/SKILL.md`；`brickkit skills update` 会装上）；参数问 `brickkit <命令> --help`；BrickKit 自己的文档用 `brickkit docs` 看。
<!-- brickkit:managed:end -->
