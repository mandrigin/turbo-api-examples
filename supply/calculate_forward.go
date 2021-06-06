package supply

import (
	"context"
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
			// this could be used to compare data with
			// https://github.com/lastmjs/eth-total-supply#total-eth-supply
			log.Info(p.Sprintf("Stats: blockNum=%d\n\tsupply=%d", blockNumber, totalSupply))
		}
	}

	return nil
}

var (
	oldBalanceBuffer = uint256.NewInt()
	newBalanceBuffer = uint256.NewInt()
)

func calculateAtBlock(db ethdb.Database, blockNumber uint64, totalSupply *uint256.Int) error {
	changesetKey := dbutils.EncodeBlockNumber(blockNumber)

	errWalk := changeset.Walk(db, dbutils.PlainAccountChangeSetBucket, changesetKey, 8*8, func(blockN uint64, k, accountDataBeforeBlock []byte) (bool, error) {
		err := decodeAccountBalanceTo(accountDataBeforeBlock, oldBalanceBuffer)
		if err != nil {
			return false, err
		}

		var accountDataAfterBlock []byte
		accountDataAfterBlock, err = getAsOf(db, false, k, blockNumber+1)
		if err != nil && err != ethdb.ErrKeyNotFound {
			fmt.Println("err in get as of", err)
			return false, err
		}

		err = decodeAccountBalanceTo(accountDataAfterBlock, newBalanceBuffer)
		if err != nil {
			return false, err
		}

		totalSupply.Sub(totalSupply, oldBalanceBuffer)
		totalSupply.Add(totalSupply, newBalanceBuffer)

		return true, nil
	})
	return errWalk
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
		accData, err = state.GetAsOf(txHolder.Tx(), false, key, timestamp)
	} else if kvDB, ok := db.(*ethdb.ObjectDatabase); ok {
		tx, err := kvDB.RwKV().BeginRw(context.TODO())
		if err != nil {
			return nil, err
		}
		defer tx.Commit(context.TODO())
		accData, err = state.GetAsOf(tx, false, key, timestamp)
	} else {
		panic(fmt.Sprintf("should be a TX or a object database, got %T %+v", db, db))
	}
	return accData, err
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
