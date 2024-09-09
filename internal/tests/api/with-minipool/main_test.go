package with_minipool

import (
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	cstestutils "github.com/nodeset-org/hyperdrive-constellation/internal/tests/utils"
	cstesting "github.com/nodeset-org/hyperdrive-constellation/testing"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/node-manager-core/log"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
)

// Various singleton variables used for testing
var (
	harness      *cstestutils.StandardTestHarness
	standardSalt *big.Int = big.NewInt(0x90de5e7)
	mp           minipool.IMinipool
)

// Initialize a common server used by all tests
func TestMain(m *testing.M) {
	_harness, err := cstestutils.CreateStandardTestHarness(nil)
	if err != nil {
		fail("error creating standard test harness: %v", err)
	}
	harness = _harness

	// Create a minipool
	mp = createMinipool(standardSalt, harness.MainNode, harness.MainNodeAddress)
	stakeMinipool(harness.MainNode, harness.MainNodeAddress, mp)

	// Run tests
	code := m.Run()

	// Clean up and exit
	cleanup()
	os.Exit(code)
}

// Fail with an error message
func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
	cleanup()
	os.Exit(1)
}

// Clean up the test harness
func cleanup() {
	if harness == nil {
		return
	}
	err := harness.Close()
	if err != nil {
		harness.Logger.Error("Error closing harness", log.Err(err))
	}
	harness = nil
}

// Makes a minipool
func createMinipool(salt *big.Int, node *cstesting.ConstellationNode, nodeAddress common.Address) minipool.IMinipool {
	// Get some services
	sp := harness.MainNode.GetServiceProvider()
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()
	bindings := harness.Bindings
	logger := harness.Logger

	// Query some details
	var rplPrice *big.Int
	var minipoolBond *big.Int
	err := qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.PriceFetcher.GetRplPrice(mc, &rplPrice)
		csMgr.SuperNodeAccount.Bond(mc, &minipoolBond)
		return nil
	}, nil,
		bindings.RpSuperNode.Exists,
		bindings.RpSuperNode.RplStake,
		bindings.RpSuperNode.EthMatched,
		bindings.MinipoolManager.LaunchBalance,
		bindings.DepositPoolManager.Balance,
		bindings.ProtocolDaoManager.Settings.Deposit.MaximumDepositPoolSize,
		bindings.OracleDaoManager.Settings.Minipool.ScrubPeriod,
	)
	if err != nil {
		fail("error querying chain details during minipool creation: %v", err)
	}

	// Verify some details
	if !bindings.RpSuperNode.Exists.Get() {
		fail("supernode account is not registered with RP")
	}
	if bindings.RpSuperNode.RplStake.Get().Cmp(common.Big0) != 0 {
		fail("supernode account has RPL staked")
	}
	if bindings.DepositPoolManager.Balance.Get().Cmp(common.Big0) != 0 {
		fail("deposit pool balance is not zero")
	}
	if rplPrice.Cmp(common.Big0) <= 0 {
		fail("RPL price is zero or negative")
	}
	logger.Info(fmt.Sprintf("RPL price is %.6f RPL/ETH (%s wei)", eth.WeiToEth(rplPrice), rplPrice.String()))

	// Send ETH to the RP deposit pool
	deployerOpts := harness.DeployerOpts
	fundOpts := &bind.TransactOpts{
		From:  deployerOpts.From,
		Value: bindings.ProtocolDaoManager.Settings.Deposit.MaximumDepositPoolSize.Get(), // Deposit the maximum amount
	}
	txInfo, err := bindings.DepositPoolManager.Deposit(fundOpts)
	if err != nil {
		fail("error depositing ETH to the RP deposit pool: %v", err)
	}
	err = harness.TestManager.MineTxBeforeTest(txInfo, deployerOpts)
	if err != nil {
		fail("error funding the RP deposit pool: %v", err)
	}
	logger.Info("Funded the RP deposit pool")

	// Get the deposit amounts
	wethAmount, rplAmount := getDepositAmounts(1)

	// Deposit WETH to the WETH vault
	err = cstestutils.DepositToWethVaultBeforeTest(harness, csMgr.WethVault, bindings.Weth, wethAmount, deployerOpts)
	if err != nil {
		fail("error depositing WETH to the WETH vault: %v", err)
	}

	// Deposit RPL to the RPL vault
	err = cstestutils.DepositToRplVaultBeforeTest(harness, csMgr.RplVault, bindings.Rpl, rplAmount, deployerOpts)
	if err != nil {
		fail("error depositing RPL to the RPL vault: %v", err)
	}

	// Register with Constellation
	cstestutils.RegisterWithConstellationBeforeTest(harness, node)

	// Deposit to make a minipool
	mp, err := cstestutils.CreateMinipoolBeforeTest(harness, node, nodeAddress, salt, bindings.RpSuperNode, bindings.MinipoolManager)
	if err != nil {
		fail("error creating minipool: %v", err)
	}
	return mp
}

