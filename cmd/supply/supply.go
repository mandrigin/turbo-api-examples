package main

import (
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/ledgerwatch/turbo-geth/common/dbutils"
	"github.com/ledgerwatch/turbo-geth/core/types/accounts"
	"github.com/ledgerwatch/turbo-geth/ethdb"
	"github.com/ledgerwatch/turbo-geth/log"
)

type SupplyData struct {
	Version  uint
	Balances *big.Int
}

func calculateEthSupply(db ethdb.Database, from, to, currentStateAt uint64) (computed uint64, err error) {
	computed = 0
	err = nil

	blockNumber := to
	if to > currentStateAt {
		// we can't go past the current state
		to = currentStateAt
	}

	log.Info("computing eth supply", "from", from, "to", to)

	supply := big.NewInt(0)

	for blockNumber >= from {
		if blockNumber == currentStateAt {
			log.Info("calculating for the current state")
			var a accounts.Account
			count := 0
			db.Walk(dbutils.PlainStateBucket, nil, 0, func(k, v []byte) (bool, error) {
				if len(k) != 20 {
					return true, nil
				}
				if err = a.DecodeForStorage(v); err != nil {
					return false, err
				}
				count++
				supply.Add(supply, a.Balance.ToBig())
				if count%100000 == 0 {
					fmt.Printf("Processed %dK account records\n", count/1000)
				}
				return true, nil
			})
			fmt.Printf("Total accounts: %d, supply: %d\n", count, supply)
		}
		return 0, nil
	}

	return 0, nil
}

func unwindEthSupply(db ethdb.Database, from, to uint64) (err error) {
	if from <= to {
		// nothing to do here
		return nil
	}
	log.Info("removing eth supply entries", "from", from, "to", to)

	for blockNumber := from; blockNumber > to; blockNumber-- {
		key := keyFromBlockNumber(blockNumber)

		err = db.Delete(ethSupplyBucket, key)

		if err != nil && err == ethdb.ErrKeyNotFound {
			log.Warn("no supply entry found for block", "blockNumber", blockNumber)
			err = nil
		} else {
			return err
		}
	}

	return nil
}

func keyFromBlockNumber(blockNumber uint64) []byte {
	var buffer [8]byte
	binary.BigEndian.PutUint64(buffer[:], blockNumber)
	return buffer[:]
}
