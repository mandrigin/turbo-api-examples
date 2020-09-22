package main

import (
	"fmt"
	"os"

	"github.com/ledgerwatch/turbo-geth/common/dbutils"
	"github.com/ledgerwatch/turbo-geth/eth/stagedsync"
	"github.com/ledgerwatch/turbo-geth/eth/stagedsync/stages"
	"github.com/ledgerwatch/turbo-geth/log"
	"github.com/ledgerwatch/turbo-geth/turbo/node"

	turbocli "github.com/ledgerwatch/turbo-geth/turbo/cli"

	"github.com/urfave/cli"
)

var (
	stageID         = stages.SyncStage("org.ffconsulting.ETH_SUPPLY")
	ethSupplyBucket = "org.ffconsulting.tg.db.ETH_SUPPLY"
)

func main() {
	app := turbocli.MakeApp(runTurboGeth, turbocli.DefaultFlags)
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func ethSupplyStage(ctx *cli.Context) stagedsync.StageBuilder {
	return stagedsync.StageBuilder{
		ID: stageID,
		Build: func(world stagedsync.StageParameters) *stagedsync.Stage {
			return &stagedsync.Stage{
				ID:          stageID,
				Description: "Calculate ETH supply",
				ExecFunc: func(s *stagedsync.StageState, _ stagedsync.Unwinder) error {
					from := s.BlockNumber
					to, err := s.ExecutionAt(world.TX)
					if err != nil {
						return err
					}
					fmt.Println("from", from, "to", to)
					computed, err := calculateEthSupply(from, to)
					if err != nil {
						return err
					}
					return s.DoneAndUpdate(world.TX, computed)
				},

				UnwindFunc: func(u *stagedsync.UnwindState, s *stagedsync.StageState) error {
					err := unwindEthSupply(u.UnwindPoint)
					if err != nil {
						return err
					}
					return u.Done(world.TX)
				},
			}
		},
	}
}

func runTurboGeth(ctx *cli.Context) {
	sync := stagedsync.New(
		append(stagedsync.DefaultStages(), ethSupplyStage(ctx)),
		stagedsync.DefaultUnwindOrder(),
	)

	// Adding a custom bucket where we will store eth supply per block
	params := node.Params{
		CustomBuckets: map[string]dbutils.BucketConfigItem{
			ethSupplyBucket: {},
		},
	}

	tg := node.New(ctx, sync, params)

	err := tg.Serve()

	if err != nil {
		log.Error("error while serving a turbo-geth node", "err", err)
	}
}
