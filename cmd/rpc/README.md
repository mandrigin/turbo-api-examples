## RPC Daemon

This is a daemon based on [RPC Daemon Of TurboGeth](https://github.com/ledgerwatch/turbo-geth/tree/master/cmd/rpcdaemon), that adds additional API for ETH supply.

### API

The daemon supports [everything that the original daemon supports](https://github.com/ledgerwatch/turbo-geth/tree/master/cmd/rpcdaemon#rpc-implementation-status).

It also adds API for eth supply. They area all in the `tg` namespace.

#### `tg_getSupply`

Returns the supply for the specified block.

**Parameters**

The RPC command only receives one parameter.

1. block number or "latest" (0, 10000, or 'latest' are example of valid values)

**Examples**

For the block 10.000
```json
{
	"jsonrpc": "2.0",
	"id": 1,
	"method": "tg_getSupply",
	"params": [10000]
}
```

For the latest block
```json
{
	"jsonrpc": "2.0",
	"id": 1,
	"method": "tg_getSupply",
	"params": ["latest"]
}

```

For genesis
```json
{
	"jsonrpc": "2.0",
	"id": 1,
	"method": "tg_getSupply",
	"params": [0]
}
```
