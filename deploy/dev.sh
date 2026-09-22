#!/usr/bin/env sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
COMPOSE_FILE="$ROOT/deploy/docker-compose-dev.yml"
COMMAND="${1:-up}"
LOG_DIR="$ROOT/deploy/.dev-logs"

compose() {
  docker compose -f "$COMPOSE_FILE" "$@"
}

# 后台 `go run` 启动一个业务服务，日志与 PID 记录在 deploy/.dev-logs/ 下。
start_service() {
  name="$1"; dir="$2"
  mkdir -p "$LOG_DIR"
  pidfile="$LOG_DIR/$name.pid"
  if [ -f "$pidfile" ] && kill -0 "$(cat "$pidfile")" 2>/dev/null; then
    echo "  $name 已在运行（PID $(cat "$pidfile")），跳过"
    return
  fi
  (
    cd "$ROOT/$dir" || exit 1
    exec go run . server
  ) >"$LOG_DIR/$name.log" 2>&1 &
  echo $! >"$pidfile"
  echo "  $name 已后台启动（PID $!）：tail -f deploy/.dev-logs/$name.log"
}

stop_service() {
  name="$1"
  pidfile="$LOG_DIR/$name.pid"
  if [ -f "$pidfile" ]; then
    pid="$(cat "$pidfile")"
    if kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
      echo "  已停止 $name（PID $pid）"
    else
      echo "  $name 未在运行"
    fi
    rm -f "$pidfile"
  else
    echo "  $name 未在运行"
  fi
}

start_services() {
  echo ""
  echo "正在启动本地业务服务..."
  start_service devops-id devops-id
  # devops-system 依赖 devops-id 发号，等它先就绪
  sleep 3
  start_service devops-system devops-system
  echo ""
  echo "首次运行或 schema 变更后，请先执行数据库迁移：sh deploy/dev.sh migrate"
  echo "首次使用前需复制 .env.example 为 .env 并填入局域网 IP（NACOS_REGISTER_IP）。"
}

case "$COMMAND" in
  up)
    # 启动中间件 + 网关 + 可观测栈，随后自动拉起本地业务服务
    compose up -d mysql redis nacos etcd higress
    compose up -d jaeger prometheus grafana
    echo ""
    echo "中间件与可观测栈已启动。"
    start_services
    ;;
  app)
    # 仅启动/补起本地业务服务，不动容器
    start_services
    ;;
  stop)
    # 停止本地业务服务（容器不受影响）
    stop_service devops-system
    stop_service devops-id
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
    echo "用法: $0 [up|app|stop|down|restart|logs|ps|migrate]" >&2
    exit 2
    ;;
esac
