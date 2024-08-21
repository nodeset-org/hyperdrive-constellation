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
	operatorDistributorAbiString string = `[{"inputs":[],"stateMutability":"nonpayable","type":"constructor"},{"inputs":[{"internalType":"uint256","name":"expectedBondAmount","type":"uint256"},{"internalType":"uint256","name":"actualBondAmount","type":"uint256"}],"name":"BadBondAmount","type":"error"},{"inputs":[{"internalType":"address","name":"expected","type":"address"},{"internalType":"address","name":"actual","type":"address"}],"name":"BadPredictedCreation","type":"error"},{"inputs":[{"internalType":"bytes32","name":"role","type":"bytes32"},{"internalType":"address","name":"user","type":"address"}],"name":"BadRole","type":"error"},{"inputs":[{"internalType":"address","name":"expectedSender","type":"address"}],"name":"BadSender","type":"error"},{"inputs":[{"internalType":"uint256","name":"expectedBalance","type":"uint256"},{"internalType":"uint256","name":"actualBalance","type":"uint256"}],"name":"InsufficientBalance","type":"error"},{"inputs":[{"internalType":"bool","name":"success","type":"bool"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"LowLevelCall","type":"error"},{"inputs":[{"internalType":"bool","name":"success","type":"bool"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"LowLevelEthTransfer","type":"error"},{"inputs":[{"internalType":"address","name":"addr","type":"address"}],"name":"NotAContract","type":"error"},{"inputs":[],"name":"ZeroAddressError","type":"error"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"previousAdmin","type":"address"},{"indexed":false,"internalType":"address","name":"newAdmin","type":"address"}],"name":"AdminChanged","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"beacon","type":"address"}],"name":"BeaconUpgraded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint8","name":"version","type":"uint8"}],"name":"Initialized","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"minipool","type":"address"}],"name":"SuspectedPenalizedMinipoolExit","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"implementation","type":"address"}],"name":"Upgraded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"_minipool","type":"address"}],"name":"WarningEthBalanceSmallerThanRefundBalance","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"_minipoolAddress","type":"address"},{"indexed":true,"internalType":"enum MinipoolStatus","name":"_status","type":"uint8"},{"indexed":true,"internalType":"bool","name":"_isFinalized","type":"bool"}],"name":"WarningMinipoolNotStaking","type":"event"},{"anonymous":false,"inputs":[],"name":"WarningNoMiniPoolsToHarvest","type":"event"},{"inputs":[{"internalType":"uint256","name":"_existingRplStake","type":"uint256"},{"internalType":"uint256","name":"_ethStaked","type":"uint256"}],"name":"calculateRequiredRplTopDown","outputs":[{"internalType":"uint256","name":"withdrawableStakeRpl","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"_existingRplStake","type":"uint256"},{"internalType":"uint256","name":"_rpEthMatched","type":"uint256"}],"name":"calculateRplStakeShortfall","outputs":[{"internalType":"uint256","name":"requiredStakeRpl","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"contract IMinipool","name":"minipool","type":"address"}],"name":"distributeExitedMinipool","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"getDirectory","outputs":[{"internalType":"contract Directory","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getImplementation","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getNextMinipool","outputs":[{"internalType":"contract IMinipool","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getNextMinipoolIndex","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getTvlEth","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getTvlRpl","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"_directory","type":"address"}],"name":"initialize","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"lastProcessedMinipoolIndex","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"minimumStakeRatio","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"rewardAmount","type":"uint256"},{"internalType":"uint256","name":"avgTreasuryFee","type":"uint256"},{"internalType":"uint256","name":"avgOperatorsFee","type":"uint256"}],"name":"onEthBeaconRewardsReceived","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"_amount","type":"uint256"}],"name":"onIncreaseOracleError","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"oracleError","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"contract IMinipool","name":"minipool","type":"address"}],"name":"processMinipool","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"processNextMinipool","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"_bond","type":"uint256"}],"name":"provisionLiquiditiesForMinipoolCreation","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"proxiableUUID","outputs":[{"internalType":"bytes32","name":"","type":"bytes32"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"_ethStaked","type":"uint256"}],"name":"rebalanceRplStake","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"rebalanceRplVault","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"rebalanceWethVault","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"resetOracleError","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"_minimumStakeRatio","type":"uint256"}],"name":"setMinimumStakeRatio","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"_targetStakeRatio","type":"uint256"}],"name":"setTargetStakeRatio","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"_amount","type":"uint256"}],"name":"stakeRpl","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256[]","name":"rewardIndex","type":"uint256[]"},{"internalType":"uint256[]","name":"amountRPL","type":"uint256[]"},{"internalType":"uint256[]","name":"amountETH","type":"uint256[]"},{"internalType":"bytes32[][]","name":"merkleProof","type":"bytes32[][]"}],"name":"submitMerkleClaim","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"targetStakeRatio","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"ethAmount","type":"uint256"},{"internalType":"uint256","name":"rplAmount","type":"uint256"}],"name":"transferMerkleClaimToStreamer","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"_amount","type":"uint256"}],"name":"unstakeRpl","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newImplementation","type":"address"}],"name":"upgradeTo","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newImplementation","type":"address"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"upgradeToAndCall","outputs":[],"stateMutability":"payable","type":"function"},{"stateMutability":"payable","type":"receive"}]`
)

