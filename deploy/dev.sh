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
  migrate) compose run --rm devops-system migrate ;;
  down) compose down ;;
  restart) compose restart devops-id devops-system higress ;;
  logs) compose logs -f ;;
  ps) compose ps ;;
  *)
    echo "用法: $0 [up|down|restart|logs|ps|migrate]" >&2
    exit 2
    ;;
esac
