// 临时探针:完整复现 root.go 的 loadNacosConfig 流程(远程优先+本地兜底)+ gRPC 全链路。
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	infracfg "devops-infra/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	grpcinfra "devops-infra/grpc"
	"devops-infra/nacos"

	"devops-id/pkg/idclient"
)

func main() {
	if err := infracfg.Init("config/bootstrap.yaml"); err != nil {
		panic(err)
	}
	fmt.Println("[0] 配置源:", infracfg.Files(), "APP_ENV =", infracfg.Env())

	var cfg struct {
		Nacos    infracfg.NacosConfig `mapstructure:"nacos"`
		IDClient struct {
			Endpoint    string        `mapstructure:"endpoint"`
			DialTimeout time.Duration `mapstructure:"dial_timeout"`
			CallTimeout time.Duration `mapstructure:"call_timeout"`
		} `mapstructure:"idclient"`
	}
	if err := infracfg.LoadInto(&cfg); err != nil {
		panic(err)
	}

	if err := nacos.Init(cfg.Nacos); err != nil {
		fmt.Println("[1] nacos.Init FAILED:", err)
		os.Exit(1)
	}
	defer nacos.Close()

	dataIDs := infracfg.RemoteDataIDs(cfg.Nacos.ConfigCenter.DataIDs)
	profileDataID := infracfg.ProfileDataID()
	fmt.Println("[2] 拉取 dataIDs:", dataIDs, "profile =", profileDataID)
	profileLoaded := false
	for _, dataID := range dataIDs {
		content, err := nacos.Fetch(dataID)
		if err != nil {
			fmt.Printf("    %-28s 远程未命中: %v\n", dataID, err)
			continue
		}
		if content == "" {
			fmt.Printf("    %-28s 远程内容为空\n", dataID)
			continue
		}
		if err := infracfg.MergeRemoteYAML(dataID, content); err != nil {
			fmt.Println("    MergeRemoteYAML FAILED:", err)
			os.Exit(1)
		}
		fmt.Printf("    %-28s 远程命中(合并)\n", dataID)
		if dataID == profileDataID {
			profileLoaded = true
		}
	}
	if !profileLoaded {
		if err := infracfg.MergeEnvConfig(); err != nil {
			fmt.Println("[3] 本地兜底 MergeEnvConfig FAILED:", err)
			os.Exit(1)
		}
		fmt.Println("[3] 远程未命中 profile → 本地兜底已合并:", infracfg.Files())
	} else {
		fmt.Println("[3] 远程 profile 命中,未合并本地文件")
	}

	if err := infracfg.LoadInto(&cfg); err != nil {
		panic(err)
	}
	fmt.Printf("[4] 最终 idclient = %+v\n", cfg.IDClient)
	if cfg.IDClient.Endpoint == "" {
		fmt.Println("[!] endpoint 为空")
		os.Exit(1)
	}

	conn, err := grpcinfra.Dial(context.Background(), cfg.IDClient.Endpoint, cfg.IDClient.DialTimeout)
	if err != nil {
		fmt.Println("[5] dial FAILED:", err)
		os.Exit(1)
	}
	fmt.Println("[5] dial OK, target =", conn.Target())
	defer conn.Close()

	// 先看 conn 状态
	time.Sleep(500 * time.Millisecond)
	fmt.Println("[5.5] conn state =", conn.GetState())

	// 直连测试(绕过 nacos resolver,排除 TCP 层问题)
	for _, addr := range []string{"127.0.0.1:9990", "172.16.60.150:9990"} {
		rawConn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil { fmt.Printf("    直连 %s NewClient err: %v\n", addr, err); continue }
		pctx, pcancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err = healthpb.NewHealthClient(rawConn).Check(pctx, &healthpb.HealthCheckRequest{})
		pcancel()
		if err != nil { fmt.Printf("    直连 %s Ping FAILED: %v\n", addr, err) } else { fmt.Printf("    直连 %s Ping OK\n", addr) }
		_ = rawConn.Close()
	}

	idc := idclient.New(conn, idclient.BufferConfig{Batch: 1, CallTimeout: cfg.IDClient.CallTimeout})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := idc.Ping(ctx); err != nil {
		fmt.Println("[6] Ping(经 nacos resolver)FAILED:", err)
		fmt.Println("[6.5] conn state =", conn.GetState())
		os.Exit(1)
	}
	fmt.Println("[6] Ping OK — readiness 应当通过")

	if id, err := idc.Next(context.Background(), "probe-tag"); err != nil {
		fmt.Println("[7] Next FAILED:", err)
		os.Exit(1)
	} else {
		fmt.Println("[7] Next OK, id =", id)
	}
	fmt.Println("=== 全链路通过 ===")
}