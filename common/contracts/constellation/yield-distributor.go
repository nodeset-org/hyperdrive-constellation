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
	yieldDistributorAbiString string = `[{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"previousAdmin","type":"address"},{"indexed":false,"internalType":"address","name":"newAdmin","type":"address"}],"name":"AdminChanged","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"beacon","type":"address"}],"name":"BeaconUpgraded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint8","name":"version","type":"uint8"}],"name":"Initialized","type":"event"},{"anonymous":false,"inputs":[{"components":[{"internalType":"address","name":"recipient","type":"address"},{"internalType":"uint256","name":"eth","type":"uint256"}],"indexed":false,"internalType":"struct Reward","name":"","type":"tuple"}],"name":"RewardDistributed","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"implementation","type":"address"}],"name":"Upgraded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"operator","type":"address"},{"indexed":false,"internalType":"uint256","name":"interval","type":"uint256"}],"name":"WarningAlreadyClaimed","type":"event"},{"inputs":[],"name":"currentInterval","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"currentIntervalGenesisTime","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"dustAccrued","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"finalizeInterval","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"getDirectory","outputs":[{"internalType":"contract Directory","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getImplementation","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getIntervals","outputs":[{"components":[{"internalType":"uint256","name":"amount","type":"uint256"},{"internalType":"uint256","name":"numOperators","type":"uint256"},{"internalType":"uint256","name":"maxValidators","type":"uint256"}],"internalType":"struct Interval[]","name":"","type":"tuple[]"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getIsEndOfIntervalTime","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"_rewardee","type":"address"},{"internalType":"uint256","name":"_startInterval","type":"uint256"},{"internalType":"uint256","name":"_endInterval","type":"uint256"}],"name":"harvest","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"","type":"address"},{"internalType":"uint256","name":"","type":"uint256"}],"name":"hasClaimed","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"_directory","type":"address"}],"name":"initialize","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"","type":"uint256"}],"name":"intervals","outputs":[{"internalType":"uint256","name":"amount","type":"uint256"},{"internalType":"uint256","name":"numOperators","type":"uint256"},{"internalType":"uint256","name":"maxValidators","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"k","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"maxIntervalLengthSeconds","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"proxiableUUID","outputs":[{"internalType":"bytes32","name":"","type":"bytes32"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"_maxIntervalLengthSeconds","type":"uint256"}],"name":"setMaxIntervalTime","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"_k","type":"uint256"}],"name":"setRewardIncentiveModel","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"treasurySweep","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newImplementation","type":"address"}],"name":"upgradeTo","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newImplementation","type":"address"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"upgradeToAndCall","outputs":[],"stateMutability":"payable","type":"function"},{"inputs":[{"internalType":"uint256","name":"weth","type":"uint256"}],"name":"wethReceived","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"weth","type":"uint256"}],"name":"wethReceivedVoidClaim","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"yieldAccruedInInterval","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"stateMutability":"payable","type":"receive"}]`
)

// ABI cache
var yieldDistributorAbi abi.ABI
var yieldDistributorOnce sync.Once

type Interval struct {
	Amount        *big.Int `abi:"amount"`
	NumOperators  *big.Int `abi:"numOperators"`
	MaxValidators *big.Int `abi:"maxValidators"`
}

type YieldDistributor struct {
	Address  common.Address
	contract *eth.Contract
	txMgr    *eth.TransactionManager
}

// Create a new YieldDistributor instance
func NewYieldDistributor(address common.Address, ec eth.IExecutionClient, txMgr *eth.TransactionManager) (*YieldDistributor, error) {
	// Parse the ABI
	var err error
	yieldDistributorOnce.Do(func() {
		var parsedAbi abi.ABI
		parsedAbi, err = abi.JSON(strings.NewReader(yieldDistributorAbiString))
		if err == nil {
			yieldDistributorAbi = parsedAbi
		}
	})
	if err != nil {
		return nil, fmt.Errorf("error parsing YieldDistributor ABI: %w", err)
	}

	// Create the contract
	contract := &eth.Contract{
		ContractImpl: bind.NewBoundContract(address, yieldDistributorAbi, ec, ec, ec),
		Address:      address,
		ABI:          &yieldDistributorAbi,
	}

	return &YieldDistributor{
		Address:  address,
		contract: contract,
		txMgr:    txMgr,
	}, nil
}

// =============
// === Calls ===
// =============

func (c *YieldDistributor) GetCurrentInterval(mc *batch.MultiCaller, out **big.Int) {
	eth.AddCallToMulticaller(mc, c.contract, out, "currentInterval")
}

func (c *YieldDistributor) GetIntervalByIndex(mc *batch.MultiCaller, out *Interval, index *big.Int) {
	eth.AddCallToMulticaller(mc, c.contract, out, "intervals", index)
}

// ====================
// === Transactions ===
// ====================

func (c *YieldDistributor) Harvest(_rewardee common.Address, _startInterval *big.Int, _endInterval *big.Int, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "harvest", opts, _rewardee, _startInterval, _endInterval)
}

func (c *YieldDistributor) FinalizeInterval(opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "finalizeInterval", opts)
}
