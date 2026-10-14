# devOps 任务运行器（替代 Makefile）
#
# 语法要点（just 与 make 的关键差异）：
#   - 缩进用【空格】，命令之间不用制表符
#   - 变量插值用 {{var}}，shell 命令用反引号
#   - 每个 recipe 默认独立 shell，跨行状态需显式声明（见下方 set shell）
#   - 用 `just --list` 查看全部命令

# 版本号：从 git tag 推导，无 tag 时回退 dev。
#
# 注意：git describe 在多 tag 仓库下会返回 "devops-common/v0.1.1-dirty" 这类
# 含斜杠的值，而 docker tag 不允许斜杠（会报 "invalid reference format"）。
# 故把斜杠与非法字符统一替换为短横线。
version := `git describe --tags --always --dirty 2>/dev/null | tr '/' '-' | tr -cd '[:alnum:]._-' || echo dev`

# 本地 Go 工具链（模块缓存在 /d/Go/pkg）；可用 `just GO=/path/to/go test` 覆盖
go := env_var_or_default("GO", "/d/GoToolchain/go/bin/go.exe")

compose := "docker compose -f deploy/docker-compose.yml"
dev_compose := "docker compose -f deploy/docker-compose-dev.yml"

# go.work 是多模块工作区，`go build ./...` 在根目录会报
# "directory prefix . does not contain modules listed in go.work"，
# 必须逐个模块指定。
modules := "devops-common devops-id devops-infra devops-system"

# 列出全部可用命令
default:
    @just --list --unsorted

# ---------- 容器构建 ----------

# 构建 devops-id 镜像
build-id:
    docker build -f devops-id/Dockerfile --build-arg VERSION={{version}} -t devops-id:{{version}} .

# 构建 devops-system 镜像
build-system:
    docker build -f devops-system/Dockerfile --build-arg VERSION={{version}} -t devops-system:{{version}} .

# 构建全部镜像
build: build-id build-system

# ---------- 生产编排（deploy/docker-compose.yml）----------

# 建业务表与种子数据（幂等，表存在即跳过）
migrate:
    {{compose}} run --rm devops-system migrate

# 启动生产编排
up:
    {{compose}} up -d

# 停止生产编排
down:
    {{compose}} down

# 跟踪生产编排日志
logs:
    {{compose}} logs -f

# 查看生产编排状态
ps:
    {{compose}} ps

# ---------- 开发编排（deploy/docker-compose-dev.yml）----------
# 只启中间件 + 网关 + 可观测栈；业务服务由本地 go run 启动。

# 启动开发环境依赖
dev-up:
    {{dev_compose}} up -d mysql redis nacos etcd higress
    {{dev_compose}} up -d jaeger prometheus alertmanager grafana

# 停止开发环境依赖
dev-down:
    {{dev_compose}} down

# 跟踪开发环境日志
dev-logs:
    {{dev_compose}} logs -f

# 查看开发环境状态
dev-ps:
    {{dev_compose}} ps

# 用一次性容器执行建表与种子初始化
dev-migrate:
    {{dev_compose}} build devops-system
    {{dev_compose}} --profile app run --rm devops-system migrate

# ---------- 本地校验（不走容器）----------

# 编译全工作区（逐模块）
build-local:
    #!/usr/bin/env bash
    set -euo pipefail
    for m in {{modules}}; do
        echo "==> 编译 $m"
        {{go}} build "./$m/..."
    done

# 静态检查全工作区（逐模块）
vet:
    #!/usr/bin/env bash
    set -euo pipefail
    for m in {{modules}}; do
        echo "==> vet $m"
        {{go}} vet "./$m/..."
    done

# 跑全工作区测试（逐模块）
test:
    #!/usr/bin/env bash
    set -euo pipefail
    for m in {{modules}}; do
        echo "==> 测试 $m"
        {{go}} test "./$m/..."
    done

# 只测 devops-system
test-system:
    {{go}} test ./devops-system/...

# 只测新增的领域层与应用层
test-domain:
    {{go}} test ./devops-system/internal/domain/... ./devops-system/internal/application/... ./devops-system/internal/arch/...

# 格式化（gofmt）
fmt:
    {{go}} fmt ./devops-system/... ./devops-common/... ./devops-infra/... ./devops-id/...

# 编译 + vet + 测试：提交前跑这个
check: build-local vet test

# 清空 Go 构建缓存
clean:
    {{go}} clean -cache -testcache
