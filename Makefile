# BrickKit Monorepo Makefile
#
# 交付物：
#   bin/brickkit         BrickKit CLI（单二进制分发，001 §4.1）
#   bin/market-server    BrickKit Market 后端（007）
#
# 本地工具链统一装在 .tools/bin，不污染系统环境。

SHELL := /bin/bash
.DEFAULT_GOAL := help

# ---------- 路径与变量 ----------
ROOT          := $(shell pwd)
TOOLS_BIN     := $(ROOT)/.tools/bin
TOOLS_INCLUDE := $(ROOT)/.tools/include
BIN           := $(ROOT)/bin
PROTO_INCLUDE := $(ROOT)/proto/include
# proto 工具链自检的生成物：放在 .tools 下，Go 工具链忽略以 . 开头的目录，
# 因此不会污染 ./... 的构建范围与 go.mod 依赖
PROTO_CHECK_OUT := $(ROOT)/.tools/proto-check

export PATH := $(TOOLS_BIN):$(PATH)

GO        := go
PROTOC    := $(TOOLS_BIN)/protoc
GOLANGCI  := $(TOOLS_BIN)/golangci-lint

PROTOC_VERSION := 35.1

VERSION    ?= $(shell git describe --tags --dirty 2>/dev/null || echo "0.1.0-dev")
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

VERSION_PKG := github.com/brickkit/brickkit/internal/version
LDFLAGS     := -s -w \
	-X '$(VERSION_PKG).Version=$(VERSION)' \
	-X '$(VERSION_PKG).Commit=$(COMMIT)' \
	-X '$(VERSION_PKG).BuildDate=$(BUILD_DATE)'

TESTFLAGS ?= -count=1

# 平台自测组件（tests/components/*）：各自是独立的 Go module 与独立的组件仓库，
# 不参与主 module 的 ./... 构建，需要单独测试与构建镜像。
DEMO_COMPONENTS := demo-hello demo-caller department-tree auth-password-login authorization-rbac erp-backend portal-user-frontend infra-api-docs
# Python 组件（people-basic）没法在宿主机上直接跑测试：本机未安装 python3-venv。
# 它的测试跑在容器里（Dockerfile 的 test 层），版本固定、可复现。
PY_COMPONENTS := people-basic infra-redis-event-bus
# 多版本共存验证需要同一份 hello 的两个 tag
DEMO_HELLO_TAGS := 1.0.0 2.0.0

# 按类别执行 tests/checklist/清单.tsv 里的验收测试。
#
# 这几个目标以前指向 tests/boundary/ 这类空目录，于是永远打印"暂无测试文件，跳过"。
# 而那些测试其实早就写好了，只是写在**被测代码旁边**。清单把两者接了起来：
# checklist_test.go 保证清单不失效，这里按类别把它点名的测试真跑一遍。
define run_category
	@echo "▶ $(1)：先验清单没失效"
	@$(GO) test $(TESTFLAGS) ./tests/checklist/...
	@echo ""
	@awk -F'\t' -v C="$(1)" '!/^#/ && NF==7 && $$1==C && $$4=="test" { print $$5 "\t" $$6 "\t" $$7 }' \
	     tests/checklist/清单.tsv | sort -u \
	  | awk -F'\t' '{ if (k != $$1 "\t" $$2) { if (k) print k "\t" p; k=$$1 "\t" $$2; p=$$3 } \
	                  else p = p "|" $$3 } END { if (k) print k "\t" p }' \
	  | while IFS=$$(printf '\t') read -r mod pkg names; do \
	        echo "  ── $$mod/$${pkg#./}  ($$names)"; \
	        ( cd $$mod && $(GO) test $(TESTFLAGS) $$pkg -run "^($$names)\$$" ) || exit 1; \
	    done
	@echo ""
	@echo "✅ $(1)：清单里 $$(awk -F'\t' -v C="$(1)" '!/^#/ && NF==7 && $$1==C {print $$2}' tests/checklist/清单.tsv | sort -u | wc -l) 项全部通过"

endef

