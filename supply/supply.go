package supply

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/holiman/uint256"
	"github.com/ledgerwatch/turbo-geth/common/dbutils"
	"github.com/ledgerwatch/turbo-geth/core"
	"github.com/ledgerwatch/turbo-geth/core/state"
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

func Calculate(db ethdb.Database, from, currentStateAt uint64) error {
	log.Info("computing eth supply", "from", from, "to", currentStateAt)

	p := message.NewPrinter(language.English)

	totalSupply := uint256.NewInt()

	for blockNumber := from; blockNumber < currentStateAt; blockNumber++ {
		var err error
		var ethHoldersCount int
		if blockNumber == currentStateAt {
			log.Warn("calculating supply at the tip is not supported yet")
		} else if blockNumber == 0 {
			// calc from genesis
			ethHoldersCount, err = calculateAtGenesis(db, totalSupply)
			log.Info(p.Sprintf("Stats: blockNum=%d\n\ttotal accounts with non zero balance=%d\n\tsupply=%d", blockNumber, ethHoldersCount, totalSupply))
		} else {
			panic("boom")
		}

		if err != nil {
			return err
		}
	}

	/*
		for blockNumber >= from {
			if blockNumber == currentStateAt {
				log.Info("Calculating supply for the current state (will be slow)")
				processed := 0
				err := db.Walk(dbutils.PlainStateBucket, nil, 0, func(k, v []byte) (bool, error) {
					if !isAccount(k) {
						// for storage entries we just continue
						return true, nil
					}

					address := common.BytesToAddress(k)

					err := decodeAccountAndUpdateBalance(v, address, accountBalances, totalSupply)
					if err != nil {
						return false, err
					}

					processed++
					if processed%100000 == 0 {
						log.Info(p.Sprintf("Processed %d account records in current state\n", processed))
					}

					return true, nil
				})
				if err != nil {
					return err
				}
			} else {
				// to get the state for blockNumber if we have the state for blockNuber + 1
				// we need to apply changesets by key blockNumber + 1 to the state
				changesetKey := dbutils.EncodeTimestamp(blockNumber + 1)
				changeSet, err := db.Get(dbutils.PlainAccountChangeSetBucket, changesetKey)
				if err != nil {
					return err
				}
				err = changeset.AccountChangeSetPlainBytes(changeSet).Walk(func(k, v []byte) error {
					address := common.BytesToAddress(k)
					return decodeAccountAndUpdateBalance(v, address, accountBalances, totalSupply)
				})
				if err != nil {
					return err
				}
			}

			ethHoldersCount := len(accountBalances) // those who have non-zero balance

			if blockNumber%10_000 == 0 {
				log.Info(p.Sprintf("Stats: blockNum=%d\n\ttotal accounts with non zero balance=%d\n\tsupply=%d", blockNumber, ethHoldersCount, totalSupply))
			}

			if err := SetSupplyForBlock(db, blockNumber, totalSupply); err != nil {
				return err
			}

			if blockNumber == 0 {
				break
			}

			blockNumber--
		}
	*/

	return nil
}

func calculateAtGenesis(db ethdb.Database, totalSupply *uint256.Int) (int, error) {
	var genesis *core.Genesis

	holderCount := 0

	block, _, _, err := genesis.ToBlock(nil, false)
	if err != nil {
		return 0, err
	}

	zero := uint256.NewInt()

	for _, tx := range block.Transactions() {
		recipient := tx.To()
		var accData []byte
		var err error
		if txHolder, ok := db.(ethdb.HasTx); ok {
			accData, err = GetAsOfTx(txHolder.Tx(), false, recipient[:], 1)
		} else {
			accData, err = state.GetAsOf(db.(ethdb.KV), false, recipient[:], 1)
		}
		if err != nil {
			return 0, err
		}
		err = decodeAccountAndUpdateBalance(accData, zero, totalSupply)
		if err != nil {
			return 0, err
		}

		holderCount++
	}

	return holderCount, nil
}

func GetAsOfTx(tx ethdb.Tx, storage bool, key []byte, timestamp uint64) ([]byte, error) {
	var dat []byte
	v, err := state.FindByHistory(tx, storage, key, timestamp)
	if err == nil {
		dat = make([]byte, len(v))
		copy(dat, v)
		return dat, nil
	}
	if !errors.Is(err, ethdb.ErrKeyNotFound) {
		return nil, err
	}
	v, err = tx.Get(dbutils.PlainStateBucket, key)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, ethdb.ErrKeyNotFound
	}
	dat = make([]byte, len(v))
	copy(dat, v)
	return dat, nil
}

// inspired by accounts.Account#DecodeForStorage, but way more light weight
// it uses some knowledge about how turbo-geth stores accounts
// but it makes the operations with very good performance
func decodeAccountAndUpdateBalance(enc []byte, prevBalance *uint256.Int, totalSupply *uint256.Int) error {
	// if an account was removed...
	if len(enc) == 0 {
		if prevBalance.Uint64() > 0 {
			totalSupply.Sub(totalSupply, prevBalance)
		}
		return nil
	}

	var fieldSet = enc[0]

	// if an account has 0 balance...
	if fieldSet&2 <= 0 {
		// ...and it had some balance before
		if prevBalance.Uint64() > 0 {
			// set it to 0 and update the value
			totalSupply.Sub(totalSupply, prevBalance)
		}
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

	decodeLength := int(enc[pos])

	if len(enc) < pos+decodeLength+1 {
		return fmt.Errorf(
			"malformed CBOR for Account.Nonce: %s, Length %d",
			enc[pos+1:], decodeLength)
	}

	// update existing balance if we found it
	if prevBalance.Uint64() > 0 {
		// remove the old value
		totalSupply.Sub(totalSupply, prevBalance)
	}

	balance := prevBalance // perf optimization on reuse so we don't alloc unneeded stuff
	balance.Clear()

	// update value in-place so we don't overload the GC
	balance.SetBytes(enc[pos+1 : pos+decodeLength+1])

	// add the new value
	totalSupply.Add(totalSupply, balance)

	return nil
}

func Unwind(db ethdb.Database, from, to uint64) (err error) {
	if from <= to {
		// nothing to do here
		return nil
	}
	log.Info("removing eth supply entries", "from", from, "to", to)

	for blockNumber := from; blockNumber > to; blockNumber-- {
		err = DeleteSupplyForBlock(db, blockNumber)
		if err != nil && err == ethdb.ErrKeyNotFound {
			log.Warn("no supply entry found for block", "blockNumber", blockNumber)
			err = nil
		} else {
			return err
		}
	}

	return nil
}
