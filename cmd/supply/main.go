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
	stageID = stages.SyncStage("org.ffconsulting.ETH_SUPPLY")
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
					// TODO: calc eth supply between blocks
					return s.DoneAndUpdate(world.TX, to)
				},

				UnwindFunc: func(u *stagedsync.UnwindState, s *stagedsync.StageState) error {
					// TODO: undo eth calculation
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

	tg := node.New(ctx, sync, node.Params{})

	err := tg.Serve()

	if err != nil {
		log.Error("error while serving a turbo-geth node", "err", err)
	}
}
