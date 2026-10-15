package http

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

	"github.com/stanley760/devops/devops-infra/config"
	commonResp "github.com/stanley760/devops/devops-common/response"
	"github.com/stanley760/devops/devops-infra/errs"
	"github.com/stanley760/devops/devops-infra/http/middleware"
	"github.com/stanley760/devops/devops-infra/http/response"
	"github.com/stanley760/devops/devops-infra/observability"
)

type Register func(r *gin.Engine)

type ReadinessCheck struct {
	Name  string
	Check func(context.Context) error
}

func Run(cfg config.ServerConfig, register Register, readiness ...ReadinessCheck) error {
	return RunWithOptions(cfg, register, nil, readiness...)
}

// GuardHooks 业务侧注入的全局中间件，在运维端点挂载之后、业务路由之前生效。
//
// 之所以做成扩展点而非把流量入口守卫写死在 infra：守卫（拒绝未经网关的直连请求）
// 是对外暴露 HTTP 的业务才需要的策略，且 Secret 由业务配置持有；infra 不应
// 感知具体业务的安全开关。
type GuardHooks struct {
	// Guards 在运维端点（/healthz /readyz /metrics）挂载后按序注册。
	// 这些端点不经网关，必须在守卫之前挂载，否则健康检查与 Prometheus 抓取会被拦。
	Guards []gin.HandlerFunc
}

// RunWithOptions 在现有 HTTP 装配上接入 OTel trace/RED 中间件与 /metrics。
// observability 运行时可为 nil，保持既有调用方零改动；
// guard 为 nil 时不注册任何业务守卫，同样保持既有调用方零改动。
func RunWithOptions(cfg config.ServerConfig, register Register, obs *observability.Runtime, guard *GuardHooks, readiness ...ReadinessCheck) error {
	gin.SetMode(ginMode())
	r, err := newRouter(cfg, register, readiness, obs, guard)
	if err != nil {
		return err
	}
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

func newRouter(cfg config.ServerConfig, register Register, readiness []ReadinessCheck, obs []*observability.Runtime, guard ...*GuardHooks) (*gin.Engine, error) {
	r := gin.New()
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, fmt.Errorf("配置可信代理失败: %w", err)
	}
	if len(obs) > 0 && obs[0] != nil {
		r.Use(obs[0].Middleware())
		r.Use(obs[0].MetricsMiddleware())
	}
	r.Use(middleware.Recovery())
	r.Use(middleware.AccessLog())
	r.HandleMethodNotAllowed = true
	r.NoRoute(func(c *gin.Context) { response.Fail(c, errs.CodeNotFound, "接口不存在") })
	r.NoMethod(func(c *gin.Context) { response.Fail(c, errs.CodeMethodNotAllowed, errs.ErrMethodNotAllowed.Msg) })
	r.GET("/healthz", func(c *gin.Context) { response.OK(c, gin.H{"status": "ok"}) })
	if len(obs) > 0 && obs[0] != nil && obs[0].MetricsHandler != nil {
		r.GET("/metrics", gin.WrapH(obs[0].MetricsHandler))
	}
	r.GET("/readyz", func(c *gin.Context) {
		var ready, unready []string
		for _, check := range readiness {
			if check.Check == nil {
				ready = append(ready, check.Name)
				continue
			}
			checkCtx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
			err := check.Check(checkCtx)
			cancel()
			if err != nil {
				unready = append(unready, check.Name)
				continue
			}
			ready = append(ready, check.Name)
		}
		if len(unready) == 0 {
			response.OK(c, gin.H{"status": "ready", "ready": ready})
			return
		}
		c.JSON(http.StatusServiceUnavailable, commonResp.BaseResponse[any]{
			Code: http.StatusServiceUnavailable,
			Msg:  "服务未就绪",
			Data: gin.H{"status": "unready", "ready": ready, "unready": unready},
		})
	})
	// 业务守卫必须在运维端点之后、业务路由之前：前者供直连探活，后者强制收口。
	if len(guard) > 0 && guard[0] != nil {
		for _, g := range guard[0].Guards {
			if g != nil {
				r.Use(g)
			}
		}
	}
	if register != nil {
		register(r)
	}
	return r, nil
}

func ginMode() string {
	switch config.Env() {
	case "prod", "PROD":
		return gin.ReleaseMode
	default:
		return gin.DebugMode
	}
}
