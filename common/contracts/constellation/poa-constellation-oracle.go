package constellation

import (
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/eth"
)

const (
	poaConstellationOracleAbiString string = `[{"inputs":[],"stateMutability":"nonpayable","type":"constructor"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"previousAdmin","type":"address"},{"indexed":false,"internalType":"address","name":"newAdmin","type":"address"}],"name":"AdminChanged","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"beacon","type":"address"}],"name":"BeaconUpgraded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint8","name":"version","type":"uint8"}],"name":"Initialized","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"int256","name":"_amount","type":"int256"}],"name":"TotalYieldAccruedUpdated","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"implementation","type":"address"}],"name":"Upgraded","type":"event"},{"inputs":[],"name":"getDirectory","outputs":[{"internalType":"contract Directory","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getImplementation","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getLastUpdatedTotalYieldAccrued","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getTotalYieldAccrued","outputs":[{"internalType":"int256","name":"","type":"int256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"directoryAddress","type":"address"}],"name":"initialize","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"_directoryAddress","type":"address"}],"name":"initializeOracle","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"proxiableUUID","outputs":[{"internalType":"bytes32","name":"","type":"bytes32"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"bytes","name":"_sig","type":"bytes"},{"components":[{"internalType":"int256","name":"newTotalYieldAccrued","type":"int256"},{"internalType":"uint256","name":"expectedOracleError","type":"uint256"},{"internalType":"uint256","name":"timeStamp","type":"uint256"}],"internalType":"struct PoAConstellationOracle.PoAOracleSignatureData","name":"sigData","type":"tuple"}],"name":"setTotalYieldAccrued","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newImplementation","type":"address"}],"name":"upgradeTo","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newImplementation","type":"address"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"upgradeToAndCall","outputs":[],"stateMutability":"payable","type":"function"}]`
)

// ABI cache
var poaConstellationOracleAbi abi.ABI
var poaConstellationOracleOnce sync.Once

type PoAOracleSignatureData struct {
	NewTotalYieldAccrued *big.Int `abi:"newTotalYieldAccrued"`
	ExpectedOracleError  *big.Int `abi:"expectedOracleError"`
	Timestamp            *big.Int `abi:"timeStamp"`
}

type PoAConstellationOracle struct {
	Address  common.Address
	contract *eth.Contract
	txMgr    *eth.TransactionManager
}

// Create a new PoAConstellationOracle instance
func NewPoAConstellationOracle(address common.Address, ec eth.IExecutionClient, txMgr *eth.TransactionManager) (*PoAConstellationOracle, error) {
	// Parse the ABI
	var err error
	poaConstellationOracleOnce.Do(func() {
		var parsedAbi abi.ABI
		parsedAbi, err = abi.JSON(strings.NewReader(poaConstellationOracleAbiString))
		if err == nil {
			poaConstellationOracleAbi = parsedAbi
		}
	})
	if err != nil {
		return nil, fmt.Errorf("error parsing PoAConstellationOracle ABI: %w", err)
	}

	// Create the contract
	contract := &eth.Contract{
		ContractImpl: bind.NewBoundContract(address, poaConstellationOracleAbi, ec, ec, ec),
		Address:      address,
		ABI:          &poaConstellationOracleAbi,
	}

	return &PoAConstellationOracle{
		Address:  address,
		contract: contract,
		txMgr:    txMgr,
	}, nil
}

// =============
// === Calls ===
// =============

// Gets the total yield Constellation has accrued
func (c *PoAConstellationOracle) GetTotalYieldAccrued(mc *batch.MultiCaller, out **big.Int) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getTotalYieldAccrued")
}

// ====================
// === Transactions ===
// ====================

// Sets the total yield Constellation has accrued as reported by the xrETH Oracle
func (c *PoAConstellationOracle) SetTotalYieldAccrued(newTotalYieldAccrued *big.Int, currentOracleError *big.Int, signature []byte, signatureTimestamp time.Time, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	timestamp := signatureTimestamp.UTC().Unix()
	timestampBig := big.NewInt(timestamp)

	data := PoAOracleSignatureData{
		NewTotalYieldAccrued: newTotalYieldAccrued,
		ExpectedOracleError:  currentOracleError,
		Timestamp:            timestampBig,
	}
	return c.txMgr.CreateTransactionInfo(c.contract, "setTotalYieldAccrued", opts, signature, data)
}