// Makes a minipool, waits for the scrub check, then stakes it
func stakeMinipool(node *cstesting.ConstellationNode, nodeAddress common.Address, mp minipool.IMinipool) error {
	// Get the scrub period
	testMgr := harness.TestManager
	logger := harness.Logger
	bindings := harness.Bindings
	sp := node.GetServiceProvider()
	qMgr := sp.GetQueryManager()
	err := qMgr.Query(nil, nil,
		bindings.OracleDaoManager.Settings.Minipool.ScrubPeriod,
	)
	if err != nil {
		return fmt.Errorf("error querying scrub period: %w", err)
	}

	// Fast forward time
	timeToAdvance := bindings.OracleDaoManager.Settings.Minipool.ScrubPeriod.Formatted()
	secondsPerSlot := time.Duration(testMgr.GetBeaconMockManager().GetConfig().SecondsPerSlot) * time.Second
	slotsToAdvance := uint(timeToAdvance / secondsPerSlot)
	err = testMgr.AdvanceSlots(slotsToAdvance, false)
	if err != nil {
		return fmt.Errorf("error advancing slots: %w", err)
	}
	err = testMgr.CommitBlock()
	if err != nil {
		return fmt.Errorf("error committing block: %w", err)
	}
	logger.Info(fmt.Sprintf("Advanced %d slots", slotsToAdvance))

	// Stake the minipool
	err = cstestutils.StakeMinipoolBeforeTest(harness, node, nodeAddress, mp)
	if err != nil {
		return fmt.Errorf("error staking minipool: %w", err)
	}
	return nil
}

// Get the amount of ETH and RPL to deposit into the WETH and RPL vaults respectively in order to launch the given number of minipools
func getDepositAmounts(minipoolCount int) (*big.Int, *big.Int) {
	// Get some services
	sp := harness.MainNode.GetServiceProvider()
	bindings := harness.Bindings
	logger := harness.Logger
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()
	countBig := big.NewInt(int64(minipoolCount))

	// Query some details
	var rplPerEth *big.Int
	var minipoolBond *big.Int
	var ethReserveRatio *big.Int
	var rplReserveRatio *big.Int
	var mintFee *big.Int
	err := qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.PriceFetcher.GetRplPrice(mc, &rplPerEth)
		csMgr.SuperNodeAccount.Bond(mc, &minipoolBond)
		csMgr.WethVault.GetLiquidityReservePercent(mc, &ethReserveRatio)
		csMgr.RplVault.GetLiquidityReservePercent(mc, &rplReserveRatio)
		csMgr.WethVault.GetMintFee(mc, &mintFee)
		return nil
	}, nil,
		bindings.RpSuperNode.RplStake,
		bindings.RpSuperNode.EthMatched,
		bindings.MinipoolManager.LaunchBalance,
	)
	if err != nil {
		fail("error querying chain details during deposit calculation: %v", err)
	}

	// Get the total ETH bond and borrow amounts
	launchRequirement := bindings.MinipoolManager.LaunchBalance.Get()
	totalEthBond := new(big.Int).Mul(minipoolBond, countBig)
	totalEthBorrow := new(big.Int).Sub(launchRequirement, minipoolBond)
	totalEthBorrow.Mul(totalEthBorrow, countBig)
	logger.Info(fmt.Sprintf("Calculating RPL shortfall for %d minipools with %.2f ETH bond and %.2f ETH borrow", minipoolCount, eth.WeiToEth(totalEthBond), eth.WeiToEth(totalEthBorrow)))

	// Get the RPL requirement
	var rplShortfall *big.Int
	totalEthMatched := bindings.RpSuperNode.EthMatched.Get()
	ethAmount := new(big.Int).Add(totalEthMatched, totalEthBorrow)
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.OperatorDistributor.CalculateRplStakeShortfall(mc, &rplShortfall, bindings.RpSuperNode.RplStake.Get(), ethAmount)
		return nil
	}, nil)
	if err != nil {
		fail("error calculating RPL shortfall: %v", err)
	}
	logger.Info(fmt.Sprintf("RPL shortfall is %.6f RPL (%s wei)", eth.WeiToEth(rplShortfall), rplShortfall.String()))

	// Fix the ETH amount based on the liquidity reserve
	oneEth := big.NewInt(1e18)
	ethCollateral := new(big.Int).Sub(oneEth, ethReserveRatio)
	mintFeeFactor := new(big.Int).Sub(oneEth, mintFee)
	ethDepositRequirement := new(big.Int).Mul(totalEthBond, oneEth)
	ethDepositRequirement.Mul(ethDepositRequirement, oneEth)
	ethDepositRequirement.Div(ethDepositRequirement, ethCollateral)
	ethDepositRequirement.Div(ethDepositRequirement, mintFeeFactor)
	ethDepositRequirement.Add(ethDepositRequirement, common.Big1)

	// Fix the RPL amount based on the liquidity reserve
	rplCollateral := new(big.Int).Sub(oneEth, rplReserveRatio)
	rplDepositRequirement := new(big.Int).Mul(rplShortfall, oneEth)
	rplDepositRequirement.Div(rplDepositRequirement, rplCollateral)
	rplDepositRequirement.Add(rplDepositRequirement, common.Big1)

	logger.Info(fmt.Sprintf("Total deposit requirements are %.2f ETH (%s wei) and %.6f RPL (%s wei)", eth.WeiToEth(ethDepositRequirement), ethDepositRequirement.String(), eth.WeiToEth(rplDepositRequirement), rplDepositRequirement.String()))
	return ethDepositRequirement, rplDepositRequirement
}
