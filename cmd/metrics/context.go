package main

import (
	"time"

	"github.com/baking-bad/bcdhub/internal/config"
	"github.com/baking-bad/bcdhub/internal/contractparser/consts"
	"github.com/baking-bad/bcdhub/internal/database"
	"github.com/baking-bad/bcdhub/internal/elastic"
	"github.com/baking-bad/bcdhub/internal/mq"
	"github.com/baking-bad/bcdhub/internal/noderpc"
)

// Context -
type Context struct {
	DB      database.DB
	ES      *elastic.Elastic
	RPC     map[string]noderpc.Pool
	MQ      *mq.MQ
	Aliases map[string]string
}

func newContext(cfg config.Config) (*Context, error) {
	es := elastic.WaitNew([]string{cfg.Elastic.URI})
	RPCs := createRPCs(cfg)
	messageQueue, err := mq.NewReceiver(cfg.RabbitMQ.URI, cfg.RabbitMQ.Queues, "metrics")
	if err != nil {
		return nil, err
	}

	db, err := database.New(cfg.DB.ConnString)
	if err != nil {
		return nil, err
	}

	aliases, err := db.GetAliasesMap(consts.Mainnet)
	if err != nil {
		return nil, err
	}

	return &Context{
		DB:      db,
		ES:      es,
		RPC:     RPCs,
		MQ:      messageQueue,
		Aliases: aliases,
	}, nil
}

func (ctx *Context) close() {
	ctx.MQ.Close()
	ctx.DB.Close()
}

func createRPCs(cfg config.Config) map[string]noderpc.Pool {
	rpc := make(map[string]noderpc.Pool)
	for network, rpcProvider := range cfg.RPC {
		rpc[network] = noderpc.NewPool([]string{rpcProvider.URI}, time.Second*time.Duration(rpcProvider.Timeout))
	}
	return rpc
}

// func (ctx *Context) getRPC(network string) (noderpc.Pool, error) {
// 	if rpc, ok := ctx.RPC[network]; ok {
// 		return rpc, nil
// 	}
// 	return nil, fmt.Errorf("Unknown rpc network %s", network)
// }
