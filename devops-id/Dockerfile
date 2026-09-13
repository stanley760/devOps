# devops-id 发号服务镜像（多阶段构建：静态编译 + alpine 运行时）。
#
# 构建上下文必须是仓库根：go.work 工作区使本模块引用 devops-infra，
#   docker build -f devops-id/Dockerfile -t devops-id:dev .
#
# 镜像内配置仅为占位（config/config.yaml），实际环境差异通过
# APP_ENV 合并环境文件覆盖（挂载 prod.yaml，见 deploy/）。
FROM golang:1.26-alpine AS builder

ARG VERSION=dev
# 默认走国内代理（与宿主机 go env GOPROXY 一致），海外构建可 --build-arg GOPROXY= 覆盖
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}

WORKDIR /src

# 依赖描述先于源码拷贝，go.sum 不变时命中层缓存
COPY go.work go.work.sum ./
COPY devops-infra/go.mod devops-infra/go.sum devops-infra/
COPY devops-id/go.mod devops-id/go.sum devops-id/
COPY devops-system/go.mod devops-system/go.sum devops-system/

# go.work 工作区含全部三个模块，预下载需逐模块执行
RUN --mount=type=cache,target=/go/pkg/mod \
    cd devops-infra && go mod download \
    && cd ../devops-id && go mod download \
    && cd ../devops-system && go mod download

COPY devops-infra/ devops-infra/
COPY devops-id/ devops-id/
COPY devops-system/ devops-system/

# CGO_ENABLED=0 静态编译；-trimpath 去掉本机路径
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd devops-id && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath \
        -ldflags "-s -w -X main.version=${VERSION}" \
        -o /out/devops-id .

FROM alpine:3.22

RUN apk add --no-cache tzdata ca-certificates \
    && addgroup -g 10001 -S app && adduser -u 10001 -S -G app app

WORKDIR /app
COPY --from=builder /out/devops-id /usr/local/bin/devops-id
COPY --chown=app:app devops-id/config/ /app/config/
RUN mkdir -p /app/logs /app/data && chown app:app /app/logs /app/data

USER app
ENV TZ=Asia/Shanghai \
    APP_ENV=prod
EXPOSE 9090

# gRPC 无 HTTP 端点，容器级健康检查退化为 TCP 探活；
# K8s 请用原生 grpc 探针（见 deploy/k8s/devops.yaml）
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
    CMD nc -z 127.0.0.1 9090 || exit 1

# 优雅退出由程序自身处理（revoke lease 立即释放槽位），务必用 exec 形式传信号
ENTRYPOINT ["devops-id"]
CMD ["server"]
