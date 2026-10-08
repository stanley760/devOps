//go:build wireinject

package app

import (
	"github.com/google/wire"

	httpinfra "devops-infra/http"

	"devops-system/internal/auth"
	"devops-system/internal/config"
	"devops-system/internal/middleware"
)

// InitializeHTTP assembles services, handlers, and the business route callback.
// Regenerate the injector with: go generate -tags=wireinject ./internal/app
//
//go:generate go run github.com/google/wire/cmd/wire
func InitializeHTTP(cfg config.Config, kit *middleware.Kit, authM *auth.Manager) httpinfra.Register {
	wire.Build(serviceSet, routeSet)
	return nil
}
