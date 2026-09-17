# devops-id 发号服务接入指南

`devops-id` 是基于 Snowflake 算法的 gRPC 分布式 ID 服务。服务按 `leaf_name + biz_tag` 在 etcd 中隔离 worker-ID 分配空间；首次请求某个 `biz_tag` 时才为该业务标识分配 worker。

## 接入概览

| 项目 | 说明 |
| --- | --- |
| 协议 | gRPC / protobuf |
| 服务名 | `idgen.IdGen` |
| 默认监听 | `:9090` |
| 方法 | `Next`、`BatchNext` |
| ID 类型 | `int64` |
| 健康检查 | `grpc.health.v1.Health/Check` |
| 传输安全 | 当前使用明文 gRPC，应部署在可信内网 |

## ID 语义

- 同一 `biz_tag` 内 ID 唯一、趋势递增，但不保证连续。
- 不同 `biz_tag` 的 ID 空间完全独立，允许出现相同 ID。
- `biz_tag` 不参与 Snowflake 位运算；它与 `leaf_name` 一起决定 etcd 槽位和本地 worker 缓存的命名空间。
- 多客户端并发取号时，不同客户端获得的 ID 可能交错。
- 服务重启或客户端预取缓冲未消费完会产生空洞；不要把 ID 当作无空洞序列使用。
- 只把 ID 作为业务标识使用，不要从 ID 解析业务含义。

## Worker 分配

Snowflake ID 结构为：

```text
| 1 bit 符号位 | 41 bit 毫秒时间戳 | 10 bit worker ID | 12 bit 序列号 |
```

worker ID 的分配按以下维度隔离：

```text
idgen/snowflake/{leaf_name}/{biz_tag}/
```

例如 `leaf_name=devops-id`、`biz_tag=user` 时，服务实例会在 `idgen/snowflake/devops-id/user/` 下抢占槽位。`role` 等其它 `biz_tag` 会使用独立的槽位前缀，因此拥有独立 worker-ID 空间。

本地 worker 缓存路径为：

```text
{worker_cache}/{leaf_name}/{biz_tag}/worker_id
```

服务启动时不分配 worker。某个 `biz_tag` 首次收到取号请求时，`devops-id` 才会创建该 tag 的生成器并通过 etcd 分配 worker ID。后续相同 `biz_tag` 的请求会复用该生成器。

`leaf_name` 与 `biz_tag` 必须是安全的路径组件：不能为空、包含前后空白、`.`、`..`、`/` 或 `\`。

## gRPC 契约

Proto 源文件：[`proto/idgen.proto`](./proto/idgen.proto)。

```protobuf
service IdGen {
  rpc Next(NextRequest) returns (NextReply);
  rpc BatchNext(BatchNextRequest) returns (BatchNextReply);
}

message NextRequest {
  string biz_tag = 1;
}

message NextReply {
  int64 id = 1;
}

message BatchNextRequest {
  string biz_tag = 1;
  int32 count = 2;
}

message BatchNextReply {
  repeated int64 ids = 1;
}
```

### 方法说明

| 方法 | Full method | 请求 | 响应 | 用途 |
| --- | --- | --- | --- | --- |
| `Next` | `/idgen.IdGen/Next` | `biz_tag` | `id` | 取一个 ID |
| `BatchNext` | `/idgen.IdGen/BatchNext` | `biz_tag`、`count` | `ids` | 原子取一批 ID，供客户端预取缓冲使用 |

### 参数约束

- `biz_tag` 必须是非空且可作为路径组件的业务标识，建议使用稳定单数标识，如 `user`、`order`。
- `BatchNext.count` 范围为 `1..100000`。
- `BatchNext` 返回列表在单次响应内单调递增。
- 业务接入建议使用较小批量（例如 100），不要把上限当成常规配置。

## 错误与重试

| gRPC 状态 | 含义 | 建议 |
| --- | --- | --- |
| `InvalidArgument` | `biz_tag` 非法或 `count` 越界 | 修复请求参数，不要原样重试 |
| `Unavailable` | etcd 不可用、时钟回拨超阈值、worker lease 失效、服务不可达 | 使用指数退避重试 |
| `Internal` | 服务端内部错误 | 记录错误并检查服务端日志，不要盲目重试 |

重试语义是“最多消耗额外 ID，不会在同一个 `biz_tag` 内产生重复 ID”。如果请求已到达服务端但响应丢失，这批 ID 可能已分配但调用方未收到；再次重试会取得新 ID。不要在业务层缓存失败请求的 ID 并复用。

## Go 接入

### 推荐：使用 `idclient` 预取缓冲

`pkg/idclient` 封装了 `BatchNext` 预取、按 `biz_tag` 隔离的本地缓冲、低水位异步补充和健康检查。对同一个 `biz_tag`，本地发号顺序递增。

```go
package main

