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

# 一次性建业务表与种子数据（幂等，可重复执行）
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

dev-up:
	$(DEV_COMPOSE) build
	$(DEV_COMPOSE) up -d mysql redis nacos etcd
	$(DEV_COMPOSE) run --rm devops-system migrate
	$(DEV_COMPOSE) up -d devops-id devops-system

dev-down:
	$(DEV_COMPOSE) down

dev-logs:
	$(DEV_COMPOSE) logs -f

dev-ps:
	$(DEV_COMPOSE) ps

dev-migrate:
	$(DEV_COMPOSE) run --rm devops-system migrate
