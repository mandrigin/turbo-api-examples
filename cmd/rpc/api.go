package main

import (
	"context"
	"fmt"

	"github.com/mandrigin/turbo-api-examples/supply"

	"github.com/ledgerwatch/turbo-geth/eth/stagedsync/stages"
	"github.com/ledgerwatch/turbo-geth/ethdb"
	"github.com/ledgerwatch/turbo-geth/rpc"

	"github.com/holiman/uint256"
)

var _ SupplyAPI = &API{}

// API - implementation of ExampleApi
type API struct {
	kv ethdb.KV
	db ethdb.Getter
}

type GetSupplyResponse struct {
	BlockNumber uint64 `json:"block_number"`
	Supply      string `json:"supply"`
}

func NewAPI(kv ethdb.KV, db ethdb.Getter) *API {
	return &API{kv: kv, db: db}
}

func (api *API) GetSupply(ctx context.Context, rpcBlockNumber rpc.BlockNumber) (interface{}, error) {
	var err error
	var blockNumber uint64
	if rpcBlockNumber == rpc.PendingBlockNumber {
		return nil, fmt.Errorf("supply for pending block not supported")
	} else if rpcBlockNumber == rpc.LatestBlockNumber {
		blockNumber, _, err = stages.GetStageProgress(api.db, supply.StageID)
		if err != nil {
			return nil, err
		}
	} else {
		blockNumber = uint64(rpcBlockNumber)
	}

	var supplyValue *uint256.Int
	supplyValue, err = supply.GetSupplyForBlock(api.db, blockNumber)
	if err != nil {
		if err == ethdb.ErrKeyNotFound {
			return nil, fmt.Errorf("the ETH supply is not calculated yet for the block %d", blockNumber)
		}
		return nil, err
	}

	return &GetSupplyResponse{
		BlockNumber: blockNumber,
		Supply:      supplyValue.ToBig().String(),
	}, nil
}
