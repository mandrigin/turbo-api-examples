`cmd/mint`: total burned gas by block N to gas price
---

This is an example using turbo-geth API.

## Usage

```
go run ./cmd/mint --datadir <path to turbo-geth datadir> --output <path to output csv> --block <block to beging calculation from>
```

It computes cumulative number of gas from the specified **block** and stores it to the CSV file provided by **output** parameter.

## Leveraging Turbo-API & Staged Sync

This example is an example of [turbo-api](https://github.com/ledgerwatch/turbo-geth/tree/master/turbo) and to make a custom stage for
[staged sync](https://github.com/ledgerwatch/turbo-geth/tree/master/eth/stagedsync).

It runs the whole turbo-geth node under the hood and plugs one stage and two
additional command-line flags to it.

### Setting up

You need to use go modules.

Initialize modules: `go mod init`

Add turbo-geth api: `go install github.com/ledgerwatch/turbo-geth`

### Turbo-API: Running A Node

You can create a turbo-geth node by using `turbocli.MakeApp` and providing the
main function to it.

Then, turbo-api will take care of parsing command-line arguments (both default
and custom) and provides a pre-generated `*cli.Context` into the main function.

Main function is fully user-defined, but if you just want to run a node, it
usually looks like that.

This creates the node with default [staged sync](https://github.com/ledgerwatch/turbo-geth/tree/master/eth/stagedsync) stages
and the default *unwind order*.

```go
func runTurboGeth(ctx *cli.Context) {
	sync := stagedsync.New(
		stagedsync.DefaultStages(),
		stagedsync.DefaultUnwindOrder(),
	)

	tg := node.New(ctx, sync, node.Params{})

	err := tg.Serve()

	if err != nil {
		log.Error("error while serving a turbo-geth node", "err", err)
	}
}
```

**Unwind Order** defines which stages should be unwound before which. They
aren't always in reverse, because, for instance, you can only update the
transaction pool after you completed the full unwind of other stages.

`tg.Serve()` creates the turbo-geth node and blocks execution until the node is
stopped by a user.

### Turbo-API: Custom Flags

To add custom flags to your binary, you need to first declare them:

```go
var (
	outputFileNameFlag = cli.StringFlag{
		Name:  "output",
		Value: "mint.csv",
	}

	blockNumberFlag = cli.Int64Flag{
		Name:  "block",
		Value: 0,
	}
)
```

Then, you can add these flags to the default ones when creating the new turbo-geth node.

```go
app := turbocli.MakeApp(runTurboGeth, append(
        turbocli.DefaultFlags, 
        outputFileNameFlag,  // <-- our custom flag 1
        blockNumberFlag,     // <-- our custom flag 2
))
```

After that, you can get the values everywhere in the code that has access to
the `ctx *cli.Context` of the current application.

Turbo-API always provides this context to the main function of the app (the
first parameter in `turbocli.MakeApp`, in our case it is called `runTurboGeth`).

```go
func runTurboGeth(ctx *cli.Context) {
	sync := stagedsync.New(
		syncStages(ctx),
		...
}
```

Then we pass it to the function `syncStages` that we declared, so we can have
access to this `*cli.Context` inside our custom stage.

```go
func syncStages(ctx *cli.Context) stagedsync.StageBuilders {
	return append(
		stagedsync.DefaultStages(),
        ...
        ...
            fileName := ctx.String(outputFileNameFlag.Name) // <-- getting a string value
            if fileName == "" {
                fileName = "mint.csv"
            }

            blockNumber := ctx.Uint64(blockNumberFlag.Name) // <-- getting a block number
        ...
```

### Turbo-API: Adding Custom Sync Stages

One of the most common use-case as envisioned by the authors is altering sync stages.

You can add your own new stages for additional functionality or generating additonal indexes in the DB that your app needs. You can add your own stages for, say, generating some analytics (that is what we are doing here). You can also change the existing stages to, say, change hexary Merkle tries to Binary ones. But this is out of scope for this project.

In this example, we will generate and update a CSV with cumulative number of gas burned and average gas price. It might not be very practical but it makes a good showcase.

First we need to create a factory method for our custom stage.

```go
stageID := stages.SyncStage("org.ffconsulting.AVG_GAS_PRICE")

...

stagedsync.StageBuilder{
    ID: stageID, // id of the stag
    Build: func(world stagedsync.StageParameters) *stagedsync.Stage { ... },
}
```

As you see, you need to provide the `stageID` there. You can choose the name at will as long as it is not empty and doesn't clash with existing names.
I recommend you to prefix the names with something unique like `org.ffconsulting`).

Then the second parameter is actually our factory functin that receives the
current state of the world (`stagedsync.StageParameters`) and returns the stage.
This state provides stuff like current DB transaction, and other information
that is useful for the stage.

The builder function might look like that.

```go
func(world stagedsync.StageParameters) *stagedsync.Stage {
    return &stagedsync.Stage{
        ID:          stageID,
        Description: "Plot Minted Coins",
        ExecFunc: func(s *stagedsync.StageState, _ stagedsync.Unwinder) error {
            ...
            s.Done()
        },

        UnwindFunc: func(u *stagedsync.UnwindState, s *stagedsync.StageState) error {
            ...
            return u.Done(world.TX)
        },
    }
}
```

It is a bit of a mouthful. So as ID you have to provide the same `stageID` as
before. 

**Description** is just a test that will be shown in the logs when this stage
is running.

**ExecFunc** is the function that will be executed when the stage is called. It
should always end with `s.Done()` or `s.DoneAndUpdate` functions, so it will
pass the functinality to the next stage (or the next staged sync cycle if it is
the last one).

**UnwindFunc** is a function that is called when the stage needs to be unwound.
It receives **from** and **to** blocks in `stagedsync.UnwindState`. And it must
end with `u.Done` with the current transaction, marking the unwind successful.

For this example we don't do anything for unwinding, wo the function consists
only of `u.Done(world.TX)`.

Let's look closer at our `ExecFunc`.

```
ExecFunc: func(s *stagedsync.StageState, _ stagedsync.Unwinder) error {
    fileName := ctx.String(outputFileNameFlag.Name)
    if fileName == "" {
        fileName = "mint.csv"
    }

    blockNumber := ctx.Uint64(blockNumberFlag.Name)

    err := mint(world.TX, fileName, blockNumber)
    if err != nil {
        return err
    }

    s.Done()
    return nil
},
```

So here we read ouf custom parameters and then call a function defined in
[`mint.go`](./mint.go) with it.

`world.TX` is the current transaction that staged sync runs. Between cycles
staged sync tries to run everything in a single transaction so the information
in the database stays consistent. There we receive the most up to date version
of it. We can both read and write data to buckets, using `world.TX`.


## Conclusion

You can, with relatively little amount of code, create some specialized nodes
using turbo-api.

