package svc

import (
	"syncer/internal/config"
	"syncer/internal/core"
)

type ServiceContext struct {
	Config config.Config
	Syncer *core.Syncer
}

func NewServiceContext(c config.Config, syncer *core.Syncer) *ServiceContext {
	return &ServiceContext{
		Config: c,
		Syncer: syncer,
	}
}
