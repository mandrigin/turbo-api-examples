package supply

import (
	"errors"
	"fmt"
	"math/big"
	"os"

	"github.com/holiman/uint256"
	"github.com/ledgerwatch/turbo-geth/common"
	"github.com/ledgerwatch/turbo-geth/common/changeset"
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

	validationSupply := uint256.NewInt()

	for blockNumber := from; blockNumber < currentStateAt; blockNumber++ {
		var err error
		if blockNumber == 0 {
			// calc from genesis
			err = calculateAtGenesis(db, totalSupply)
		} else {
			err = calculateAtBlock(db, blockNumber, totalSupply)
		}

		if err != nil {
			return err
		}

		dbKey := keyFromBlockNumber(blockNumber)
		err = db.Put(BucketNameV2, dbKey, common.CopyBytes(totalSupply.Bytes()))
		if err != nil {
			return err
		}

		dataFromPrevRun, err := db.Get(BucketName, dbKey)
		if err != nil {
			panic(err)
		}

		validationSupply.Clear()
		validationSupply.SetBytes(dataFromPrevRun)

		if !totalSupply.Eq(validationSupply) {
			log.Error(p.Sprintf("Mismatch: blockNum=%d\n\tsupply(old)=%d suppply(new)=%d", blockNumber, validationSupply, totalSupply))
			os.Exit(1)
			panic("boom")
		}

		if blockNumber%1000 == 0 {
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
	changesetKey := dbutils.EncodeTimestamp(blockNumber)
	changeSet, err := db.Get(dbutils.PlainAccountChangeSetBucket, changesetKey)
	if err != nil {
		return err
	}
	err = changeset.AccountChangeSetPlainBytes(changeSet).Walk(func(k, accountDataBeforeBlock []byte) error {
		err = decodeAccountBalanceTo(accountDataBeforeBlock, oldBalanceBuffer)
		if err != nil {
			return err
		}

		fmt.Printf("Blk %d Old balance buffer: %d\n", blockNumber, oldBalanceBuffer)

		var accountDataAfterBlock []byte
		accountDataAfterBlock, err = GetAsOf(db, false, k, blockNumber+1)
		if err != nil {
			return err
		}

		err = decodeAccountBalanceTo(accountDataAfterBlock, newBalanceBuffer)
		if err != nil {
			return err
		}

		fmt.Printf("Blk %d Old balance buffer: %d\n", blockNumber, newBalanceBuffer)

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

func GetAsOf(db ethdb.Database, storage bool, key []byte, timestamp uint64) ([]byte, error) {
	var accData []byte
	var err error
	if txHolder, ok := db.(ethdb.HasTx); ok {
		accData, err = GetAsOfTx(txHolder.Tx(), false, key, timestamp)
	} else {
		accData, err = state.GetAsOf(db.(ethdb.KV), false, key, timestamp)
	}
	return accData, err
}

func GetAsOfTx(tx ethdb.Tx, storage bool, key []byte, timestamp uint64) ([]byte, error) {
	fmt.Println("Get as of (TX)")
	var dat []byte
	v, err := state.FindByHistory(tx, storage, key, timestamp)
	if err == nil {
		fmt.Println("found in history")
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
	fmt.Println("found in curent state history")
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
