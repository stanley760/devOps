#!/usr/bin/env bash
# 本地校验脚本：编译 + 静态检查 + 测试。
# 用法：
#   scripts/verify.sh build   仅编译全工作区
#   scripts/verify.sh test    编译 + 测试全工作区
#   scripts/verify.sh test-system  编译 + 测试 devops-system
#   scripts/verify.sh test ./devops-system/internal/...  测试指定包
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO="${GO_BIN:-/d/GoToolchain/go/bin/go.exe}"
cd "$ROOT"

if [ ! -x "$GO" ]; then
  echo "找不到 Go 工具链: $GO（可用 GO_BIN 环境变量覆盖）" >&2
  exit 127
fi

TARGET="${2:-}"
MODULES="devops-common devops-id devops-infra devops-system devops-user"

has_go_files() {
  find "$1" -type f -name '*.go' -print -quit | grep -q .
}

build_all() {
  for m in $MODULES; do
    if ! has_go_files "$m"; then
      echo "  - $m（无 Go 源文件，跳过）"
      continue
    fi
    echo "  - $m"
    "$GO" build ./"$m"/...
  done
}

test_all() {
  for m in $MODULES; do
    if ! has_go_files "$m"; then
      echo "  - $m（无 Go 源文件，跳过）"
      continue
    fi
    echo "  - $m"
    "$GO" test ./"$m"/...
  done
}

vet_all() {
  for m in $MODULES; do
    if ! has_go_files "$m"; then
      echo "  - $m（无 Go 源文件，跳过）"
      continue
    fi
    echo "  - $m"
    "$GO" vet ./"$m"/...
  done
}

echo "==> go build（全部模块）"
build_all

case "${1:-test}" in
  build)
    echo "==> 仅编译，通过"
    ;;
  vet)
    if [ -n "$TARGET" ]; then
      echo "==> go vet $TARGET"
      "$GO" vet "$TARGET"
    else
      echo "==> go vet（全部模块）"
      vet_all
    fi
    echo "==> vet 通过"
    ;;
  test)
    if [ -n "$TARGET" ]; then
      echo "==> go test $TARGET"
      "$GO" test "$TARGET"
    else
      echo "==> go test（全部模块）"
      test_all
    fi
    ;;
  test-system)
    echo "==> go test ./devops-system/..."
    "$GO" test ./devops-system/...
    ;;
  *)
    echo "未知子命令: $1（支持 build|vet|test|test-system）" >&2
    exit 2
    ;;
esac
