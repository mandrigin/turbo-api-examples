package main

import (
	"fmt"
	"os"

	"github.com/mandrigin/turbo-api-examples/supply"

	"github.com/ledgerwatch/turbo-geth/common/dbutils"
	"github.com/ledgerwatch/turbo-geth/eth/stagedsync"
	"github.com/ledgerwatch/turbo-geth/log"
	"github.com/ledgerwatch/turbo-geth/turbo/node"

	"github.com/urfave/cli"

	turbocli "github.com/ledgerwatch/turbo-geth/turbo/cli"
)

func main() {
	app := turbocli.MakeApp(runTurboGeth, turbocli.DefaultFlags)
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runTurboGeth(ctx *cli.Context) {
	sync := stagedsync.New(
		append(stagedsync.DefaultStages(), supply.SyncStage(ctx)),
		stagedsync.DefaultUnwindOrder(),
		stagedsync.OptionalParameters{},
	)

	// Adding a custom bucket where we will store eth supply per block
	params := node.Params{
		CustomBuckets: map[string]dbutils.BucketConfigItem{supply.BucketName: {}},
	}

	tg := node.New(ctx, sync, params)

	err := tg.Serve()

	if err != nil {
		log.Error("error while serving a turbo-geth node", "err", err)
	}
}
