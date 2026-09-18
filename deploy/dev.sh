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
    compose build
    compose up -d mysql redis nacos etcd
    compose run --rm devops-system migrate
    compose up -d devops-id devops-system
    compose up -d higress
    ;;
  local-up)
    compose stop devops-system
    compose build devops-id
    compose up -d mysql redis nacos etcd
    compose up -d devops-id higress
    echo "基础设施和 Higress 已启动。请在 IDE 中启动 devops-system，并设置 APP_ENV=local。"
    echo "本地服务必须设置 NACOS_REGISTER_IP 为宿主机局域网 IPv4 地址。"
    ;;
  local-down)
    compose stop higress devops-id mysql redis nacos etcd
    ;;
  migrate) compose run --rm devops-system migrate ;;
  down) compose down ;;
  restart) compose restart devops-id devops-system higress ;;
  logs) compose logs -f ;;
  ps) compose ps ;;
  *)
    echo "用法: $0 [up|local-up|down|local-down|restart|logs|ps|migrate]" >&2
    exit 2
    ;;
esac
