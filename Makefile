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
DEMO_COMPONENTS := demo-hello demo-caller department-tree auth-password-login authorization-rbac
# Python 组件（people-basic）没法在宿主机上直接跑测试：本机未安装 python3-venv。
# 它的测试跑在容器里（Dockerfile 的 test 层），版本固定、可复现。
PY_COMPONENTS := people-basic
# 多版本共存验证需要同一份 hello 的两个 tag
DEMO_HELLO_TAGS := 1.0.0 2.0.0

# 若目标目录下没有 *_test.go，则跳过而不是报错（骨架阶段用）。
define run_tests
	@if [ -d "$(1)" ] && [ -n "$$(find $(1) -name '*_test.go' -print -quit 2>/dev/null)" ]; then \
		echo "▶ go test ./$(1)/..."; \
		$(GO) test $(TESTFLAGS) ./$(1)/...; \
	else \
		echo "⏭  $(1)：暂无测试文件，跳过"; \
	fi
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
lint: ## 静态检查（有 golangci-lint 时用它，否则回退 go vet）
	@if [ -x "$(GOLANGCI)" ]; then \
		echo "▶ golangci-lint run"; \
		$(GOLANGCI) run ./... && (cd market-server && $(GOLANGCI) run ./...); \
	else \
		echo "ℹ️  未安装 golangci-lint（make tools-lint 可安装），回退到 go vet"; \
		$(MAKE) --no-print-directory vet; \
	fi

.PHONY: tidy
tidy: ## go mod tidy（两个 module）
	$(GO) mod tidy
	cd market-server && $(GO) mod tidy

# ---------- 测试 ----------
.PHONY: test
test: test-unit ## 默认测试（= test-unit）

.PHONY: test-unit
test-unit: ## 单元测试（internal + tests/unit）
	@if [ -n "$$(find internal -name '*_test.go' -print -quit 2>/dev/null)" ]; then \
		echo "▶ go test ./internal/..."; $(GO) test $(TESTFLAGS) ./internal/...; \
	else echo "⏭  internal：暂无测试文件，跳过"; fi
	$(call run_tests,tests/unit)

.PHONY: test-integration
test-integration: ## 集成测试
	$(call run_tests,tests/integration)

.PHONY: test-e2e
test-e2e: ## 端到端测试
	$(call run_tests,tests/e2e)

.PHONY: test-boundary
test-boundary: ## 边界测试
	$(call run_tests,tests/boundary)

.PHONY: test-error
test-error: ## 错误处理测试
	$(call run_tests,tests/error)

.PHONY: test-compat
test-compat: ## 兼容性测试（需要 Docker 与 Podman）
	$(call run_tests,tests/compat)

.PHONY: test-security
test-security: ## 安全测试
	$(call run_tests,tests/security)

.PHONY: test-perf
test-perf: ## 性能基准测试
	$(call run_tests,tests/perf)

.PHONY: test-regression
test-regression: ## 回归测试
	$(call run_tests,tests/regression)

.PHONY: test-upgrade
test-upgrade: ## 升级测试（用 tests/e2e 中的 upgrade 用例）
	@if [ -n "$$(find tests/e2e -name '*upgrade*_test.go' -print -quit 2>/dev/null)" ]; then \
		echo "▶ go test ./tests/e2e/... -run Upgrade"; \
		$(GO) test $(TESTFLAGS) ./tests/e2e/... -run Upgrade; \
	else echo "⏭  升级测试：暂无测试文件，跳过"; fi

.PHONY: test-market
test-market: ## 市场后端测试
	@if [ -n "$$(find market-server -name '*_test.go' -print -quit 2>/dev/null)" ]; then \
		echo "▶ market-server go test ./..."; \
		cd market-server && $(GO) test $(TESTFLAGS) ./...; \
	else echo "⏭  market-server：暂无测试文件，跳过"; fi

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
		brickkit-test/people-basic

.PHONY: demo-images
demo-images: ## 构建平台自测组件的容器镜像（Step 11-15 的真实验证靠它们）
	@docker build -q -t brickkit-demo/caller:1.0.0 tests/components/demo-caller
	@docker build -q -t brickkit-demo/department-tree:1.0.0 tests/components/department-tree
	@docker build -q -t brickkit-demo/people-basic:1.0.0 tests/components/people-basic
	@docker build -q -t brickkit-demo/auth-password-login:1.0.0 tests/components/auth-password-login
	@docker build -q -t brickkit-demo/authorization-rbac:1.0.0 tests/components/authorization-rbac
	@for tag in $(DEMO_HELLO_TAGS); do \
		docker build -q -t brickkit-demo/hello:$$tag tests/components/demo-hello; \
	done
	@docker images --format '{{.Repository}}:{{.Tag}}' | grep '^brickkit-demo/' | sort

.PHONY: test-all
test-all: test-unit test-integration test-market test-components test-e2e test-boundary test-error test-security test-perf test-regression ## 执行全部测试（不含 compat）

# 覆盖率政策：
#   internal/ 保持 COVER_MIN 以上；不追求 100%。
#   进程入口（main）、以及对外部引擎的调用（docker / kubectl / MinIO 等）
#   不强求覆盖——为这些代码硬凑覆盖率只会写出没有意义的测试。
COVER_MIN ?= 95

.PHONY: cover
cover: ## 单元测试覆盖率（按函数展开 + 总计）
	$(GO) test -coverprofile=coverage.out ./internal/... 2>/dev/null || true
	@if [ -f coverage.out ]; then $(GO) tool cover -func=coverage.out; else echo "无覆盖率数据"; fi

.PHONY: cover-html
cover-html: ## 生成 coverage.html 便于本地查看
	$(GO) test -coverprofile=coverage.out ./internal/... >/dev/null
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "✅ coverage.html"

.PHONY: cover-check
cover-check: ## 覆盖率门槛检查（低于 COVER_MIN 则失败）
	@$(GO) test -coverprofile=coverage.out ./internal/... >/dev/null
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
	@echo "podman   : $$(podman --version 2>/dev/null || echo 未安装)"
	@echo "python   : $$(python3 --version 2>/dev/null || echo 未安装)"
	@echo "cosign   : $$(cosign version 2>/dev/null | head -1 || echo 未安装)"
	@echo "grpcurl  : $$(grpcurl --version 2>&1 | head -1 || echo 未安装)"