// ABI cache
var operatorDistributorAbi abi.ABI
var operatorDistributorOnce sync.Once

type OperatorDistributor struct {
	Address  common.Address
	contract *eth.Contract
	txMgr    *eth.TransactionManager
}

// Create a new OperatorDistributor instance
func NewOperatorDistributor(address common.Address, ec eth.IExecutionClient, txMgr *eth.TransactionManager) (*OperatorDistributor, error) {
	// Parse the ABI
	var err error
	operatorDistributorOnce.Do(func() {
		var parsedAbi abi.ABI
		parsedAbi, err = abi.JSON(strings.NewReader(operatorDistributorAbiString))
		if err == nil {
			operatorDistributorAbi = parsedAbi
		}
	})
	if err != nil {
		return nil, fmt.Errorf("error parsing OperatorDistributor ABI: %w", err)
	}

	// Create the contract
	contract := &eth.Contract{
		ContractImpl: bind.NewBoundContract(address, operatorDistributorAbi, ec, ec, ec),
		Address:      address,
		ABI:          &operatorDistributorAbi,
	}

	return &OperatorDistributor{
		Address:  address,
		contract: contract,
		txMgr:    txMgr,
	}, nil
}

// =============
// === Calls ===
// =============

// Calculates the additional amount of RPL required to be staked in order to be able to stake the given amount of ETH, based on Constellation's targetStakeRatio
func (c *OperatorDistributor) CalculateRplStakeShortfall(mc *batch.MultiCaller, out **big.Int, existingRplStake *big.Int, ethStaked *big.Int) {
	eth.AddCallToMulticaller(mc, c.contract, out, "calculateRplStakeShortfall", existingRplStake, ethStaked)
}

func (c *OperatorDistributor) GetNextMinipoolDebug() (common.Address, error) {
	res := []any{}
	err := c.contract.ContractImpl.Call(nil, &res, "getNextMinipool")
	if err != nil {
		return common.Address{}, err
	}
	return res[0].(common.Address), nil
}

func (c *OperatorDistributor) GetNextMinipool(mc *batch.MultiCaller, out *common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getNextMinipool")
}

func (c *OperatorDistributor) GetOracleError(mc *batch.MultiCaller, out **big.Int) {
	eth.AddCallToMulticaller(mc, c.contract, out, "oracleError")
}

func (c *OperatorDistributor) GetCurrentMinipoolIndex(mc *batch.MultiCaller, out **big.Int) {
	eth.AddCallToMulticaller(mc, c.contract, out, "currentMinipool")
}

// ====================
// === Transactions ===
// ====================

// TODO: description
func (c *OperatorDistributor) ProcessMinipool(address common.Address, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "processMinipool", opts, address)
}

// TODO: description
func (c *OperatorDistributor) ProcessNextMinipool(opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "processNextMinipool", opts)
}

// Adjusts the Supernode's RPL stake to make sure it's in line with the target stake ratio
func (c *OperatorDistributor) RebalanceRplStake(ethStaked *big.Int, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "rebalanceRplStake", opts, ethStaked)
}

// Allocates the necessary liquidity for the creation of a new minipool.
func (c *OperatorDistributor) ProvisionLiquiditiesForMinipoolCreation(newMinipoolBond *big.Int, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "provisionLiquiditiesForMinipoolCreation", opts, newMinipoolBond)
}

// Sets Constellation's target stake ratio
func (c *OperatorDistributor) SetTargetStakeRatio(ratio *big.Int, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "setTargetStakeRatio", opts, ratio)
}
