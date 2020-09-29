package supply

import (
	"encoding/binary"

	"github.com/ledgerwatch/turbo-geth/ethdb"

	"github.com/holiman/uint256"
)

const BucketName = "org.ffconsulting.tg.db.ETH_SUPPLY.v2"

func SetSupplyForBlock(db ethdb.Putter, blockNumber uint64, supply *uint256.Int) error {
	return db.Put(BucketName, keyFromBlockNumber(blockNumber), supply.Bytes())
}

func GetSupplyForBlock(db ethdb.Getter, blockNumber uint64) (*uint256.Int, error) {
	bytes, err := db.Get(BucketName, keyFromBlockNumber(blockNumber))
	if err != nil {
		if err == ethdb.ErrKeyNotFound {
			return uint256.NewInt(), nil
		}
		return nil, err
	}

	supply := uint256.NewInt()
	supply.SetBytes(bytes)

	return supply, nil
}

func DeleteSupplyForBlock(db ethdb.Deleter, blockNumber uint64) error {
	return db.Delete(BucketName, keyFromBlockNumber(blockNumber))
}

func keyFromBlockNumber(blockNumber uint64) []byte {
	var buffer [8]byte
	binary.BigEndian.PutUint64(buffer[:], blockNumber)
	return buffer[:]
}