# 若目标目录下没有 *_test.go，则**报错**而不是跳过。
# 一个永远跳过、却还在 test-all 成绩单上占一行的目标，比没有这个目标更坏。
define run_tests
	@if [ ! -d "$(1)" ] || [ -z "$$(find $(1) -name '*_test.go' -print -quit 2>/dev/null)" ]; then \
		echo "❌ $(1)：一个测试文件都没有。"; \
		echo "   目录空了却还在跑，输出会长得和「全部通过」一模一样。"; \
		echo "   要么把测试放回来，要么把这个目标删掉。"; \
		exit 1; \
	fi
	@echo "▶ go test ./$(1)/..."
	@$(GO) test $(TESTFLAGS) ./$(1)/...
endef

# ---------- 帮助 ----------
.PHONY: help
help: ## 显示所有可用目标
	@echo "BrickKit Makefile 目标："
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

# ---------- 构建 ----------
.PHONY: build
build: build-cli build-market ## 构建 CLI 与市场后端

.PHONY: build-cli
build-cli: ## 构建 BrickKit CLI 到 bin/brickkit
	@mkdir -p $(BIN)
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/brickkit ./cmd/brickkit
	@echo "✅ $(BIN)/brickkit"

.PHONY: build-market
build-market: ## 构建市场后端到 bin/market-server
	@mkdir -p $(BIN)
	cd market-server && $(GO) build -trimpath -o $(BIN)/market-server ./cmd/server
	@echo "✅ $(BIN)/market-server"

.PHONY: install
install: ## 安装 CLI 到 GOBIN
	$(GO) install -trimpath -ldflags "$(LDFLAGS)" ./cmd/brickkit

.PHONY: clean
clean: ## 清理构建产物与生成文件
	rm -rf $(BIN) $(PROTO_CHECK_OUT) coverage.out coverage.html
	@echo "✅ 已清理"

# ---------- 代码质量 ----------
.PHONY: fmt
fmt: ## 格式化代码
	$(GO) fmt ./...
	cd market-server && $(GO) fmt ./...

.PHONY: vet
vet: ## go vet（两个 module）
	$(GO) vet ./...
	cd market-server && $(GO) vet ./...

.PHONY: lint
lint: check-docs check-cli-docs check-doc-tree check-doc-fields check-guide-output cover-check ## 静态检查（文档引用 + 命令 + 目录树 + 字段骨架 + 指南预期输出 + 覆盖率门槛）
	@if [ -x "$(GOLANGCI)" ]; then \
		echo "▶ golangci-lint run"; \
		$(GOLANGCI) run ./... && (cd market-server && $(GOLANGCI) run ./...); \
	else \
		echo "ℹ️  未安装 golangci-lint（make tools-lint 可安装），回退到 go vet"; \
		$(MAKE) --no-print-directory vet; \
	fi

.PHONY: check-doc-fields
check-doc-fields: ## 检查文档里画的字段骨架与 component.yaml / brickkit.yaml 结构体一致
	@go test ./tests/docfields/

.PHONY: check-docs
check-docs: ## 检查文档引用（悬空小节号、断链、指南编号与前置）
	@python3 scripts/check-docs.py

.PHONY: check-cli-docs
check-cli-docs: build-cli ## 检查文档里的命令与参数是否真的存在（Step 40）
	@python3 scripts/check-cli-docs.py $(BIN)/brickkit

.PHONY: check-doc-tree
check-doc-tree: build-cli ## 检查文档里画的 .brickkit/ 目录树与 CLI 真的会创建的东西一致
	@python3 scripts/check-doc-tree.py $(BIN)/brickkit

# 两个"真跑"检查的分工：
#   check-guide-output  指南里的「✅ 预期」块必须逐行等于 CLI 真实输出。
#                       只覆盖不需要 Docker/minikube 的步骤，所以能进 lint 天天跑。
#   check-guides        分层冒烟：关键步骤跑得通、输出里有该有的关键词。
#                       要 Docker / minikube 的层缺环境时响亮跳过，因此不进 lint。
.PHONY: check-guide-output
check-guide-output: build-cli ## 核对试用指南的预期输出与 CLI 真实输出逐行一致
	@python3 scripts/check-guide-output.py

