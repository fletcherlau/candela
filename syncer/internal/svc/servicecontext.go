package svc

import (
	"syncer/internal/config"
	"syncer/internal/core"
	"syncer/internal/middleware"

	"github.com/zeromicro/go-zero/rest"
)

type ServiceContext struct {
	Config         config.Config
	Syncer         core.SyncRunner
	SWSyncer       *core.SWSyncer
	SignalComputer *core.SignalComputer
	Store          core.Store
	SWStore        core.SWStore
	ApiKeyAuth     rest.Middleware
}

func NewServiceContext(c config.Config, syncer core.SyncRunner, swSyncer *core.SWSyncer, signalComputer *core.SignalComputer, store core.Store, swStore core.SWStore) *ServiceContext {
	return &ServiceContext{
		Config:         c,
		Syncer:         syncer,
		SWSyncer:       swSyncer,
		SignalComputer: signalComputer,
		Store:          store,
		SWStore:        swStore,
		ApiKeyAuth:     middleware.NewApiKeyAuthMiddleware(c.ApiKey).Handle,
	}
}
