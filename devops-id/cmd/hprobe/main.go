package main

import (
	"context"
	"fmt"
	"time"

	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func try(addr string) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil { fmt.Println(addr, "NewClient err:", err); return }
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		fmt.Printf("%-25s FAILED: %v\n", addr, err)
	} else {
		fmt.Printf("%-25s OK\n", addr)
	}
}

func main() {
	try("127.0.0.1:19991")  // 我起的最新代码测试实例
	try("127.0.0.1:9991")    // 用户起的进程(25388)
}