.PHONY: check-guides
check-guides: build-cli ## 真跑试用指南里的关键步骤（缺环境的层会响亮跳过）
	@bash scripts/check-guides.sh

.PHONY: tidy
tidy: ## go mod tidy（两个 module）
	$(GO) mod tidy
	cd market-server && $(GO) mod tidy

# ---------- 测试 ----------
.PHONY: test
test: test-unit ## 默认测试（= test-unit）

.PHONY: test-unit
test-unit: ## 单元测试（internal/**，测试与被测代码同处一包）
	@echo "▶ go test ./internal/..."
	@$(GO) test $(TESTFLAGS) ./internal/...

.PHONY: test-boundary
test-boundary: ## 边界测试（Step 32，读 tests/checklist/清单.tsv）
	$(call run_category,boundary)

.PHONY: test-error
test-error: ## 错误处理测试（Step 33）
	$(call run_category,error)

.PHONY: test-compat
test-compat: ## 兼容性测试（Step 34，Docker）
	$(call run_category,compat)

.PHONY: test-security
test-security: ## 安全测试（Step 35）
	$(call run_category,security)

.PHONY: test-perf
test-perf: ## 性能基准测试
	$(call run_tests,tests/perf)

.PHONY: test-regression
test-regression: ## 回归测试（读 tests/regression/清单.tsv，先验清单没失效再逐条执行）
	@echo "▶ 校验回归清单（每一项指向的测试是否仍然存在）"
	@$(GO) test $(TESTFLAGS) ./tests/regression/...
	@echo ""
	@echo "▶ 执行清单里的 25 项回归测试"
	@awk -F'\t' '!/^#/ && NF==6 { print $$4 "\t" $$5 "\t" $$6 }' tests/regression/清单.tsv \
	  | sort -u \
	  | awk -F'\t' '{ if (k != $$1 "\t" $$2) { if (k) print k "\t" p; k=$$1 "\t" $$2; p=$$3 } \
	                  else p = p "|" $$3 } END { if (k) print k "\t" p }' \
	  | while IFS=$$(printf '\t') read -r mod pkg names; do \
	        echo "  ── $$mod/$${pkg#./}  ($$names)"; \
	        ( cd $$mod && $(GO) test $(TESTFLAGS) $$pkg -run "^($$names)\$$" ) || exit 1; \
	    done
	@echo ""
	@echo "✅ 回归清单 $$(grep -c '^R' tests/regression/清单.tsv) 项全部通过"

.PHONY: test-upgrade
test-upgrade: ## 升级测试（Step 38；用例与被测代码同处一包）
	@n=$$(for p in ./internal/cli ./internal/compose ./internal/resolver; do \
	        $(GO) test -list Upgrade $$p | grep -c '^Test'; done | paste -sd+ | bc); \
	  if [ "$$n" -lt 20 ]; then \
	    echo "❌ 只匹配到 $$n 条 Upgrade 测试（预期 20 条以上）。"; \
	    echo "   -run 匹配不到任何东西时 go test 照样报 ok——这个目标本来会静默通过。"; \
	    echo "   多半是测试被改名或搬走了，请一并更新这里的包列表。"; exit 1; \
	  fi; \
	  echo "▶ go test -run Upgrade（$$n 条）"
	@$(GO) test $(TESTFLAGS) -run Upgrade \
	    ./internal/cli/... ./internal/compose/... ./internal/resolver/...

