package handlers

import (
	"time"

	"github.com/baking-bad/bcdhub/cmd/api/oauth"
	"github.com/baking-bad/bcdhub/internal/config"
	"github.com/baking-bad/bcdhub/internal/noderpc"
	"github.com/baking-bad/bcdhub/internal/tzkt"
)

// Context -
type Context struct {
	*config.Context
	OAUTH oauth.Config
}

// NewContext -
func NewContext(cfg config.Config) (*Context, error) {
	var oauthCfg oauth.Config
	if cfg.API.OAuth.Enabled {
		var err error
		oauthCfg, err = oauth.New(cfg)
		if err != nil {
			return nil, err
		}
	}

	ctx := config.NewContext(
		config.WithElasticSearch(cfg.Elastic),
		config.WithRPC(cfg.RPC),
		config.WithDatabase(cfg.DB),
		config.WithShare(cfg.Share.Path),
		config.WithTzKTServices(cfg.TzKT),
		config.WithLoadErrorDescriptions("data/errors.json"),
	)
	return &Context{
		Context: ctx,
		OAUTH:   oauthCfg,
	}, nil
}

// Close -
func (ctx *Context) Close() {
	ctx.DB.Close()
}

func createRPCs(cfg config.Config) map[string]noderpc.Pool {
	rpc := make(map[string]noderpc.Pool)
	for network, rpcProvider := range cfg.RPC {
		rpc[network] = noderpc.NewPool([]string{rpcProvider.URI}, time.Second*time.Duration(rpcProvider.Timeout))
	}
	return rpc
}

func createTzKTSvcs(cfg config.Config) map[string]*tzkt.ServicesTzKT {
	svc := make(map[string]*tzkt.ServicesTzKT)
	for network, tzktProvider := range cfg.TzKT {
		svc[network] = tzkt.NewServicesTzKT(network, tzktProvider.ServicesURI, time.Second*time.Duration(tzktProvider.Timeout))
	}
	return svc
}
