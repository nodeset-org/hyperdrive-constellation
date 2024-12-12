# hyperdrive-constellation
Constellation module for Hyperdrive


## setting up unit test environment locally

Hardhat:
(from osha/hardhat)

```
./start.sh
```

RP / Constellation contracts:
(from constellation)

```
npx hardhat run ./scripts/sandbox.ts --network localhost
```

MC / BB:
(from rocketpool-go/tests/hardhat)

```
npx hardhat run scripts/deploy.js --network localhost
```