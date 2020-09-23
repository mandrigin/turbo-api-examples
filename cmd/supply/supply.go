package main

import (
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/holiman/uint256"
	"github.com/ledgerwatch/turbo-geth/common/changeset"
	"github.com/ledgerwatch/turbo-geth/common/dbutils"
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

	balances := make(map[[20]byte]*uint256.Int)

	p := message.NewPrinter(language.English)

	previousLog := time.Now()
	for blockNumber >= from {
		count := 0

		if blockNumber == currentStateAt {
			log.Info("Calculating supply for the current state (will be slow)")
			err := db.Walk(dbutils.PlainStateBucket, nil, 0, func(k, v []byte) (bool, error) {
				if !isAccount(k) {
					// for storage entries we just continue
					return true, nil
				}

				err := DecodeAccountRLP(v, k, balances)
				if err != nil {
					return false, err
				}

				count++
				if count%100000 == 0 {
					log.Info(p.Sprintf("Processed %d account records in current state\n", count))
				}

				return true, nil
			})
			if err != nil {
				return err
			}
		} else {
			changeSet, err := db.Get(dbutils.PlainAccountChangeSetBucket, dbutils.EncodeTimestamp(blockNumber))
			if err != nil {
				return err
			}
			err = changeset.AccountChangeSetPlainBytes(changeSet).Walk(func(k, v []byte) error {
				var kk [20]byte
				copy(kk[:], k)

				if len(v) == 0 {
					delete(balances, kk)
					return nil
				}

				return DecodeAccountRLP(v, k, balances)
			})
			if err != nil {
				return err
			}
		}

		supply := uint256.NewInt()
		supply.Clear()
		tmp := uint256.NewInt()
		for _, v := range balances {
			tmp.SetBytes(v[:])
			supply.Add(supply, tmp)
			tmp.Clear()
		}
		count = len(balances)

		now := time.Now()
		timeSpent := now.Sub(previousLog)
		previousLog = now
		blocksLeft := blockNumber - from
		timeLeft := time.Duration(blocksLeft) * timeSpent

		log.Info(p.Sprintf("Stats: blockNum=%d\n\ttotal accounts=%d\n\tsupply=%d\n\ttimePerBlock=%v\n\ttimeLeft=%v\n\tblocksLeft=%d\n", blockNumber, count, supply, timeSpent, timeLeft, blocksLeft))

		if err := db.Put(ethSupplyBucket, keyFromBlockNumber(blockNumber), supply.Bytes()); err != nil {
			return err
		}

		blockNumber--
	}

	return nil
}

// inspired by accounts.Account#DecodeForStorage, but way more light weight
func DecodeAccountRLP(enc []byte, k []byte, balances map[[20]byte]*uint256.Int) error {
	if len(enc) == 0 {
		var kk [20]byte
		copy(kk[:], k)
		delete(balances, kk)
		return nil
	}

	var fieldSet = enc[0]

	if fieldSet&2 <= 0 { // no balance to check
		return nil
	}

	var pos = 1

	if fieldSet&1 > 0 {
		decodeLength := int(enc[pos])

		if len(enc) < pos+decodeLength+1 {
			return fmt.Errorf(
				"malformed CBOR for Account.Nonce: %s, Length %d",
				enc[pos+1:], decodeLength)
		}

		pos += decodeLength + 1
	}

	if fieldSet&2 > 0 {
		decodeLength := int(enc[pos])

		if len(enc) < pos+decodeLength+1 {
			return fmt.Errorf(
				"malformed CBOR for Account.Nonce: %s, Length %d",
				enc[pos+1:], decodeLength)
		}

		var kk [20]byte
		copy(kk[:], k)

		if decodeLength == 0 {
			if balance, ok := balances[kk]; ok && balance != nil {
				balance.Clear()
			}
			return nil
		}

		if balance, ok := balances[kk]; ok && balance != nil {
			balance.SetBytes(enc[pos+1 : pos+decodeLength+1])
		} else {
			balances[kk] = uint256.NewInt().SetBytes(enc[pos+1 : pos+decodeLength+1])
		}
	}

	// we theoretically should never get there
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
