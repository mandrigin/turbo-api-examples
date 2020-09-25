package main

import (
	"context"

	"github.com/ledgerwatch/turbo-geth/ethdb"
	"github.com/ledgerwatch/turbo-geth/rpc"
)

var _ SupplyAPI = &API{}

// API - implementation of ExampleApi
type API struct {
	kv ethdb.KV
	db ethdb.Getter
}

func NewAPI(kv ethdb.KV, db ethdb.Getter) *API {
	return &API{kv: kv, db: db}
}

func (api *API) GetSupplyInfo(ctx context.Context, blockNumber rpc.BlockNumber) (interface{}, error) {
	return nil, nil
}
