package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strings"

	"github.com/ledgerwatch/turbo-geth/common"
	"github.com/ledgerwatch/turbo-geth/common/dbutils"
	"github.com/ledgerwatch/turbo-geth/core/rawdb"
	"github.com/ledgerwatch/turbo-geth/core/types"
	"github.com/ledgerwatch/turbo-geth/ethdb"
	"github.com/ledgerwatch/turbo-geth/rlp"

	"github.com/holiman/uint256"
)

func mint(db ethdb.Database, csvPath string, block uint64) error {
	if !strings.HasSuffix(csvPath, ".csv") {
		csvPath += ".csv"
	}

	f, err := os.OpenFile(csvPath, os.O_CREATE|os.O_APPEND, 0777)
	if err != nil {
		return err
	}

	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()

	var gwei uint256.Int
	gwei.SetUint64(1000000000)
	blockEncoded := dbutils.EncodeBlockNumber(block)
	canonical := make(map[common.Hash]struct{})

	err = db.Walk(dbutils.HeaderPrefix, blockEncoded, 0, func(k, v []byte) (bool, error) {
		if !dbutils.CheckCanonicalKey(k) {
			return true, nil
		}
		canonical[common.BytesToHash(v)] = struct{}{}
		return true, nil
	})
	if err != nil {
		return err
	}

	var prevBlock uint64
	var burntGas uint64
	err = db.Walk(dbutils.BlockBodyPrefix, blockEncoded, 0, func(k, v []byte) (bool, error) {
		blockNumber := binary.BigEndian.Uint64(k[:8])
		blockHash := common.BytesToHash(k[8:])
		if _, isCanonical := canonical[blockHash]; !isCanonical {
			return true, nil
		}
		if blockNumber != prevBlock && blockNumber != prevBlock+1 {
			fmt.Printf("Gap [%d-%d]\n", prevBlock, blockNumber-1)
		}

		prevBlock = blockNumber
		bodyRlp, err := rawdb.DecompressBlockBody(v)
		if err != nil {
			return false, err
		}
		body := new(types.Body)
		if err := rlp.Decode(bytes.NewReader(bodyRlp), body); err != nil {
			return false, fmt.Errorf("invalid block body RLP: %w", err)
		}
		header := rawdb.ReadHeader(db, blockHash, blockNumber)
		senders := rawdb.ReadSenders(db, blockHash, blockNumber)
		var ethSpent uint256.Int
		var ethSpentTotal uint256.Int
		var totalGas uint256.Int
		count := 0
		for i, tx := range body.Transactions {
			ethSpent.SetUint64(tx.Gas())
			totalGas.Add(&totalGas, &ethSpent)
			if senders[i] == header.Coinbase {
				continue // Mining pool sending payout potentially with abnormally low fee, skip
			}
			ethSpent.Mul(&ethSpent, tx.GasPrice())
			ethSpentTotal.Add(&ethSpentTotal, &ethSpent)
			count++
		}
		if count > 0 {
			ethSpentTotal.Div(&ethSpentTotal, &totalGas)
			ethSpentTotal.Div(&ethSpentTotal, &gwei)
			gasPrice := ethSpentTotal.Uint64()
			burntGas += header.GasUsed
			fmt.Fprintf(w, "%d, %d\n", burntGas, gasPrice)
		}
		return true, nil
	})

	return err
}