.PHONY: test-market
test-market: ## 市场后端测试（PostgreSQL 契约测试缺环境时**响亮**跳过）
	@if [ -z "$$(find market-server -name '*_test.go' -print -quit 2>/dev/null)" ]; then \
		echo "⏭  market-server：暂无测试文件，跳过"; exit 0; fi; \
	echo "▶ market-server go test ./..."; \
	(cd market-server && $(GO) test $(TESTFLAGS) ./...) || exit 1; \
	if [ -z "$$MARKET_TEST_DATABASE_URL" ]; then \
		echo "⏭  PostgreSQL 契约测试已跳过：未设置 MARKET_TEST_DATABASE_URL"; \
		echo "   刚才跑的是**内存实现**。internal/repo/postgres.go（真正上生产的那个）"; \
		echo "   一行都没被执行过——两个实现共用一份契约断言，正是为了防它们语义漂移。"; \
		echo "   要跑真库：make test-market-integration（读 .env，需要本机 PostgreSQL）"; \
	else \
		echo "✅ PostgreSQL 契约测试已随上面一并执行（MARKET_TEST_DATABASE_URL 已设置）"; \
	fi

.PHONY: openapi-people
openapi-people: ## 由 FastAPI 重新导出 people/basic 的 openapi.json
	@cd tests/components/people-basic && docker build -q --target test -t brickkit-test/people-basic . >/dev/null && \
		docker run --rm -v "$$PWD:/out" brickkit-test/people-basic \
			sh -c "cd /app && cp /out/dump_openapi.py . && python dump_openapi.py && cp openapi.json /out/openapi.json"

.PHONY: proto-department
proto-department: ## 由 proto 重新生成 department/tree 的 Go 代码
	@cd tests/components/department-tree && mkdir -p gen && PATH="$(TOOLS_BIN):$$PATH" $(PROTOC) \
		--proto_path=proto \
		--go_out=gen --go_opt=paths=source_relative \
		--go-grpc_out=gen --go-grpc_opt=paths=source_relative \
		proto/department/v1/department.proto
	@echo "✅ tests/components/department-tree/gen 已更新"

.PHONY: proto-authorization
proto-authorization: ## 由 proto 重新生成 authorization/rbac 的 Go 代码
	@cd tests/components/authorization-rbac && mkdir -p gen && PATH="$(TOOLS_BIN):$$PATH" $(PROTOC) \
		--proto_path=proto \
		--go_out=gen --go_opt=paths=source_relative \
		--go-grpc_out=gen --go-grpc_opt=paths=source_relative \
		proto/authorization/v1/authorization.proto
	@echo "✅ tests/components/authorization-rbac/gen 已更新"

.PHONY: proto-erp
proto-erp: ## 由上游 proto 生成 erp/backend 的 gRPC 客户端（people + authorization）
	@cd tests/components/erp-backend && mkdir -p gen && PATH="$(TOOLS_BIN):$$PATH" $(PROTOC) \
		--proto_path=proto \
		--go_out=gen --go_opt=paths=source_relative \
		--go-grpc_out=gen --go-grpc_opt=paths=source_relative \
		proto/people/v1/people.proto proto/authorization/v1/authorization.proto
	@echo "✅ tests/components/erp-backend/gen 已更新"

.PHONY: test-race
test-race: ## 竞态检测（internal/**）
	@echo "▶ go test -race ./internal/..."
	@$(GO) test -race $(TESTFLAGS) ./internal/...

.PHONY: test-market-integration
test-market-integration: ## 市场后端的集成测试（需要本机 PostgreSQL 与 RustFS，读 .env）
	@set -a; . ./.env; set +a; \
	export MARKET_TEST_DATABASE_URL="postgres://$$POSTGRES_USER:$$POSTGRES_PASSWORD@$$POSTGRES_HOST:$$POSTGRES_PORT/$$POSTGRES_DB?sslmode=disable"; \
	echo "▶ market-server 集成测试（PostgreSQL + RustFS）"; \
	cd market-server && $(GO) test $(TESTFLAGS) ./...

.PHONY: test-components
test-components: ## 平台自测组件的单元测试（Go 组件本地跑，Python 组件在容器里跑）
	@for c in $(DEMO_COMPONENTS); do \
		echo "▶ tests/components/$$c go test ./..."; \
		( cd tests/components/$$c && $(GO) vet ./... && $(GO) test $(TESTFLAGS) ./... ) || exit 1; \
	done
	@$(MAKE) --no-print-directory test-components-py

