#!/bin/sh
npx hardhat run scripts/deploy.js --network localhost

# Take a vanilla snapshot, revert here in case of a panic that puts HH in a weird state
SNAPSHOT=$(curl -s -X POST -H "Content-Type: application/json" --data '{"jsonrpc":"2.0","method":"evm_snapshot","params":[],"id":1}' http://localhost:8545 | jq '.result')
echo "Emergency snapshot name = $SNAPSHOT"

# Revert + resnapshot example:
# curl -X POST -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":"id","method":"evm_revert","params":["0x146"]}' http://localhost:8545
# curl -X POST -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":"id","method":"evm_snapshot"}' http://localhost:8545
