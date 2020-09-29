`cmd/supply`
---

This is the app that calculates ETH supply for each block and stores it into the db bucket.

Later you can use [`cmd/rpc`](../rpc) to query the database or the daemon itself.

It has the supply calculation as a sync stage, so while you run this app not
only does your node get synced, but supply numbers for the new blocks are
calculated too.

## Usage

```
> go run ./cmd/supply --datadir <path-to-your-tg-datadir> --private.api.addr=localhost:8787
```

The you wait for the sync to complete (it will include ETH supply calculation).

On my machine ETH supply stage takes about 4 hours.

You should see this log message.

```
INFO [09-29|09:42:09.093] ETH supply calculation... DONE. use `tg_getSupply` to get values
```

### Requesting the supply via RPC

If you still have the `./cmd/supply` node running, you can run the [RPC daemon](../rpc) too.

```
> go run ./cmd/rpc --private.api.addr=localhost:8787 --http.api=eth,debug,net,tg
```

Make sure that `--private.api.addr` value matches for both `cmd/supply` and `cmd/rpc`.

Note that `tg` namespace is added to `http.api` parameter!

Then you can use `curl` to request supply for any block.


For genesis:
```
curl --request POST \
  --url http://localhost:8545/ \
  --header 'content-type: application/json' \
  --data '{
	"jsonrpc": "2.0",
	"id": 88888,
	"method": "tg_getSupply",
	"params": [0]
}'
```

For the latest block:
```
curl --request POST \
  --url http://localhost:8545/ \
  --header 'content-type: application/json' \
  --data '{
	"jsonrpc": "2.0",
	"id": 88888,
	"method": "tg_getSupply",
	"params": ["latest"]
}'
```

For any block number:
```
curl --request POST \
  --url http://localhost:8545/ \
  --header 'content-type: application/json' \
  --data '{
	"jsonrpc": "2.0",
	"id": 88888,
	"method": "tg_getSupply",
	"params": [10000]
}'
```

You will receive a response like that

```
{
  "jsonrpc": "2.0",
  "id": 88888,
  "result": {
    "block_number": 0,
    "supply": "72009990499480000000000000"
  }
}
```
