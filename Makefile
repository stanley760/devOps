# 容器构建与本地编排快捷命令（详细说明见 README「部署」与 deploy/docker-compose.yml）
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMPOSE := docker compose -f deploy/docker-compose.yml
DEV_COMPOSE := docker compose -f deploy/docker-compose-dev.yml

.PHONY: build-id build-system build migrate up down logs ps dev-up dev-down dev-logs dev-ps dev-migrate

build-id:
	docker build -f devops-id/Dockerfile --build-arg VERSION=$(VERSION) -t devops-id:$(VERSION) .

build-system:
	docker build -f devops-system/Dockerfile --build-arg VERSION=$(VERSION) -t devops-system:$(VERSION) .

build: build-id build-system

# 一次性建业务表与种子数据（表存在即跳过，幂等，可重复执行）
migrate:
	$(COMPOSE) run --rm devops-system migrate

up:
	$(COMPOSE) up -d

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f

ps:
	$(COMPOSE) ps

# dev-up 只启动中间件 + 网关 + 可观测栈；业务服务由本地 go run 启动。
# higress-init 一次性服务会自动把 deploy/higress 配置灌入命名卷（首次/down 后）。
dev-up:
	$(DEV_COMPOSE) up -d mysql redis nacos etcd higress
	$(DEV_COMPOSE) up -d jaeger prometheus alertmanager grafana

dev-down:
	$(DEV_COMPOSE) down

dev-logs:
	$(DEV_COMPOSE) logs -f

dev-ps:
	$(DEV_COMPOSE) ps

# dev-migrate 用一次性容器执行建表与种子初始化（devops-system 服务在 app profile 下）
dev-migrate:
	$(DEV_COMPOSE) build devops-system
	$(DEV_COMPOSE) --profile app run --rm devops-system migrate
