package constellation

import (
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/eth"
)

const (
	priceFetcherAbiString string = `[{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"previousAdmin","type":"address"},{"indexed":false,"internalType":"address","name":"newAdmin","type":"address"}],"name":"AdminChanged","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"beacon","type":"address"}],"name":"BeaconUpgraded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint8","name":"version","type":"uint8"}],"name":"Initialized","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"implementation","type":"address"}],"name":"Upgraded","type":"event"},{"inputs":[],"name":"getDirectory","outputs":[{"internalType":"contract Directory","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getImplementation","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getPrice","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"_directory","type":"address"}],"name":"initialize","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"proxiableUUID","outputs":[{"internalType":"bytes32","name":"","type":"bytes32"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"newImplementation","type":"address"}],"name":"upgradeTo","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newImplementation","type":"address"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"upgradeToAndCall","outputs":[],"stateMutability":"payable","type":"function"}]`
)

// ABI cache
var priceFetcherAbi abi.ABI
var priceFetcherOnce sync.Once

type PriceFetcher struct {
	Address  common.Address
	contract *eth.Contract
	txMgr    *eth.TransactionManager
}

// Create a new PriceFetcher instance
func NewPriceFetcher(address common.Address, ec eth.IExecutionClient, txMgr *eth.TransactionManager) (*PriceFetcher, error) {
	// Parse the ABI
	var err error
	priceFetcherOnce.Do(func() {
		var parsedAbi abi.ABI
		parsedAbi, err = abi.JSON(strings.NewReader(priceFetcherAbiString))
		if err == nil {
			priceFetcherAbi = parsedAbi
		}
	})
	if err != nil {
		return nil, fmt.Errorf("error parsing PriceFetcher ABI: %w", err)
	}

	// Create the contract
	contract := &eth.Contract{
		ContractImpl: bind.NewBoundContract(address, priceFetcherAbi, ec, ec, ec),
		Address:      address,
		ABI:          &priceFetcherAbi,
	}

	return &PriceFetcher{
		Address:  address,
		contract: contract,
		txMgr:    txMgr,
	}, nil
}

// =============
// === Calls ===
// =============

// Gets the price of RPL as a ratio of RPL:ETH
func (c *PriceFetcher) GetRplPrice(mc *batch.MultiCaller, out **big.Int) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getPrice")
}

// ====================
// === Transactions ===
// ====================
