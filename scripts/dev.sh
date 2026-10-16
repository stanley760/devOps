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
  echo "首次运行或 schema 变更后，请先执行建表与种子初始化：sh deploy/dev.sh migrate"
  echo "首次使用前需复制 .env.example 为 .env 并填入局域网 IP（NACOS_REGISTER_IP）。"
}

# —— watch：监测本地业务服务（存活+就绪）。存活失败自动重启（退避+熔断），就绪失败仅告警 ——
# 端点为 dev 配置硬编码：devops-id 存活探针走 metrics :9991（若关闭 metrics 会被误判死亡，
# 由熔断兜底）；devops-system /readyz 内含 mysql/redis/devops-id 三项依赖检查。

WATCH_INTERVAL=10  # 轮询间隔（秒）
WATCH_BACKOFF=60   # 单服务重启退避（秒）
WATCH_BREAKER=5    # 连续重启熔断阈值（次）
ID_ALIVE_URL="http://127.0.0.1:9991/metrics"
SYS_ALIVE_URL="http://127.0.0.1:9999/healthz"
SYS_READY_URL="http://127.0.0.1:9999/readyz"

# HTTP 探针：输出状态码（连接失败为 000 或空）；$2 提供时把响应体写入该文件。
# curl 失败经 || true 吞掉退出码，避免 set -e 杀掉监测循环。
probe_code() {
  if [ "$#" -ge 2 ]; then
    curl -s -m 3 -o "$2" -w '%{http_code}' "$1" 2>/dev/null || true
  else
    curl -s -m 3 -o /dev/null -w '%{http_code}' "$1" 2>/dev/null || true
  fi
}

# 读单服务上次重启时间戳（持久化在 .dev-logs，watch 重启后退避仍生效）；非法内容按 0
read_last_restart() {
  value=0
  if [ -f "$LOG_DIR/$1.last-restart" ]; then
    value=$(cat "$LOG_DIR/$1.last-restart" 2>/dev/null || echo 0)
  fi
  case "$value" in
    ''|*[!0-9]*) value=0 ;;
  esac
  echo "$value"
}

# 发起重启（调用方已过退避检查）：更新时间戳并复用 start_service
watch_restart() {
  echo "[$(date '+%H:%M:%S')] 自动重启 $1 ..."
  date +%s >"$LOG_DIR/$1.last-restart"
  start_service "$1" "$2"
}

print_state() {
  # $1=服务名 $2=状态 $3=就绪明细
  case "$2" in
    UP)      echo "[$(date '+%H:%M:%S')] $1 UP" ;;
    DOWN)    echo "[$(date '+%H:%M:%S')] $1 DOWN" ;;
    UNREADY) echo "[$(date '+%H:%M:%S')] $1 UNREADY（依赖异常，仅告警不重启）: $3" ;;
  esac
}

