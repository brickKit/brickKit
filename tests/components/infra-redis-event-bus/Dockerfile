# infra/redis-event-bus 组件镜像。
#
# 宿主机上没有可用的 Python 包管理环境（python3-venv 未安装），
# 因此本组件的**测试也在容器里跑**：`--target test` 那一层带上 pytest。
# 好处是版本固定、可复现，不依赖开发机装了什么。
#
#   构建镜像：docker build -t brickkit-demo/infra-redis-event-bus:1.0.0 .
#   跑测试：  docker build --target test -t event-bus-test . && docker run --rm event-bus-test

FROM python:3.12-slim AS base
WORKDIR /app

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY app ./app
COPY component.yaml openapi.json ./

# ---------- 测试层 ----------
FROM base AS test
COPY requirements-dev.txt ./
RUN pip install --no-cache-dir -r requirements-dev.txt
COPY tests ./tests
# Dockerfile 本身也是被测对象（非 root），拷进来供测试读取
COPY Dockerfile ./
ENV PYTHONPATH=/app
CMD ["python", "-m", "pytest", "-q", "tests"]

# ---------- 运行期镜像 ----------
FROM base AS runtime
# 健康检查跑在容器**内部**，用的必须是镜像里真有的命令（002 §9.6）。
# python:slim 既没有 wget 也没有 curl——不装的话，组件明明跑得好好的，
# 平台却判它 unhealthy，依赖方永远等不到它
RUN apt-get update \
    && apt-get install -y --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/*
# 002 §1.4：容器不得以 root 运行
RUN useradd --uid 10001 --create-home brickkit
USER 10001

ENV PYTHONPATH=/app
ENV PYTHONUNBUFFERED=1
EXPOSE 8080
ENTRYPOINT ["python", "-m", "app.main"]
