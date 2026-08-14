# people/basic 组件镜像。
#
# 宿主机上没有可用的 Python 包管理环境（python3-venv 未安装），
# 因此本组件的**测试也在容器里跑**：`--target test` 那一层带上 pytest 与
# grpcio-tools。好处是版本固定、可复现，不依赖开发机装了什么。
#
#   构建镜像：docker build -t brickkit-demo/people-basic:1.0.0 .
#   跑测试：  docker build --target test -t people-basic-test . && docker run --rm people-basic-test

# ---------- 代码生成：由 proto 生成 gRPC stubs ----------
FROM python:3.12-slim AS codegen
WORKDIR /src

RUN pip install --no-cache-dir grpcio-tools==1.68.1

COPY proto ./proto
# 本组件自己的契约 + 从 department/tree 的 api-contract 产物 vendored 过来的契约
RUN mkdir -p gen && \
    python -m grpc_tools.protoc -I proto \
        --python_out=gen --grpc_python_out=gen --pyi_out=gen \
        proto/people/v1/people.proto proto/vendor/department/v1/department.proto && \
    find gen -type d -exec touch {}/__init__.py \;
# 生成的 grpc stub 用绝对导入（people.v1 / vendor.department.v1），
# 改成包内导入才能作为 gen.* 被引用
RUN find gen -name '*_grpc.py' -exec sed -i \
        -e 's/^from people\.v1 /from gen.people.v1 /' \
        -e 's/^from vendor\.department\.v1 /from gen.vendor.department.v1 /' {} \;
RUN find gen -name '*_pb2.py' -exec sed -i \
        -e 's/^from people\.v1 /from gen.people.v1 /' \
        -e 's/^from vendor\.department\.v1 /from gen.vendor.department.v1 /' {} \;

# ---------- 运行期依赖 ----------
FROM python:3.12-slim AS base
WORKDIR /app

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY app ./app
# 迁移脚本必须进镜像：002 §8.4 要求它和业务代码同镜像同版本
COPY migrations ./migrations
COPY component.yaml openapi.json ./
COPY proto ./proto
COPY --from=codegen /src/gen ./gen

# ---------- 测试层 ----------
FROM base AS test
COPY requirements-dev.txt ./
RUN pip install --no-cache-dir -r requirements-dev.txt
COPY tests ./tests
# Dockerfile 本身也是被测对象（21.10 非 root），拷进来供测试读取
COPY Dockerfile ./
ENV PYTHONPATH=/app
CMD ["python", "-m", "pytest", "-q", "tests"]

# ---------- 运行期镜像 ----------
FROM base AS runtime
# 健康检查跑在容器**内部**，用的必须是镜像里真有的命令（002 §9.6）。
# python:slim 既没有 wget 也没有 curl——不装的话，组件明明跑得好好的，
# 平台却判它 unhealthy，依赖方永远等不到它。真跑起来撞到过。
RUN apt-get update \
    && apt-get install -y --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/*
# 002 §1.4 / 开发计划 21.10：容器不得以 root 运行
RUN useradd --uid 10001 --create-home brickkit
USER 10001

ENV PYTHONPATH=/app
ENV PYTHONUNBUFFERED=1
EXPOSE 8080 9090
ENTRYPOINT ["python", "-m", "app.main"]