watch_services() {
  mkdir -p "$LOG_DIR"
  if ! command -v go >/dev/null 2>&1; then
    echo "警告: 当前 shell 找不到 go，监测到故障将无法自动重启（Git Bash 下需先 export PATH 再运行）"
  fi
  echo "监测 devops-id（$ID_ALIVE_URL）与 devops-system（$SYS_ALIVE_URL + $SYS_READY_URL）"
  echo "每 ${WATCH_INTERVAL}s 轮询；存活失败自动重启（退避 ${WATCH_BACKOFF}s，连续 ${WATCH_BREAKER} 次熔断）；就绪失败仅告警。Ctrl-C 退出。"
  echo ""
  trap 'echo ""; echo "监测已退出（服务不受影响）"; exit 0' INT TERM

  id_state="INIT"; id_breaker=0; id_tripped=0; id_last=$(read_last_restart devops-id)
  sys_state="INIT"; sys_breaker=0; sys_tripped=0; sys_last=$(read_last_restart devops-system)
  cycle=0

  while :; do
    cycle=$((cycle + 1))

    # 探测：devops-id 仅存活；devops-system 存活 + 就绪
    if [ "$(probe_code "$ID_ALIVE_URL")" = "200" ]; then id_new="UP"; else id_new="DOWN"; fi
    sys_new="DOWN"; sys_detail=""
    if [ "$(probe_code "$SYS_ALIVE_URL")" = "200" ]; then
      sys_new="UP"
      if [ "$(probe_code "$SYS_READY_URL" "$LOG_DIR/watch-readyz.body")" != "200" ]; then
        sys_new="UNREADY"
        sys_detail=$(grep -o '"unready":\[[^]]*\]' "$LOG_DIR/watch-readyz.body" 2>/dev/null || true)
        if [ -z "$sys_detail" ]; then sys_detail='"unready":[?]'; fi
      fi
    fi

    # 状态变化即时打印；恢复 UP 复位熔断计数并解除熔断
    if [ "$id_new" != "$id_state" ]; then
      print_state devops-id "$id_new" ""
      id_state="$id_new"
    fi
    if [ "$id_new" = "UP" ]; then id_breaker=0; id_tripped=0; fi
    if [ "$sys_new" != "$sys_state" ]; then
      print_state devops-system "$sys_new" "$sys_detail"
      sys_state="$sys_new"
    fi
    if [ "$sys_new" = "UP" ]; then sys_breaker=0; sys_tripped=0; fi

    # 心跳：每 6 轮（约 60s）一行
    if [ $((cycle % 6)) -eq 0 ]; then
      echo "[$(date '+%H:%M:%S')] 心跳: devops-id=$id_state devops-system=$sys_state"
    fi

    # 自动重启（仅存活失败触发；就绪失败是容器/依赖问题，重启 Go 服务无意义）
    if [ "$id_new" = "DOWN" ] && [ "$id_tripped" -ne 1 ]; then
      now=$(date +%s)
      if [ $((now - id_last)) -ge "$WATCH_BACKOFF" ]; then
        watch_restart devops-id devops-id
        id_last=$now
        id_breaker=$((id_breaker + 1))
        if [ "$id_breaker" -ge "$WATCH_BREAKER" ]; then
          id_tripped=1
          echo "[$(date '+%H:%M:%S')] devops-id 连续 ${WATCH_BREAKER} 次重启仍不健康，停止自动重启，请人工检查"
        fi
      fi
    fi
    if [ "$sys_new" = "DOWN" ] && [ "$sys_tripped" -ne 1 ]; then
      now=$(date +%s)
      if [ $((now - sys_last)) -ge "$WATCH_BACKOFF" ]; then
        # 两服务同时死亡：先等 devops-id 恢复（最长 30s）再重启 devops-system
        if [ "$id_new" = "DOWN" ]; then
          echo "  等待 devops-id 恢复后再重启 devops-system（最长 30s）..."
          waited=0
          while [ "$waited" -lt 30 ]; do
            sleep 3
            waited=$((waited + 3))
            if [ "$(probe_code "$ID_ALIVE_URL")" = "200" ]; then break; fi
          done
        fi
        watch_restart devops-system devops-system
        sys_last=$(date +%s)
        sys_breaker=$((sys_breaker + 1))
        if [ "$sys_breaker" -ge "$WATCH_BREAKER" ]; then
          sys_tripped=1
          echo "[$(date '+%H:%M:%S')] devops-system 连续 ${WATCH_BREAKER} 次重启仍不健康，停止自动重启，请人工检查"
        fi
      fi
    fi

    sleep "$WATCH_INTERVAL"
  done
}

case "$COMMAND" in
  up)
    # 启动中间件 + 网关 + 可观测栈，随后自动拉起本地业务服务
    # higress-init 会在 higress 前自动灌入 deploy/higress 的配置（首次/down 后）
    compose up -d mysql redis nacos etcd higress
    compose up -d jaeger prometheus alertmanager grafana
    echo ""
    echo "中间件与可观测栈已启动。"
    start_services
    ;;
  app)
    # 仅启动/补起本地业务服务，不动容器
    start_services
    ;;
  watch)
    # 监测本地业务服务：存活失败自动重启，就绪失败仅告警（容器与网关不在监测范围）
    watch_services
    ;;
  stop)
    # 停止本地业务服务（容器不受影响）
    stop_service devops-system
    stop_service devops-id
    ;;
  migrate)
    # dev compose 只运行基础设施；业务服务在本地运行，因此迁移也用本地配置执行。
    (cd "$ROOT/devops-system" && go run . migrate)
    ;;
  down) compose down ;;
  restart) compose restart mysql redis nacos etcd higress ;;
  sync)
    # 修改 deploy/higress 下 yaml 后的显式同步，兼治 envoy OOM 毒配置：
    #   1. 停网关（apiserver 停机，改配置文件不会被内存副本覆盖）
    #   2. 重跑 higress-init（--force-recreate 才会重跑已完成的一次性容器）灌 git 配置
    #   3. 删除 higress-config 里 all-in-one 自动生成的 inclusionRegexps: [.*]
    #      （全量指标 → envoy 统计表爆内存被 cgroup OOM 杀，supervisord 视 exit 0 为
    #      expected 不自动拉起；文件为 CRLF 行尾，sed 前必须 tr 规范化）
    #   4. 起网关。新卷首次 up 后建议跑一次本命令
    compose stop higress
    compose up -d --force-recreate higress-init
    compose run --rm --entrypoint sh higress-init -c 'if [ -f /data/configmaps/higress-config.yaml ]; then tr -d "\r" < /data/configmaps/higress-config.yaml > /tmp/hc && mv /tmp/hc /data/configmaps/higress-config.yaml && sed -i "/^      proxyStatsMatcher:$/,/^        - \.\*$/d" /data/configmaps/higress-config.yaml; fi'
    compose start higress
    ;;
  logs) compose logs -f ;;
  ps) compose ps ;;
  *)
    echo "用法: $0 [up|app|stop|down|restart|sync|logs|ps|migrate|watch]" >&2
    exit 2
    ;;
esac