import (
	"context"
	"fmt"
	"time"

	grpcinfra "devops-infra/grpc"
	"devops-id/pkg/idclient"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := grpcinfra.Dial(ctx, "127.0.0.1:9090", 2*time.Second)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	client := idclient.New(conn, idclient.BufferConfig{
		Batch:       100,
		LowWater:    0.2,
		CallTimeout: 3 * time.Second,
	})
	if err := client.Ping(ctx); err != nil {
		panic(err)
	}

	id, err := client.Next(context.Background(), "user")
	if err != nil {
		panic(err)
	}
	fmt.Println(id)
}
```

`idclient.Client` 可被多个 goroutine 共用，内部按 `biz_tag` 维护独立缓冲。

### 配置建议

| 配置 | 建议值 | 说明 |
| --- | --- | --- |
| `batch` | `1..100` | 预取数量；批量越大 RPC 越少，但进程退出时可能浪费更多 ID |
| `low_water` | `0.1..0.3` | 剩余比例低于该值时异步补充 |
| `dial_timeout` | `1s..5s` | 建立客户端连接的超时 |
| `call_timeout` | `1s..5s` | 单次 `BatchNext` 调用超时 |

### 直连 gRPC stub

如果不使用预取缓冲，可以直接使用生成的 `idgenpb` 客户端：

```go
package main

import (
	"context"
	"fmt"
	"time"

	grpcinfra "devops-infra/grpc"
	"devops-id/pkg/idgenpb"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := grpcinfra.Dial(ctx, "127.0.0.1:9090", 2*time.Second)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	client := idgenpb.NewIdGenClient(conn)
	next, err := client.Next(context.Background(), &idgenpb.NextRequest{BizTag: "user"})
	if err != nil {
		panic(err)
	}
	fmt.Println(next.GetId())
}
```

## GORM 模型接入

`pkg/idgorm` 提供 GORM `BeforeCreate` 场景下的公共发号接缝。它只依赖最小生成器接口：

```go
type Generator interface {
	Next(ctx context.Context, bizTag string) (int64, error)
}
```

`idclient.Client` 已满足该接口。

```go
package model

import (
	"gorm.io/gorm"

	"devops-id/pkg/idgorm"
)

var tableBizTag = map[string]string{
	"t_order": "order",
}

var idHook = idgorm.New(nil, tableBizTag)

func SetIDGenerator(generator idgorm.Generator) {
	idHook.SetGenerator(generator)
}

type Order struct {
	ID int64 `gorm:"primaryKey;autoIncrement:false"`
}

func (Order) TableName() string { return "t_order" }

func (m *Order) BeforeCreate(tx *gorm.DB) error {
	return idHook.Assign(tx, &m.ID)
}
```

`idHook.Assign` 会保留非零显式 ID，只对零值主键发号。新业务接入时只需要维护表名到 `biz_tag` 的映射；Snowflake worker 会在首次发号时按 `leaf_name + biz_tag` 自动分配，无需数据库预注册。

## grpcurl 快速验证

开发环境默认开启 gRPC reflection：

```bash
# 查看服务
grpcurl -plaintext 127.0.0.1:9090 list

# 健康检查
grpcurl -plaintext -d '{}' 127.0.0.1:9090 grpc.health.v1.Health/Check

# 取单个 ID
grpcurl -plaintext -d '{"biz_tag":"user"}' 127.0.0.1:9090 idgen.IdGen/Next

# 取一批 ID
grpcurl -plaintext -d '{"biz_tag":"user","count":10}' 127.0.0.1:9090 idgen.IdGen/BatchNext
```

Protobuf JSON 会把 `int64` 输出为字符串：

```json
{
  "id": "7306137248161302528"
}
```

## 非 Go 语言接入

1. 复制或引用 [`proto/idgen.proto`](./proto/idgen.proto)。
2. 使用对应语言的 protoc 插件生成 `idgen.IdGen` 客户端。
3. 调用 `/idgen.IdGen/Next` 或 `/idgen.IdGen/BatchNext`。
4. 使用 gRPC 标准健康检查 `/grpc.health.v1.Health/Check` 判断服务是否 `SERVING`。
5. 对外 API 或 JSON 数据结构将 `int64` ID 按字符串暴露。

## 接入检查清单

- [ ] 已确认服务地址和端口，且网络可达。
- [ ] 已为每类业务对象确定稳定的 `biz_tag`。
- [ ] 理解唯一性按 `biz_tag` 划分，不同 `biz_tag` 之间允许重号。
- [ ] 只把 ID 当作唯一标识，不依赖连续性。
- [ ] `BatchNext.count` 使用业务需要的最小合理值。
- [ ] 对 `Unavailable` 使用指数退避重试，对参数错误先修正请求。
- [ ] 对外 API 将 `int64` ID 序列化为字符串。
- [ ] 明文 gRPC 只部署在可信内网，或后续按需补充 TLS/mTLS。
