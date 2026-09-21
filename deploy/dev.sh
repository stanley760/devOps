#!/usr/bin/env sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
COMPOSE_FILE="$ROOT/deploy/docker-compose-dev.yml"
COMMAND="${1:-up}"

compose() {
  docker compose -f "$COMPOSE_FILE" "$@"
}

case "$COMMAND" in
  up)
    # 只启动中间件 + 网关 + 可观测栈；业务服务由本地 go run 启动
    compose up -d mysql redis nacos etcd higress
    compose up -d jaeger prometheus grafana
    echo ""
    echo "中间件与可观测栈已启动。请在宿主机本地启动业务服务："
    echo ""
    echo "  # 终端 1：启动 devops-id（gRPC 发号）"
    echo "  cd devops-id && go run . server"
    echo ""
    echo "  # 终端 2：执行数据库迁移（首次或 schema 变更后）"
    echo "  sh deploy/dev.sh migrate"
    echo ""
    echo "  # 终端 3：启动 devops-system（HTTP 服务）"
    echo "  cd devops-system && go run . server"
    echo ""
    echo "  # 首次使用：复制 .env.example 为 .env 并填入局域网 IP"
    echo "  #   cp devops-system/config/.env.example devops-system/config/.env"
    echo "  #   cp devops-id/config/.env.example devops-id/config/.env"
    echo "  # .env 含 APP_ENV=dev 和 NACOS_REGISTER_IP=192.168.x.x"
    echo ""
    ;;
  migrate)
    # 迁移用一次性容器执行（devops-system 服务在 app profile 下）
    compose build devops-system
    docker compose -f "$COMPOSE_FILE" --profile app run --rm devops-system migrate
    ;;
  down) compose down ;;
  restart) compose restart mysql redis nacos etcd higress ;;
  logs) compose logs -f ;;
  ps) compose ps ;;
  *)
    echo "用法: $0 [up|down|restart|logs|ps|migrate]" >&2
    exit 2
    ;;
esac
