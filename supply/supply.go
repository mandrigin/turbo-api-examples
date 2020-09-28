package supply

import (
	"fmt"
	"math/big"

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

func Calculate(db ethdb.Database, from, currentStateAt uint64) error {
	blockNumber := currentStateAt

	log.Info("computing eth supply", "from", from, "to", currentStateAt)

	accountBalances := make(map[common.Address]*uint256.Int)

	p := message.NewPrinter(language.English)

	totalSupply := uint256.NewInt()

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

	return nil
}

// inspired by accounts.Account#DecodeForStorage, but way more light weight
// it uses some knowledge about how turbo-geth stores accounts
// but it makes the operations with very good performance
func decodeAccountAndUpdateBalance(enc []byte, address common.Address, balances map[common.Address]*uint256.Int, totalSupply *uint256.Int) error {
	// if an account was removed...
	if len(enc) == 0 {
		// if it was in the list...
		if balance, ok := balances[address]; ok && balance != nil {
			// decrease total supply
			totalSupply.Sub(totalSupply, balance)
			// remove the account from the list of balances
			delete(balances, address)
		}
		return nil
	}

	var fieldSet = enc[0]

	// if an account has 0 balance...
	if fieldSet&2 <= 0 {
		// ...and it had some balance before
		if balance, ok := balances[address]; ok && balance != nil {
			// set it to 0 and update the value
			totalSupply.Sub(totalSupply, balance)

			balance.Clear()
			balances[address] = balance
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
	if balance, ok := balances[address]; ok && balance != nil {
		// remove the old value
		totalSupply.Sub(totalSupply, balance)

		// update value in-place so we don't overload the GC
		balance.SetBytes(enc[pos+1 : pos+decodeLength+1])

		// add the new value
		totalSupply.Add(totalSupply, balance)

		// and update the map
		balances[address] = balance
		return nil
	}

	// add a new entry to if there wasn't an existing one
	balance := uint256.NewInt().SetBytes(enc[pos+1 : pos+decodeLength+1])
	balances[address] = balance

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
