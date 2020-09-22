package main

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"time"

	"github.com/ledgerwatch/turbo-geth/common/dbutils"
	"github.com/ledgerwatch/turbo-geth/core/types/accounts"
	"github.com/ledgerwatch/turbo-geth/ethdb"
	"github.com/ledgerwatch/turbo-geth/log"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

type SupplyData struct {
	Version  uint
	Balances *big.Int
}

func isAccount(k []byte) bool {
	return len(k) == 20
}

func calculateEthSupply(db ethdb.Database, from, currentStateAt uint64) error {
	blockNumber := currentStateAt

	log.Info("computing eth supply", "from", from, "to", currentStateAt)

	balances := make(map[[20]byte]*big.Int)

	p := message.NewPrinter(language.English)

	var previousLog *time.Time
	var previousBlockNumber uint64

	for blockNumber >= from {
		supply := big.NewInt(0)
		count := 0

		if blockNumber == currentStateAt {
			log.Info("Calculating supply for the current state (will be slow)")
			var a accounts.Account
			err := db.Walk(dbutils.PlainStateBucket, nil, 0, func(k, v []byte) (bool, error) {
				if !isAccount(k) {
					// for storage entries we just continue
					return true, nil
				}

				if err := a.DecodeForStorage(v); err != nil {
					return false, err
				}

				count++
				var kk [20]byte
				copy(kk[:], k)
				balances[kk] = a.Balance.ToBig()
				if count%100000 == 0 {
					log.Info(p.Sprintf("Processed %d account records in current state\n", count))
				}

				return true, nil
			})
			if err != nil {
				return err
			}
		} else {
			var a accounts.Account
			accountsMap, _, err := ethdb.RewindDataPlain(db, blockNumber+1, blockNumber)
			if err != nil {
				return err
			}
			for k, v := range accountsMap {
				var kk [20]byte
				copy(kk[:], k)

				if len(v) == 0 {
					delete(balances, kk)
					continue
				}

				if err = a.DecodeForStorage(v); err != nil {
					return err
				}

				balances[kk] = a.Balance.ToBig()
			}
		}
		for _, v := range balances {
			supply.Add(supply, v)
		}
		count = len(balances)

		printLog := false
		speed := 0.0
		if previousLog == nil {
			now := time.Now()
			previousLog = &now
			printLog = true
			previousBlockNumber = blockNumber
		} else {
			now := time.Now()
			timeSpent := now.Sub(*previousLog)
			if timeSpent > 10*time.Second {
				printLog = true
				speed = float64(previousBlockNumber-blockNumber) / float64(timeSpent)
				previousBlockNumber = blockNumber
				previousLog = &now
			}
		}

		if printLog {
			left := "unknown"
			if speed > 0.00000001 {
				left = fmt.Sprintf("%v", time.Duration(float64(blockNumber-from)/speed)*time.Second)
			}
			log.Info(p.Sprintf("Block %d, total accounts: %d, supply: %d, speed %.2f blocks/sec left=%v\n", blockNumber, count, supply, speed, left))
			if speed < 0.000000001 { // first launch is 0.0, but with floats you are never sure
				log.Info("Will calculate supply for historical blocks now")
			}
		}

		if err := db.Put(ethSupplyBucket, keyFromBlockNumber(blockNumber), supply.Bytes()); err != nil {
			return err
		}

		blockNumber--
	}

	return nil
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
