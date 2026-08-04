package svc

import (
	"syncer/internal/config"
	"syncer/internal/core"
	"syncer/internal/middleware"

	"github.com/zeromicro/go-zero/rest"
)

type ServiceContext struct {
	Config     config.Config
	Syncer     *core.Syncer
	Store      core.Store
	ApiKeyAuth rest.Middleware
}

func NewServiceContext(c config.Config, syncer *core.Syncer, store core.Store) *ServiceContext {
	return &ServiceContext{
		Config:     c,
		Syncer:     syncer,
		Store:      store,
		ApiKeyAuth: middleware.NewApiKeyAuthMiddleware(c.ApiKey).Handle,
	}
}
