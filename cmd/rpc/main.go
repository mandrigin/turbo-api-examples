package main

import (
	"context"
	"os"

	"github.com/ledgerwatch/turbo-geth/cmd/rpcdaemon/cli"
	"github.com/ledgerwatch/turbo-geth/cmd/rpcdaemon/commands"
	"github.com/ledgerwatch/turbo-geth/cmd/rpcdaemon/filters"
	"github.com/ledgerwatch/turbo-geth/cmd/utils"
	"github.com/ledgerwatch/turbo-geth/core"
	"github.com/ledgerwatch/turbo-geth/ethdb"
	"github.com/ledgerwatch/turbo-geth/log"
	"github.com/ledgerwatch/turbo-geth/rpc"

	"github.com/spf13/cobra"
)

func main() {
	cmd, cfg := cli.RootCommand() // to understand how you can configure command, see: https://github.com/spf13/cobra
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		db, backend, err := cli.OpenDB(*cfg)
		if err != nil {
			log.Error("Could not connect to remoteDb", "error", err)
			return nil
		}

		apiList := APIList(db, backend, cfg)
		return cli.StartRpcServer(cmd.Context(), *cfg, apiList)
	}

	if err := cmd.ExecuteContext(utils.RootContext()); err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
}

// Create interface for your API
type SupplyAPI interface {
	GetSupply(ctx context.Context, blockNumber rpc.BlockNumber) (interface{}, error)
}

func APIList(kv ethdb.RoKV, eth core.ApiBackend, cfg *cli.Flags) []rpc.API {
	dbReader := ethdb.NewObjectDatabase(kv.(ethdb.RwKV)) //FIXME: Hack, find a better way.
	api := NewAPI(kv, dbReader)

	customAPIList := []rpc.API{
		{
			Namespace: "tg", // replace it by preferred namespace
			Public:    true,
			Service:   SupplyAPI(api),
			Version:   "1.0",
		},
	}

	f := filters.New(eth)

	// Add default TurboGeth api's
	return commands.APIList(context.TODO(), kv, eth, f, *cfg, customAPIList)
}
