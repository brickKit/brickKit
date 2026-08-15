# portal/user-frontend 组件镜像。
#
# 基于官方 nginx:alpine，但**以非 root 运行**（002 §1.4）。
# nginx 官方镜像默认 master 进程是 root，只有 worker 降权；要整个进程非 root，
# 需要额外做三件事，下面逐条注明——这也是这个组件想演示的东西之一。
FROM nginx:1.27-alpine

# nginx:alpine 里已有 uid=101 的 nginx 用户，直接用它
ARG UID=101

COPY html/ /usr/share/nginx/html/
COPY templates/ /etc/nginx/templates/

# 打开官方镜像自带的 resolver 探测：它会从 /etc/resolv.conf 生成
# NGINX_LOCAL_RESOLVERS，供模板里的 resolver 指令使用。
# 这个开关默认是关的（见镜像里的 /docker-entrypoint.d/15-local-resolvers.envsh）
ENV NGINX_ENTRYPOINT_LOCAL_RESOLVERS=1

# ① pid 文件挪到 /tmp：默认的 /var/run/nginx.pid 非 root 写不了
# ② 入口脚本要把模板渲染进 /etc/nginx/conf.d/，那个目录得可写
# ③ nginx 的临时目录（proxy/fastcgi/... ）同理
RUN sed -i 's|^pid .*|pid /tmp/nginx.pid;|' /etc/nginx/nginx.conf \
 && chown -R "$UID":0 /etc/nginx/conf.d /var/cache/nginx \
 && chmod -R g+w /etc/nginx/conf.d /var/cache/nginx

USER 101
# 与 component.yaml 的 deployment.port 一致。
# 非 root 进程绑不了 1024 以下的端口，所以不是 80
EXPOSE 8080