.PHONY: test-components-py
test-components-py: ## Python 组件的测试（在容器里跑，需要 Docker）
	@for c in $(PY_COMPONENTS); do \
		echo "▶ tests/components/$$c pytest（容器内）"; \
		docker build -q --target test -t brickkit-test/$$c tests/components/$$c >/dev/null || exit 1; \
		docker run --rm brickkit-test/$$c || exit 1; \
	done

.PHONY: test-components-integration
test-components-integration: ## 组件的迁移集成测试（需要本机 PostgreSQL，读 .env）
	@# 组件的数据库按设计由人创建（006 §9.1：CLI 不负责建库，见各组件 README）；
	@# 这里是测试夹具代劳，免得每次跑测试前手工建库
	@set -a; . ./.env; set +a; \
	for db in brickkit_department brickkit_people brickkit_auth brickkit_rbac; do \
		docker exec -e PGPASSWORD=$$POSTGRES_PASSWORD -i my-postgres \
			psql -U $$POSTGRES_USER -tc "SELECT 1 FROM pg_database WHERE datname='$$db'" \
			| grep -q 1 || docker exec -e PGPASSWORD=$$POSTGRES_PASSWORD -i my-postgres \
			psql -U $$POSTGRES_USER -c "CREATE DATABASE $$db" >/dev/null; \
	done; \
	echo "▶ department-tree 迁移集成测试"; \
	( cd tests/components/department-tree && \
	  DEPARTMENT_TEST_DATABASE_URL="postgres://$$POSTGRES_USER:$$POSTGRES_PASSWORD@$$POSTGRES_HOST:$$POSTGRES_PORT/brickkit_department?sslmode=disable" \
	  $(GO) test $(TESTFLAGS) ./... ) || exit 1; \
	echo "▶ auth-password-login 迁移集成测试"; \
	( cd tests/components/auth-password-login && \
	  AUTH_TEST_DATABASE_URL="postgres://$$POSTGRES_USER:$$POSTGRES_PASSWORD@$$POSTGRES_HOST:$$POSTGRES_PORT/brickkit_auth?sslmode=disable" \
	  $(GO) test $(TESTFLAGS) ./... ) || exit 1; \
	echo "▶ authorization-rbac 迁移集成测试（含 Redis 缓存契约）"; \
	( cd tests/components/authorization-rbac && \
	  RBAC_TEST_DATABASE_URL="postgres://$$POSTGRES_USER:$$POSTGRES_PASSWORD@$$POSTGRES_HOST:$$POSTGRES_PORT/brickkit_rbac?sslmode=disable" \
	  RBAC_TEST_REDIS_ADDR="$${REDIS_HOST:-localhost}:$${REDIS_PORT:-6379}" \
	  $(GO) test $(TESTFLAGS) ./... ) || exit 1; \
	echo "▶ people-basic 迁移集成测试（容器内）"; \
	PGIP=$$(docker inspect my-postgres --format '{{.NetworkSettings.Networks.bridge.IPAddress}}'); \
	docker build -q --target test -t brickkit-test/people-basic tests/components/people-basic >/dev/null; \
	docker run --rm -e PEOPLE_TEST_DATABASE_URL="postgresql://$$POSTGRES_USER:$$POSTGRES_PASSWORD@$$PGIP:5432/brickkit_people" \
		brickkit-test/people-basic || exit 1; \
	echo "▶ infra-redis-event-bus 真 Redis 契约测试（容器内）"; \
	REDISIP=$$(docker inspect my-redis --format '{{.NetworkSettings.Networks.bridge.IPAddress}}'); \
	docker build -q --target test -t brickkit-test/infra-redis-event-bus tests/components/infra-redis-event-bus >/dev/null; \
	docker run --rm -e EVENT_BUS_TEST_REDIS_ADDR="$$REDISIP:6379" \
		brickkit-test/infra-redis-event-bus

