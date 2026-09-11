// Package router 组装 HTTP 路由并启动服务。
//
// 当前为框架骨架：统一中间件（Recovery）、统一 404/405 响应与健康检查；
// 业务路由（/api/v1）随权限系统各模块逐步挂载。
package router

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"devOps/internal/config"
	"devOps/internal/errs"
	"devOps/internal/middleware"
	"devOps/internal/response"
)

// Run 启动 HTTP 服务，收到 SIGINT/SIGTERM 后优雅退出。
func Run(cfg config.ServerConfig) error {
	gin.SetMode(ginMode())

	r := gin.New()
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return fmt.Errorf("配置可信代理失败: %w", err)
	}
	// Gin 请求日志输出到控制台，终端环境下自动显示颜色。
	r.Use(gin.Logger())
	r.Use(middleware.Recovery())
	r.Use(middleware.AccessLog())

	// 未匹配路由 / 未允许方法同样走统一信封
	r.HandleMethodNotAllowed = true
	r.NoRoute(func(c *gin.Context) {
		response.Fail(c, errs.CodeNotFound, "接口不存在")
	})
	r.NoMethod(func(c *gin.Context) {
		response.Fail(c, errs.CodeMethodNotAllowed, errs.ErrMethodNotAllowed.Msg)
	})

	// 健康检查：探活接口，返回统一 JSON 信封
	r.GET("/healthz", func(c *gin.Context) {
		response.OK(c, gin.H{"status": "ok"})
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      r,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("HTTP 服务启动", "addr", srv.Addr, "mode", gin.Mode())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("HTTP 服务异常: %w", err)
	case <-ctx.Done():
	}

	slog.Info("收到退出信号，开始优雅关闭")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("优雅关闭失败: %w", err)
	}
	slog.Info("HTTP 服务已停止")
	return nil
}

// ginMode 按环境决定 gin 运行模式：
// dev 或未设置时用 debug（启动时打印路由表，便于开发），其余用 release。
func ginMode() string {
	switch config.Env() {
	case "", "dev":
		return gin.DebugMode
	default:
		return gin.ReleaseMode
	}
}
