package main

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/holiman/uint256"
	"github.com/ledgerwatch/turbo-geth/common"
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

	supply := uint256.NewInt()

	changedAccounts := 0
	for blockNumber >= from {
		count := 0

		totalRemove.Clear()
		totalRemoveAccount.Clear()
		totalAdd.Clear()
		totalCreated.Clear()

		if blockNumber == currentStateAt {
			log.Info("Calculating supply for the current state (will be slow)")
			err := db.Walk(dbutils.PlainStateBucket, nil, 0, func(k, v []byte) (bool, error) {
				if !isAccount(k) {
					// for storage entries we just continue
					return true, nil
				}

				var kk [20]byte
				copy(kk[:], k)

				err := DecodeAccountRLP(v, kk, balances, supply, false)
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
			changedAccounts = 0
			err = changeset.AccountChangeSetPlainBytes(changeSet).Walk(func(k, v []byte) error {
				trace := isInterestingAccount(k)

				var kk [20]byte
				copy(kk[:], k)

				if trace {
					fmt.Printf("BEGIN_TRACE: block=%d k=%x kk=%x idx=%d\n", blockNumber, k, kk, changedAccounts)
				}
				defer func() {
					if trace {
						fmt.Printf("END_TRACE: block=%d k=%x kk=%x idx=%d\n", blockNumber, k, kk, changedAccounts)
					}
				}()

				changedAccounts++

				return DecodeAccountRLP(v, kk, balances, supply, trace)
			})
			if err != nil {
				return err
			}
		}

		count = len(balances)

		if blockNumber%1_000_000 == 0 {
			log.Info(p.Sprintf("Stats: blockNum=%d\n\ttotal accounts with non zero balance=%d\n\tsupply=%d", blockNumber, count, supply))
		}

		for _, acc := range accountsToTrace {
			if _, ok := disappearedAccounts[acc]; !ok {
				addr := common.HexToAddress(fmt.Sprintf("0x%s", acc))
				if _, ok := balances[addr]; !ok {
					fmt.Printf("Account %s disappered in the block %d\n", acc, blockNumber)
					disappearedAccounts[acc] = struct{}{}
				}
			}
		}

		if blockNumber == 0 {
			for k, b := range balances {
				fmt.Printf("Accounts in Genesis %x %v\n", k, b)
			}
		}

		/*
			if err := db.Put(ethSupplyBucket, keyFromBlockNumber(blockNumber), supply.Bytes()); err != nil {
				return err
			}
		*/

		blockNumber--
	}

	os.Exit(0) // we are done and I don't want the sync to continue
	panic("boomio")
}

var accountsToTrace = []string{
	"e83604E4fF6Be7f96F6018D3eC3072ec525DFF6b",
	"5c6F36AF90AB1A656c6ec8c7d521512762bba3E1",
	"d6a7Ac4De7b510F0e8De519d973fA4C01Ba83400",
}

var disappearedAccounts = make(map[string]struct{})

func isInterestingAccount(k []byte) bool {
	for _, acc := range accountsToTrace {
		if strings.EqualFold(fmt.Sprintf("%x", k), acc) {
			return true
		}
	}
	return false
}

var totalRemove *uint256.Int = uint256.NewInt()
var totalAdd *uint256.Int = uint256.NewInt()
var totalCreated *uint256.Int = uint256.NewInt()
var totalRemoveAccount *uint256.Int = uint256.NewInt()

// inspired by accounts.Account#DecodeForStorage, but way more light weight
func DecodeAccountRLP(enc []byte, kk [20]byte, balances map[[20]byte]*uint256.Int, supply *uint256.Int, trace bool) error {
	if len(enc) == 0 {
		if balance, ok := balances[kk]; ok && balance != nil {
			if trace {
				fmt.Printf("delete: %v -> 0 (delete acc)\n", balance)
			}
			supply.Sub(supply, balance)
			totalRemoveAccount.Add(totalRemoveAccount, balance)
		}
		delete(balances, kk)
		return nil
	}

	var fieldSet = enc[0]

	if fieldSet&2 <= 0 { // no balance to check
		if balance, ok := balances[kk]; ok && balance != nil {
			if trace {
				fmt.Printf("update: %v -> 0 (delete acc)\n", balance)
			}
			supply.Sub(supply, balance)
			totalRemove.Add(totalRemoveAccount, balance)
		}
		delete(balances, kk)
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

		/*
			if decodeLength == 0 {
				if balance, ok := balances[kk]; ok && balance != nil {
					//fmt.Printf("update %v -> 0\n", balance)
					supply.Sub(supply, balance)
					balance.Clear()
					balances[kk] = balance
				}
				return nil
			}
		*/

		if oldBalance, ok := balances[kk]; ok && oldBalance != nil {
			if trace {
				fmt.Printf("update: %v ->", oldBalance)
			}
			supply.Sub(supply, oldBalance)
			totalRemove.Add(totalRemove, oldBalance)
			oldBalance.SetBytes(enc[pos+1 : pos+decodeLength+1])
			supply.Add(supply, oldBalance)
			totalAdd.Add(totalAdd, oldBalance)
			oldBalance.SetBytes(enc[pos+1 : pos+decodeLength+1])
			if trace {
				fmt.Printf("%v\n", oldBalance)
			}
			balances[kk] = oldBalance
		} else {
			balance := uint256.NewInt().SetBytes(enc[pos+1 : pos+decodeLength+1])
			if trace {
				fmt.Printf("create: 0 -> %v\n", balance)
			}
			supply.Add(supply, balance)
			totalCreated.Add(totalAdd, balance)
			balances[kk] = balance
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
