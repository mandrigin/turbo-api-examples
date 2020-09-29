package supply

import (
	"errors"
	"fmt"

	"github.com/holiman/uint256"
	"github.com/ledgerwatch/turbo-geth/common/changeset"
	"github.com/ledgerwatch/turbo-geth/common/dbutils"
	"github.com/ledgerwatch/turbo-geth/core"
	"github.com/ledgerwatch/turbo-geth/core/state"
	"github.com/ledgerwatch/turbo-geth/ethdb"
	"github.com/ledgerwatch/turbo-geth/log"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// CalculateForwards calculates the ETH supply between blocks `from` and `to` forward in time.
// (from the smaller block to the larger one)
// It is more efficient for the smaller block gaps because it doesn't calculate the current state supply.
// On larger block gaps it is inefficient because of many DB calls.
func CalculateForward(db ethdb.Database, from, to uint64) error {
	if from > to {
		from, to = to, from
	}

	var err error

	p := message.NewPrinter(language.English)

	totalSupply := uint256.NewInt()

	// adjust the initial position based on what we have in the DB
	// calculating forward from N to M depends on supply for the block N-1 being present in the DB
	// (so it doesn't have to recalculate everything from genesis over and over again)
	from, err = GetInitialPosition(db, from, totalSupply)
	if err != nil {
		return err
	}

	for blockNumber := from; blockNumber <= to; blockNumber++ {
		if blockNumber == 0 {
			// calc from genesis
			err = calculateAtGenesis(db, totalSupply)
		} else {
			err = calculateAtBlock(db, blockNumber, totalSupply)
		}

		if err != nil {
			return err
		}

		err = SetSupplyForBlock(db, blockNumber, totalSupply)
		if err != nil {
			return err
		}

		if blockNumber%10_000 == 0 {
			log.Info(p.Sprintf("Stats: blockNum=%d\n\tsupply=%d", blockNumber, totalSupply))
		}
	}

	log.Info("ETH supply calculation... DONE", "from", from, "to", to, "totalSupply", totalSupply.ToBig().String())

	return nil
}

var (
	oldBalanceBuffer = uint256.NewInt()
	newBalanceBuffer = uint256.NewInt()
)

func calculateAtBlock(db ethdb.Database, blockNumber uint64, totalSupply *uint256.Int) error {
	changesetKey := dbutils.EncodeTimestamp(blockNumber)
	changeSet, err := db.Get(dbutils.PlainAccountChangeSetBucket, changesetKey)
	if err != nil && err != ethdb.ErrKeyNotFound {
		fmt.Println("error while searching for a changeset", err)
		return err
	}
	err = changeset.AccountChangeSetPlainBytes(changeSet).Walk(func(k, accountDataBeforeBlock []byte) error {
		err = decodeAccountBalanceTo(accountDataBeforeBlock, oldBalanceBuffer)
		if err != nil {
			return err
		}

		var accountDataAfterBlock []byte
		accountDataAfterBlock, err = getAsOf(db, false, k, blockNumber+1)
		if err != nil && err != ethdb.ErrKeyNotFound {
			fmt.Println("err in get as of", err)
			return err
		}

		err = decodeAccountBalanceTo(accountDataAfterBlock, newBalanceBuffer)
		if err != nil {
			return err
		}

		totalSupply.Sub(totalSupply, oldBalanceBuffer)
		totalSupply.Add(totalSupply, newBalanceBuffer)

		return nil
	})
	return err
}

func calculateAtGenesis(db ethdb.Database, totalSupply *uint256.Int) error {
	genesis := core.DefaultGenesisBlock()

	for _, account := range genesis.Alloc {
		balance, overflow := uint256.FromBig(account.Balance)
		if overflow {
			panic("overflows should not happen in genesis")
		}
		totalSupply.Add(totalSupply, balance)
	}

	return nil
}

// getAsOf is a wrapper for state.GetAsOf for the staged sync.
// we not always are getting `ethdb.KV`, sometimes we are getting `ethdb.Tx`
// this code figures out what object do we get and calls a the right method.
func getAsOf(db ethdb.Database, storage bool, key []byte, timestamp uint64) ([]byte, error) {
	var accData []byte
	var err error
	if txHolder, ok := db.(ethdb.HasTx); ok {
		accData, err = getAsOfTx(txHolder.Tx(), false, key, timestamp)
	} else if kvHolder, ok := db.(ethdb.HasKV); ok {
		accData, err = state.GetAsOf(kvHolder.KV(), false, key, timestamp)
	} else {
		panic("should be either TX or KV")
	}
	return accData, err
}

// getAsOfTx is a reimplementation of `state.GetAsOf` but for time when we already have
// an open transaction at hand.
func getAsOfTx(tx ethdb.Tx, storage bool, key []byte, timestamp uint64) ([]byte, error) {
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
func decodeAccountBalanceTo(enc []byte, to *uint256.Int) error {
	to.Clear()
	if len(enc) == 0 {
		return nil
	}

	var fieldSet = enc[0]

	// if an account has 0 balance...
	if fieldSet&2 <= 0 {
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

	to.SetBytes(enc[pos+1 : pos+decodeLength+1])
	return nil
}
