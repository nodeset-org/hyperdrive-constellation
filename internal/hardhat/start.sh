#!/bin/sh
npx hardhat run scripts/deploy.js --network localhost

# Take a vanilla snapshot, revert here in case of a panic that puts HH in a weird state
SNAPSHOT=$(curl -s -X POST -H "Content-Type: application/json" --data '{"jsonrpc":"2.0","method":"evm_snapshot","params":[],"id":1}' http://localhost:8545 | jq '.result')
echo "Emergency snapshot name = $SNAPSHOT"