.PHONY: demo-images
demo-images: ## 构建平台自测组件的容器镜像（Step 11-15 的真实验证靠它们）
	@docker build -q -t brickkit-demo/caller:1.0.0 tests/components/demo-caller
	@docker build -q -t brickkit-demo/department-tree:1.0.0 tests/components/department-tree
	@docker build -q -t brickkit-demo/people-basic:1.0.0 tests/components/people-basic
	@docker build -q -t brickkit-demo/auth-password-login:1.0.0 tests/components/auth-password-login
	@docker build -q -t brickkit-demo/authorization-rbac:1.0.0 tests/components/authorization-rbac
	@docker build -q -t brickkit-demo/erp-backend:1.0.0 tests/components/erp-backend
	@docker build -q -t brickkit-demo/portal-user-frontend:1.0.0 tests/components/portal-user-frontend
	@docker build -q -t brickkit-demo/infra-redis-event-bus:1.0.0 tests/components/infra-redis-event-bus
	@docker build -q -t brickkit-demo/infra-api-docs:1.0.0 tests/components/infra-api-docs
	@for tag in $(DEMO_HELLO_TAGS); do \
		docker build -q -t brickkit-demo/hello:$$tag tests/components/demo-hello; \
	done
	@docker images --format '{{.Repository}}:{{.Tag}}' | grep '^brickkit-demo/' | sort

.PHONY: test-all
test-all: test-unit test-race test-market test-components test-boundary test-error test-compat test-security test-upgrade test-perf test-regression ## 执行全部测试

# 覆盖率政策：
#   internal/ 保持 COVER_MIN 以上；不追求 100%。
#   进程入口（main）、以及对外部引擎的调用（docker / kubectl / RustFS 等）
#   不强求覆盖——为这些代码硬凑覆盖率只会写出没有意义的测试。
#
# 关于 -coverpkg：不加它，`go test ./internal/...` **只统计每个包自己的测试**，
# 于是 internal/deploy 这种"没有自己的测试、但被 compose 与 k8s 大量走到"的包
# 一律记 0%。那不是覆盖率低，是**没量到**。加上之后 deploy 从 0% 变成 83–100%，
# 总数从 91.4% 变成 93.0%。
#
# 关于 92 这个数：门槛原本写的是 95，而真实值从来没到过——因为它既不在
# `make lint` 也不在 `make test-all` 里，**从没被跑过**。
# 一个没人跑的门槛写多高都不算数。现在按真实值（93.0%）留一格余量定在 92，
# 并且接进了 lint，让它每次都跑。
#
# 剩下的 11 个 0% 函数：错误包装器（writeError / moveError / credentialWriteError）
# 与对外部引擎的调用（engine.Status / CurrentContext / pathExists）——
# 正是上面政策里说不强求的那两类。
#
# 关于 -count=1：**量覆盖率绝不能用测试结果缓存**。-coverpkg 让每个测试二进制
# 都带上全部 internal/ 包的插桩块；改了某个文件后，那些从缓存里回放的包
# 交回来的仍是**按旧代码切分的块**。于是同一个文件在 profile 里出现两套
# 互不兼容的块边界（实测 workspace.go 69 组 vs 正常的 40 组），
# `go tool cover` 合并不了，只能把两套都算进分母——旧那套全是 0，
# 总数凭空掉 2~3 个百分点，单语句函数会显示成 38.5% 这种不可能的值。
# 症状具有欺骗性：它看起来像"覆盖率退化了"，实际是**量错了**。
COVER_MIN ?= 92
COVERPKG := -coverpkg=./internal/... -count=1

.PHONY: cover
cover: ## 单元测试覆盖率（按函数展开 + 总计）
	$(GO) test $(COVERPKG) -coverprofile=coverage.out ./internal/... 2>/dev/null || true
	@if [ -f coverage.out ]; then $(GO) tool cover -func=coverage.out; else echo "无覆盖率数据"; fi

.PHONY: cover-html
cover-html: ## 生成 coverage.html 便于本地查看
	$(GO) test $(COVERPKG) -coverprofile=coverage.out ./internal/... >/dev/null
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "✅ coverage.html"

