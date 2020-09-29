package supply

import (
	"github.com/ledgerwatch/turbo-geth/eth/stagedsync"
	"github.com/ledgerwatch/turbo-geth/eth/stagedsync/stages"
	"github.com/ledgerwatch/turbo-geth/ethdb"
	"github.com/ledgerwatch/turbo-geth/log"

	"github.com/urfave/cli"
)

var (
	StageID = stages.SyncStage("org.ffconsulting.ETH_SUPPLY")
)

func SyncStage(ctx *cli.Context) stagedsync.StageBuilder {
	return stagedsync.StageBuilder{
		ID: StageID,
		Build: func(world stagedsync.StageParameters) *stagedsync.Stage {
			return &stagedsync.Stage{
				ID:          StageID,
				Description: "Calculate ETH supply",
				ExecFunc: func(s *stagedsync.StageState, _ stagedsync.Unwinder) error {
					from := s.BlockNumber
					currentStateAt, err := s.ExecutionAt(world.TX)
					if err != nil {
						return err
					}

					if from > currentStateAt {
						log.Info("Computing ETH supply... DONE", "from", from, "to", currentStateAt)
						return nil
					}

					// pick the most efficient way of calculating ETH supply
					// backward calculation is way faster but requires about 25 minutes
					// of reading the current state.
					// calculating forward doesn't require any of that but it is way slower at
					// higher block numbers.
					// So at about 50.000 blocks, it is faster to read the current state and then
					// quickly calculate supply for everything else.
					// Lower than that it, it is usually faster to just calculate supply.
					// This code will result for most people that for genesis sync it will use backward
					// calculation, and for being near the tip -- forward one.
					if currentStateAt-from >= 50_000 {
						log.Info("Computing Eth supply backward", "from", from, "to", currentStateAt)
						err = CalculateBackward(world.TX, from, currentStateAt)
					} else {
						log.Info("Computing Eth supply forward", "from", from, "to", currentStateAt)
						err = CalculateForward(world.TX, from, currentStateAt)
					}

					if err != nil {
						return err
					}

					log.Info("ETH supply calculation... DONE. use `tg_getSupply` to get values", "from", from, "to", to)
					return s.DoneAndUpdate(world.TX, currentStateAt)
				},

				UnwindFunc: func(u *stagedsync.UnwindState, s *stagedsync.StageState) error {
					err := Unwind(world.TX, s.BlockNumber, u.UnwindPoint)
					if err != nil {
						return err
					}
					return u.Done(world.TX)
				},
			}
		},
	}
}

func Unwind(db ethdb.Database, from, to uint64) (err error) {
	if from > to {
		from, to = to, from
	}
	if from == to {
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
