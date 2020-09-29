package supply

import (
	"github.com/ledgerwatch/turbo-geth/eth/stagedsync"
	"github.com/ledgerwatch/turbo-geth/eth/stagedsync/stages"

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

					err = CalculateForward(world.TX, from, currentStateAt)
					if err != nil {
						return err
					}
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