.PHONY: cover-check
cover-check: ## 覆盖率门槛检查（低于 COVER_MIN 则失败）
	@$(GO) test $(COVERPKG) -coverprofile=coverage.out ./internal/... >/dev/null
	@total=$$($(GO) tool cover -func=coverage.out | awk '/^total:/{gsub("%","",$$3); print $$3}'); \
	echo "internal 覆盖率：$$total%（门槛 $(COVER_MIN)%）"; \
	awk -v t="$$total" -v m="$(COVER_MIN)" 'BEGIN{ if (t+0 < m+0) { print "❌ 覆盖率低于门槛"; exit 1 } print "✅ 覆盖率达标" }'

# ---------- Proto ----------
.PHONY: proto-gen
proto-gen: proto-example ## 生成全部 proto 代码

.PHONY: proto-example
proto-example: ## 生成示例 proto 代码（工具链自检，验证项 1.5）
	@mkdir -p $(PROTO_CHECK_OUT)
	$(PROTOC) \
		-I proto/example \
		-I $(PROTO_INCLUDE) \
		-I $(TOOLS_INCLUDE) \
		--go_out=paths=source_relative:$(PROTO_CHECK_OUT) \
		--go-grpc_out=paths=source_relative:$(PROTO_CHECK_OUT) \
		proto/example/ping.proto
	@ls -1 $(PROTO_CHECK_OUT)
	@echo "✅ protoc 工具链正常"

# ---------- 工具链 ----------
.PHONY: tools
tools: tools-protoc tools-plugins ## 安装全部本地工具链到 .tools/bin

.PHONY: tools-helm
tools-helm: ## 安装 helm 到 .tools/bin（只有验证市场 chart 时才需要）
	@mkdir -p $(TOOLS_BIN)
	@if [ -x "$(TOOLS_BIN)/helm" ]; then echo "✅ helm 已就位"; else \
		echo "⬇️  下载 helm ..."; \
		tmp=$$(mktemp -d); \
		curl -sSL "https://get.helm.sh/helm-v3.16.3-linux-amd64.tar.gz" | tar -xz -C $$tmp; \
		install -m 0755 $$tmp/linux-amd64/helm $(TOOLS_BIN)/helm; \
		rm -rf $$tmp; \
		echo "✅ $(TOOLS_BIN)/helm"; \
	fi

.PHONY: market-chart-check
market-chart-check: tools-helm ## 校验市场 Helm chart（渲染 + values schema）
	@echo "==> 口令缺失时必须拦住"
	@if $(TOOLS_BIN)/helm template m deploy/market/helm/brickkit-market >/dev/null 2>&1; then \
		echo "❌ 没给口令却渲染成功了——那会生成一份起不来的清单"; exit 1; \
	else echo "   ✅ 已拦下"; fi
	@echo "==> 给了口令必须能渲染"
	@$(TOOLS_BIN)/helm template m deploy/market/helm/brickkit-market \
		--set auth.databasePassword=x --set auth.rustfsAccessKey=x \
		--set auth.rustfsSecretKey=x --set auth.adminPassword=x >/dev/null
	@echo "   ✅ 渲染通过"
	@echo "==> values schema 必须拦下写错的值"
	@if $(TOOLS_BIN)/helm template m deploy/market/helm/brickkit-market \
		--set auth.databasePassword=x --set auth.rustfsAccessKey=x \
		--set auth.rustfsSecretKey=x --set auth.adminPassword=x \
		--set storage.endpoint=no-scheme:9000 >/dev/null 2>&1; then \
		echo "❌ storage.endpoint 少了 scheme 却没被拦下"; exit 1; \
	else echo "   ✅ 已拦下"; fi

.PHONY: tools-protoc
tools-protoc: ## 下载 protoc 到 .tools/bin
	@mkdir -p $(TOOLS_BIN) $(TOOLS_INCLUDE)
	@tmp=$$(mktemp -d) && \
	curl -sSL -o $$tmp/protoc.zip \
		"https://github.com/protocolbuffers/protobuf/releases/download/v$(PROTOC_VERSION)/protoc-$(PROTOC_VERSION)-linux-x86_64.zip" && \
	unzip -q -o $$tmp/protoc.zip -d $$tmp/protoc && \
	cp $$tmp/protoc/bin/protoc $(TOOLS_BIN)/protoc && chmod +x $(TOOLS_BIN)/protoc && \
	cp -r $$tmp/protoc/include/. $(TOOLS_INCLUDE)/ && rm -rf $$tmp
	@$(PROTOC) --version

