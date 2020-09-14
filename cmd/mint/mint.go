package main

import (
	"bufio"
	"bytes"
	"context"
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

func mint(db *ethdb.ObjectDatabase, csvPath string, block uint64) error {
	if !strings.HasSuffix(csvPath, ".csv") {
		csvPath += ".csv"
	}

	f, err := os.OpenFile(csvPath, os.O_APPEND, 0777)
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
	if err1 := db.KV().View(context.Background(), func(tx ethdb.Tx) error {
		c := tx.Cursor(dbutils.HeaderPrefix)
		// This is a mapping of contractAddress + incarnation => CodeHash
		for k, v, err := c.Seek(blockEncoded); k != nil; k, v, err = c.Next() {
			if err != nil {
				return err
			}
			// Skip non relevant records
			if !dbutils.CheckCanonicalKey(k) {
				continue
			}
			canonical[common.BytesToHash(v)] = struct{}{}
		}
		c = tx.Cursor(dbutils.BlockBodyPrefix)
		var prevBlock uint64
		var burntGas uint64
		for k, v, err := c.Seek(blockEncoded); k != nil; k, v, err = c.Next() {
			if err != nil {
				return err
			}
			blockNumber := binary.BigEndian.Uint64(k[:8])
			blockHash := common.BytesToHash(k[8:])
			if _, isCanonical := canonical[blockHash]; !isCanonical {
				continue
			}
			if blockNumber != prevBlock && blockNumber != prevBlock+1 {
				fmt.Printf("Gap [%d-%d]\n", prevBlock, blockNumber-1)
			}
			prevBlock = blockNumber
			bodyRlp, err := rawdb.DecompressBlockBody(v)
			if err != nil {
				return err
			}
			body := new(types.Body)
			if err := rlp.Decode(bytes.NewReader(bodyRlp), body); err != nil {
				return fmt.Errorf("invalid block body RLP: %w", err)
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
		}
		return nil
	}); err1 != nil {
		return err1
	}
	return nil
}
