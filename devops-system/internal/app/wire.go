//go:build wireinject

package app

import (
	httpinfra "github.com/stanley760/devOps/devops-infra/http"

	"github.com/google/wire"
	"github.com/stanley760/devOps/devops-system/internal/auth"
	"github.com/stanley760/devOps/devops-system/internal/config"
	"github.com/stanley760/devOps/devops-system/internal/middleware"
)

// InitializeHTTP assembles services, handlers, and the business route callback.
// Regenerate the injector with: go generate -tags=wireinject ./internal/app
//
//go:generate go run github.com/google/wire/cmd/wire
func InitializeHTTP(cfg config.Config, kit *middleware.Kit, authM *auth.Manager) httpinfra.Register {
	wire.Build(serviceSet, routeSet)
	return nil
}