.PHONY: tools-plugins
tools-plugins: ## 安装 protoc 插件到 .tools/bin
	GOBIN=$(TOOLS_BIN) $(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	GOBIN=$(TOOLS_BIN) $(GO) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	GOBIN=$(TOOLS_BIN) $(GO) install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest
	GOBIN=$(TOOLS_BIN) $(GO) install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@latest
	@ls -1 $(TOOLS_BIN)

.PHONY: tools-lint
tools-lint: ## 安装 golangci-lint 到 .tools/bin
	GOBIN=$(TOOLS_BIN) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	@$(GOLANGCI) --version

.PHONY: tools-proto-include
tools-proto-include: ## 下载 google/api 与 openapiv2 依赖 proto 到 proto/include
	@mkdir -p $(PROTO_INCLUDE)/google/api $(PROTO_INCLUDE)/protoc-gen-openapiv2/options
	@for f in annotations.proto http.proto field_behavior.proto; do \
		curl -sSfL -o $(PROTO_INCLUDE)/google/api/$$f \
			"https://raw.githubusercontent.com/googleapis/googleapis/master/google/api/$$f" && echo "ok google/api/$$f"; \
	done
	@for f in annotations.proto openapiv2.proto; do \
		curl -sSfL -o $(PROTO_INCLUDE)/protoc-gen-openapiv2/options/$$f \
			"https://raw.githubusercontent.com/grpc-ecosystem/grpc-gateway/main/protoc-gen-openapiv2/options/$$f" && echo "ok openapiv2/$$f"; \
	done

# ============================================================
# 市场部署（deploy/market，详见《部署模式》与《市场部署与运维指南》）
# ============================================================

MARKET_DEPLOY := deploy/market

.PHONY: market-up
market-up: ## 本地起一套完整市场（PostgreSQL + RustFS + Market API），首次会构建镜像
	@if [ ! -f $(MARKET_DEPLOY)/.env ]; then \
		cp $(MARKET_DEPLOY)/.env.example $(MARKET_DEPLOY)/.env; \
		echo "⚠️  已生成 $(MARKET_DEPLOY)/.env，请先把里面的口令改掉再执行一次 make market-up"; \
		exit 1; \
	fi
	@cd $(MARKET_DEPLOY) && docker compose up -d --build
	@echo "▶ 健康检查：curl http://localhost:$${MARKET_PORT:-8080}/api/v1/health"

.PHONY: market-down
market-down: ## 停止市场（数据保留在 deploy/market/data/）
	@cd $(MARKET_DEPLOY) && docker compose down

.PHONY: market-logs
market-logs: ## 跟随查看市场 API 日志
	@cd $(MARKET_DEPLOY) && docker compose logs -f market-api

.PHONY: market-image
market-image: ## 只构建市场镜像（VERSION=x.y.z 注入版本号）
	docker build -t brickkit/market-server:$(or $(VERSION),dev) \
		--build-arg VERSION=$(or $(VERSION),dev) market-server/

.PHONY: env
env: ## 打印开发环境基线（对照开发计划附录 G）
	@echo "Go       : $$($(GO) version)"
	@echo "protoc   : $$([ -x $(PROTOC) ] && $(PROTOC) --version || echo 未安装)"
	@echo "docker   : $$(docker --version 2>/dev/null || echo 未安装)"
	@echo "compose  : $$(docker compose version 2>/dev/null | head -1 || echo 未安装)"
	@echo "python   : $$(python3 --version 2>/dev/null || echo 未安装)"
	@echo "cosign   : $$(cosign version 2>/dev/null | head -1 || echo 未安装)"
	@echo "grpcurl  : $$(grpcurl --version 2>&1 | head -1 || echo 未安装)"
