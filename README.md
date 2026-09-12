# devOps — 用户权限系统（多租户）

![Go Version](https://img.shields.io/badge/go-1.26+-00ADD8?logo=go)
![Gin](https://img.shields.io/badge/Gin-v1.12-00ADD8?logo=gin)
![GORM](https://img.shields.io/badge/GORM-v1.31-00ADD8)
![Casbin](https://img.shields.io/badge/Casbin-v3-00ADD8)

Go 工作区（go.work）三模块：**devops-infra**（共享基础设施）+ **devops-system**（用户权限系统）+ **devops-id**（分布式 ID 发号服务，美团 Leaf）。

数据模型对齐 [RuoYi-vue-pro（yudao）](https://github.com/YunaiV/ruoyi-vue-pro) 精简版。

## 特性 / Features

- **JWT 双 token**：access（短效）+ refresh（Redis 会话，支持登出/踢下线/租户停用全员下线）
- **RBAC + 域模型**：Casbin `g, r, p` 域模型（`g` 域内用户-角色、`g2` 平台管理员），菜单权限按钮级控制
- **多租户双模式**：
  - 模式A `shared`（默认）：共享 Schema + `tenant_id` 判别列，GORM 插件自动注入过滤（fail-closed，无租户上下文直接报错防串数据）
  - 模式B `schema`：每租户独立库（`tenant_{code}`），Resolver 按 ctx 路由连接池 + 建库自动 provision + 菜单 fan-out
- **分布式 ID（美团 Leaf）**：
  - `segment` 号段模式（默认）：MySQL `t_leaf_alloc` + 服务端双 buffer + 动态 step（消耗 <15min 翻倍、>30min 减半，写回表跨实例生效）
  - `snowflake` 雪花模式：etcd 槽位抢占 workerID（lease 保活、宕机 TTL 释放）+ 时钟回拨保护（<5ms 自旋、≥5ms 拒发）
- **工程化**：统一响应信封、结构化异步日志（大小/时间双滚动）、CORS、优雅退出链、多环境配置（`APP_ENV` 覆盖）

## 目录 / TOC

- [快速开始](#快速开始--quick-start)
- [CLI](#cli)
- [配置](#配置--configuration)
- [API](#api)
- [架构要点](#架构要点)
- [测试](#测试--testing)
- [Roadmap](#roadmap)

## 快速开始 / Quick Start

### 前置依赖

| 组件 | 用途 | 必需 |
|---|---|---|
| Go 1.26+ | 编译 | 是 |
| MySQL 8 | 业务库（`t_tenant` + `t_system_*` + `casbin_rule` + `t_leaf_alloc`） | 是 |
| Redis | refresh 会话 | 是 |
| etcd | 仅 `idgen.mode=snowflake` | 否 |

建库（一次）：

```sql
CREATE DATABASE devops DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER 'devops'@'%' IDENTIFIED BY 'changeme';
GRANT ALL PRIVILEGES ON devops.* TO 'devops'@'%';
GRANT CREATE ON `tenant\_%`.* TO 'devops'@'%';  -- 仅模式B需要（建租户库）
```

### 启动

```bash
cd devops-id && go run . migrate   # 建号段表 + 注册 biz_tag（user/role/menu/dept/post/tenant）
cd devops-id && go run . server    # gRPC :9090（segment 模式默认）

cd devops-system && go run . migrate  # 建业务表 + 种子（租户 default、admin/Admin@123、菜单树、角色）
cd devops-system && go run . server   # HTTP :8080
```

> 同库部署时 system 的 `migrate` 也会注册号段行，`devops-id migrate` 可省略。

### 验证

```bash
# 登录（admin / Admin@123，默认租户 default）
curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"Admin@123"}'
# → {"code":200,"msg":"ok","data":{"access_token":"...","refresh_token":"..."}}

# 携带 token 查询用户列表
curl -s http://localhost:8080/api/v1/users?page_no=1&page_size=10 \
  -H 'Authorization: Bearer <access_token>'
```

## CLI

```text
devops-system            用户权限系统
  server                 启动 HTTP 服务（:8080）
  migrate                建表 + 复合唯一索引 + 号段注册 + 种子数据（幂等）

devops-id                分布式 ID 发号服务
  server                 启动 gRPC 发号服务（:9090，segment | snowflake）
  migrate                建号段表 t_leaf_alloc + 注册 biz_tag
    --tags log,order     额外注册的 biz_tag，逗号分隔
```

全局参数：`-f, --config-file` 指定配置路径（默认 `config/config.yaml`）；`--version` 查看版本。

## 配置 / Configuration

「基础 + 覆盖」模式：`config/config.yaml` 全量公共项，`config/{dev,test,prod}.yaml` 仅覆盖差异字段，`APP_ENV` 选择环境。

devops-system 关键段：

| 段 | 关键项 | 说明 |
|---|---|---|
| `server` | `port` / `trusted_proxies` / `cors.allow_origins` | HTTP 端口、可信代理、跨域白名单（空则不启用 CORS） |
| `mysql` | `host` / `database` … | 业务库连接 |
| `redis` | `host` / `db` | 会话存储 |
| `jwt` | `signing_key`（≥32 位）/ `access_expire` / `refresh_expire` | 双 token 参数 |
| `tenant` | `mode`（shared \| schema）/ `schema_template` / `default_code` | 多租户模式 |
| `idclient` | `endpoint` / `batch` / `low_water` | devops-id gRPC 接入（batch>1 开启客户端预取缓冲） |

devops-id 关键段：

| 段 | 关键项 | 说明 |
|---|---|---|
| `idgen` | `mode`（segment \| snowflake）/ `step` / `low_water` | 发号模式与号段参数 |
| `etcd` | `endpoints` / `lease_ttl` / `worker_cache` | 仅 snowflake：槽位分配 |

snowflake 模式切换示例：

```yaml
idgen:
  mode: snowflake
etcd:
  endpoints: ["192.168.215.3:2379"]
```

devops-id 已内置环境覆盖文件 `config/snowflake.yaml`（etcd 地址指向实际部署），直接启动：

```bash
cd devops-id && APP_ENV=snowflake go run . server

# 观察槽位持有与 lease TTL 释放
etcdctl --endpoints=192.168.215.3:2379 get --prefix idgen/snowflake/
```

已按上述 etcd 完成运行时验收：双实例同启槽位不冲突（0/1）、`kill -9` 宕机 10s 后槽位自动释放、优雅退出 revoke 立即释放、新实例依序抢占释放的槽位、双实例 400+ 次并发发号零重复。

## API

统一前缀 `/api/v1`，统一信封 `{"code", "msg", "data"}`（code 与 HTTP 状态一致）；`types.ID` 序列化为字符串（防 JS 精度丢失）。

| 分组 | 路由 | 鉴权 |
|---|---|---|
| 认证 | `POST /auth/login` `POST /auth/refresh` | 公开（login 可带 `tenant_code`，缺省取 `tenant.default_code`） |
| 认证 | `POST /auth/logout` `GET /auth/info` `GET /auth/routers` `PUT /auth/profile` `PUT /auth/profile/password` | JWT |
| 用户 | `GET/POST /users` `GET/PUT/DELETE /users/:id` `PUT /users/:id/password` | JWT + 租户 + `system:user:*` |
| 角色 | `GET/POST /roles` `GET/PUT/DELETE /roles/:id` `PUT /roles/:id/menus` | JWT + 租户 + `system:role:*` |
| 菜单 | `GET/POST /menus` `GET/PUT/DELETE /menus/:id` | JWT + 租户 + `system:menu:*` |
| 部门 | `GET/POST /depts` `GET/PUT/DELETE /depts/:id` | JWT + 租户 + `system:dept:*` |
| 岗位 | `GET/POST /posts` `GET/PUT/DELETE /posts/:id` | JWT + 租户 + `system:post:*` |
| 租户 | `GET/POST /tenants` `GET/PUT/DELETE /tenants/:id` `PUT /tenants/:id/status` | JWT + **平台**（`g2` 平台管理员 + `platform:tenant:*`） |

中间件链：`Recovery → AccessLog → CORS →（公开路由 | JWT → Tenant → Perm → 业务 | JWT → PlatformPerm → 租户管理）`。

## 架构要点

- **分层**：`cmd → router → api → service → dao → model`；基础设施（db/authz/store/logger）包级 `Init/Close` 单例，业务层构造注入
- **租户隔离 fail-closed**：纯模型查询由 GORM `TenantPlugin` 自动注入 `tenant_id`；原生 JOIN 由 dao 显式过滤（插件注入裸列会歧义）；模式B 主库不建租户表，误路由表现为「表不存在」
- **软删除改名**：删除用户/角色等唯一字段实体时改名（`admin` → `admin_del_1694...`），允许同名重建
- **ID 双模式**：segment 趋势递增、可读；snowflake 高吞吐不依赖 DB。DTO 层 ID 字符串化一次到位，切换无感
- **事务边界**：DB 事务只管业务表（user_role/role_menu），casbin 策略在事务外同步更新（gorm-adapter 自持事务）；失败补偿 + 重试由调用方语义保证
- **优雅退出**：system 先停 HTTP 排空在途请求，再关 resolver → authz → idclient → store → db；snowflake 退出 revoke lease 立即释放槽位

## 测试 / Testing

```bash
go work sync
go build ./... && go vet ./... && go test ./...
```

覆盖：infra（日志滚动、panic 恢复、CORS、响应信封）、id 集成测试（需本地 `:9090` 服务在跑，未启动自动 Skip）。

## Roadmap

- [ ] 操作/登录日志、账号配额、行级数据权限
- [ ] 租户套餐、域名 → 租户映射、A→B 迁移工具
- [ ] Prometheus 发号监控
- [ ] Docker 与生产部署
