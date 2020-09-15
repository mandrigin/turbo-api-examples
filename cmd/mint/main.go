package main

import (
	"fmt"
	"os"

	"github.com/ledgerwatch/turbo-geth/eth/stagedsync"
	"github.com/ledgerwatch/turbo-geth/eth/stagedsync/stages"
	"github.com/ledgerwatch/turbo-geth/log"
	"github.com/ledgerwatch/turbo-geth/turbo/node"

	turbocli "github.com/ledgerwatch/turbo-geth/turbo/cli"

	"github.com/urfave/cli"
)

var (
	outputFileNameFlag = cli.StringFlag{
		Name:  "output",
		Value: "mint.csv",
	}

	blockNumberFlag = cli.Int64Flag{
		Name:  "block",
		Value: 0,
	}

	stageID = stages.SyncStage("AVG_GAS_PRICE")
)

func main() {
	app := turbocli.MakeApp(runTurboGeth, append(turbocli.DefaultFlags, outputFileNameFlag, blockNumberFlag))
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func syncStages(ctx *cli.Context) stagedsync.StageBuilders {
	return append(
		stagedsync.DefaultStages(),
		stagedsync.StageBuilder{
			ID: stageID,
			Build: func(world stagedsync.StageParameters) *stagedsync.Stage {
				return &stagedsync.Stage{
					ID:          stageID,
					Description: "Plot Minted Coins",
					ExecFunc: func(s *stagedsync.StageState, _ stagedsync.Unwinder) error {
						fileName := ctx.String(outputFileNameFlag.Name)
						if fileName == "" {
							fileName = "mint.csv"
						}

						blockNumber := ctx.Uint64(blockNumberFlag.Name)
						if s.BlockNumber > blockNumber {
							blockNumber = s.BlockNumber
						}

						err := mint(world.TX, fileName, blockNumber)
						if err != nil {
							return err
						}

						var newBlockNum uint64
						newBlockNum, err = s.ExecutionAt(world.TX)
						if err != nil {
							return err
						}

						return s.DoneAndUpdate(world.TX, newBlockNum)
					},

					UnwindFunc: func(u *stagedsync.UnwindState, s *stagedsync.StageState) error {
						return u.Done(world.TX)
					},
				}
			},
		},
	)
}

func runTurboGeth(ctx *cli.Context) {
	sync := stagedsync.New(
		syncStages(ctx),
		stagedsync.DefaultUnwindOrder(),
	)

	tg := node.New(ctx, sync, node.Params{})

	err := tg.Serve()

	if err != nil {
		log.Error("error while serving a turbo-geth node", "err", err)
	}
}
