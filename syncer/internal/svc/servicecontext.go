package svc

import (
	"syncer/internal/config"
	"syncer/internal/core"
)

type ServiceContext struct {
	Config config.Config
	Syncer *core.Syncer
	Store  core.Store
}

func NewServiceContext(c config.Config, syncer *core.Syncer, store core.Store) *ServiceContext {
	return &ServiceContext{
		Config: c,
		Syncer: syncer,
		Store:  store,
	}
}
