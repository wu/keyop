package heartbeat

import (
	"context"
	"github.com/wu/keyop/core"
)

func init() {
	core.RegisterService("heartbeat", func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return NewService(deps, cfg, ctx)
	})
}
