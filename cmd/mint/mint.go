package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ledgerwatch/turbo-geth/common"
	"github.com/ledgerwatch/turbo-geth/common/dbutils"
	"github.com/ledgerwatch/turbo-geth/core/rawdb"
	"github.com/ledgerwatch/turbo-geth/core/types"
	"github.com/ledgerwatch/turbo-geth/ethdb"
	"github.com/ledgerwatch/turbo-geth/log"
	"github.com/ledgerwatch/turbo-geth/rlp"

	"github.com/holiman/uint256"
)

func readBlockNumberFromFile(f *os.File) uint64 {
	csvReader := csv.NewReader(f)
	previousLine := []string{}

	for {
		line, err := csvReader.Read()
		if err == io.EOF {
			if len(previousLine) == 0 {
				log.Error("No fields found in the line")
				return 0
			}
			numberString := previousLine[0]
			number, err := strconv.ParseInt(numberString, 10, 64)
			if err != nil {
				log.Error("Error parsing block number", "numberString", numberString, "err", err)
				return 0
			}
			return uint64(number)
		} else if err != nil {
			log.Error("Something happenned during reading", "err", err)
			return 0
		}
		previousLine = line
	}
}

func mint(db ethdb.Database, csvPath string, block uint64) error {
	if !strings.HasSuffix(csvPath, ".csv") {
		csvPath += ".csv"
	}

	log.Info("plotting minted coins", "block", block, "file", csvPath)

	f, err := os.OpenFile(csvPath, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0777)
	if err != nil {
		return err
	}

	blockNumberFromFile := readBlockNumberFromFile(f)

	fmt.Println("bnff", blockNumberFromFile, "block", block)

	if blockNumberFromFile > block {
		block = blockNumberFromFile
	}

	fmt.Println("bnff", blockNumberFromFile, "block", block)

	f.Seek(0, 0) // reset to the beginning

	defer func() {
		fmt.Println("closed", csvPath)
		f.Close()
	}()

	w := bufio.NewWriter(f)
	defer func() {
		w.Flush()
		fmt.Println("flushed", csvPath)
	}()

	var gwei uint256.Int
	gwei.SetUint64(1000000000)
	blockEncoded := dbutils.EncodeBlockNumber(block)
	canonical := make(map[common.Hash]struct{})

	err = db.Walk(dbutils.HeaderPrefix, blockEncoded, 0, func(k, v []byte) (bool, error) {
		if !dbutils.CheckCanonicalKey(k) {
			return true, nil
		}
		canonical[common.BytesToHash(v)] = struct{}{}
		return true, nil
	})
	if err != nil {
		return err
	}

	log.Info("minted coins: canonical hashes", "count", len(canonical))

	var prevBlock uint64
	var burntGas uint64

	log.Info("walking through block bodies", "fromBlock", block)

	err = db.Walk(dbutils.BlockBodyPrefix, blockEncoded, 0, func(k, v []byte) (bool, error) {
		blockNumber := binary.BigEndian.Uint64(k[:8])
		blockHash := common.BytesToHash(k[8:])
		if _, isCanonical := canonical[blockHash]; !isCanonical {
			return true, nil
		}

		if blockNumber != prevBlock && blockNumber != prevBlock+1 {
			fmt.Printf("Gap [%d-%d]\n", prevBlock, blockNumber-1)
		}

		if blockNumber%1000 == 0 {
			log.Info("walking through block bodies", "fromBlock", block, "current", blockNumber)
		}

		prevBlock = blockNumber
		bodyRlp, err := rawdb.DecompressBlockBody(v)
		if err != nil {
			return false, err
		}
		body := new(types.Body)
		if err := rlp.Decode(bytes.NewReader(bodyRlp), body); err != nil {
			return false, fmt.Errorf("invalid block body RLP: %w", err)
		}
		header := rawdb.ReadHeader(db, blockHash, blockNumber)
		senders := rawdb.ReadSenders(db, blockHash, blockNumber)
		var ethSpent uint256.Int
		var ethSpentTotal uint256.Int
		var totalGas uint256.Int

		count := 0

		for i, tx := range body.Transactions {
			ethSpent.SetUint64(tx.Gas())
			totalGas.Add(&totalGas, &ethSpent)
			if senders[i] == header.Coinbase {
				continue // Mining pool sending payout potentially with abnormally low fee, skip
			}
			ethSpent.Mul(&ethSpent, tx.GasPrice())
			ethSpentTotal.Add(&ethSpentTotal, &ethSpent)
			count++
		}
		if count > 0 {
			ethSpentTotal.Div(&ethSpentTotal, &totalGas)
			ethSpentTotal.Div(&ethSpentTotal, &gwei)
			gasPrice := ethSpentTotal.Uint64()
			burntGas += header.GasUsed
			if _, err := fmt.Fprintf(w, "%d, %d, %d\n", blockNumber, burntGas, gasPrice); err != nil {
				return false, err
			}
		}
		return true, nil
	})

	log.Info("walking through block bodies... DONE")

	return err
}
