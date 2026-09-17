# 容器构建与本地编排快捷命令（详细说明见 README「部署」与 deploy/docker-compose.yml）
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMPOSE := docker compose -f deploy/docker-compose.yml

.PHONY: build-id build-system build migrate up down logs ps

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
