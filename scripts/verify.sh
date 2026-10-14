#!/usr/bin/env bash
# 本地校验脚本：编译 + 静态检查 + 测试。
# 用法：
#   scripts/verify.sh build   仅编译全工作区
#   scripts/verify.sh test    编译 + 测试全工作区
#   scripts/verify.sh test-system  编译 + 测试 devops-system
#   scripts/verify.sh test ./devops-system/internal/...  测试指定包
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO="${GO_BIN:-/d/GoToolchain/go/bin/go.exe}"
cd "$ROOT"

if [ ! -x "$GO" ]; then
  echo "找不到 Go 工具链: $GO（可用 GO_BIN 环境变量覆盖）" >&2
  exit 127
fi

TARGET="${2:-./...}"
MODULES="devops-common devops-id devops-infra devops-system devops-user"

build_all() {
  for m in $MODULES; do
    echo "  - $m"
    "$GO" build ./"$m"/... || return 1
  done
}

echo "==> go build（全部模块）"
build_all || exit 1

case "${1:-test}" in
  build)
    echo "==> 仅编译，通过"
    ;;
  vet)
    echo "==> go vet $TARGET"
    "$GO" vet "$TARGET" || exit 1
    echo "==> vet 通过"
    ;;
  test)
    echo "==> go test $TARGET"
    "$GO" test "$TARGET" || exit 1
    ;;
  *)
    echo "未知子命令: $1（支持 build|vet|test）" >&2
    exit 2
    ;;
esac
