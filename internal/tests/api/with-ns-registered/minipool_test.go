package with_ns_registered

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	cstestutils "github.com/nodeset-org/hyperdrive-constellation/internal/tests/utils"
	hdtesting "github.com/nodeset-org/hyperdrive-daemon/testing"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/stretchr/testify/require"
)

var (
	salt *big.Int = big.NewInt(0x90de5e7)
)

// Test getting the available minipool count when there are no minipools available
func TestMinipoolGetAvailableMinipoolCount_Zero(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

	// Check the available minipool count
	cs := testMgr.GetApiClient()
	countResponse, err := cs.Minipool.GetAvailableMinipoolCount()
	require.NoError(t, err)
	require.Equal(t, 0, countResponse.Data.Count)
}

// Test getting the available minipool count when there is one minipool available
func TestMinipoolGetAvailableMinipoolCount_One(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

	// Set up the NodeSet mock server
	expectedMinipoolCount := 1
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	nsMgr.SetAvailableConstellationMinipoolCount(nsEmail, expectedMinipoolCount)

	// Check the available minipool count
	cs := testMgr.GetApiClient()
	countResponse, err := cs.Minipool.GetAvailableMinipoolCount()
	require.NoError(t, err)
	require.Equal(t, expectedMinipoolCount, countResponse.Data.Count)
}

// Run a full cycle test of provisioning RP and Constellation, then depositing and staking a minipool
func TestMinipoolDepositAndStake_SimpleRoundTrip(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

	bindings, err := cstestutils.CreateBindings(testMgr.GetConstellationServiceProvider())
	require.NoError(t, err)
	t.Log("Created contract bindings")

	depositAndStakeMinipool(t, bindings)
	simulateEthRewardToYieldDistributor(t, bindings)
}

// Disable RPL coverage limitation, deposit 1000 xrETH/ETH, set RPL coverage limitation to 30%
func TestMinipoolDepositAndStake_BigRplBootstrap_MintGreaterThanCoverageRatio(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

	// Get some services
	sp := testMgr.GetConstellationServiceProvider()
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()

	// Disable RPL coverage limitation
	bindings, err := cstestutils.CreateBindings(testMgr.GetConstellationServiceProvider())
	require.NoError(t, err)
	disableCoverageTx, err := bindings.RplVault.SetEnforceWETHCoverageRatio(false, deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, disableCoverageTx, deployerOpts, "Disabled RPL coverage limitation")

	// Mint 1000 xrETH + 1000 ETH of xRPL
	ethAmount := eth.EthToWei(1000)
	// TODO: Make sure to disable the coverage limitation
	// Also add a require that it's not disabled prior to this
	cstestutils.DepositToWethVault(t, testMgr, bindings.WethVault, bindings.Weth, ethAmount, deployerOpts)

	var rplPrice *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.PriceFetcher.GetRplPrice(mc, &rplPrice)
		return nil
	}, nil)

	require.NoError(t, err)
	t.Logf("RPL price: %v", rplPrice.String())
	rplAmount := ethAmount.Mul(ethAmount, rplPrice)
	rplAmount = rplAmount.Div(rplAmount, eth.EthToWei(1e18))
	t.Logf("Minting RPL: %v", rplAmount.String())
	cstestutils.DepositToRplVault(t, testMgr, bindings.RplVault, bindings.Rpl, rplAmount, deployerOpts)

	// Enable RPL coverage and set to 30%
	enableCoverageTx, err := bindings.RplVault.SetEnforceWETHCoverageRatio(true, deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, enableCoverageTx, deployerOpts, "Enabled RPL coverage limitation")

	// Set the coverage ratio to 30%
	coverageRatio := big.NewInt(3e17)
	t.Logf("Setting RPL coverage limitation: %v", coverageRatio.String())
	setCoverageTx, err := bindings.RplVault.SetWETHCoverageRatio(coverageRatio, deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, setCoverageTx, deployerOpts, "Set RPL coverage limitation to 30%")

	// Attempt to mint 1 xrETH (should fail)
	ethAmount = eth.EthToWei(1)
	err = testMgr.Constellation_DepositToWethVault(bindings.Weth, bindings.WethVault, ethAmount, deployerOpts)
	require.Error(t, err)
	t.Logf("Failed to mint 1 xrETH as expected: %v", err)
}

// Disable RPL coverage limitation, deposit 1000 xrETH/ETH, set RPL coverage limitation to 100%
func TestMinipoolDepositAndStake_BigRplBootstrap_MintEqualToCoverageRatio(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

	// Get some services
	sp := testMgr.GetConstellationServiceProvider()
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()

	// Disable RPL coverage limitation
	bindings, err := cstestutils.CreateBindings(testMgr.GetConstellationServiceProvider())
	require.NoError(t, err)
	disableCoverageTx, err := bindings.RplVault.SetEnforceWETHCoverageRatio(false, deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, disableCoverageTx, deployerOpts, "Disabled RPL coverage limitation")

	// Mint 1000 xrETH + 1000 ETH of xRPL
	ethAmount := eth.EthToWei(1000)
	// TODO: Make sure to disable the coverage limitation
	// Also add a require that it's not disabled prior to this
	cstestutils.DepositToWethVault(t, testMgr, bindings.WethVault, bindings.Weth, ethAmount, deployerOpts)

	var rplPrice *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.PriceFetcher.GetRplPrice(mc, &rplPrice)
		return nil
	}, nil)

	require.NoError(t, err)
	t.Logf("RPL price: %v", rplPrice.String())
	rplAmount := ethAmount.Mul(ethAmount, rplPrice)
	rplAmount = rplAmount.Div(rplAmount, eth.EthToWei(1e18))
	t.Logf("Minting RPL: %v", rplAmount.String())
	cstestutils.DepositToRplVault(t, testMgr, bindings.RplVault, bindings.Rpl, rplAmount, deployerOpts)

	// Enable RPL coverage and set to 100%
	enableCoverageTx, err := bindings.RplVault.SetEnforceWETHCoverageRatio(true, deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, enableCoverageTx, deployerOpts, "Enabled RPL coverage limitation")

	coverageRatio := big.NewInt(1e18)
	t.Logf("Setting RPL coverage limitation: %v", coverageRatio.String())
	setCoverageTx, err := bindings.RplVault.SetWETHCoverageRatio(coverageRatio, deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, setCoverageTx, deployerOpts, "Set RPL coverage limitation to 100%")

	// Attempt to mint 1 xrETH (should fail)
	ethAmount = eth.EthToWei(1)
	err = testMgr.Constellation_DepositToWethVault(bindings.Weth, bindings.WethVault, ethAmount, deployerOpts)
	require.Error(t, err)
	t.Logf("Failed to mint 1 xrETH as expected: %v", err)
}

// Disable RPL coverage limitation, deposit 1000 xrETH/ETH, set RPL coverage limitation to 200%
func TestMinipoolDepositAndStake_BigRplBootstrap_MintLessThanCoverageRatio(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

	// Get some services
	sp := testMgr.GetConstellationServiceProvider()
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()

	// Disable RPL coverage limitation
	bindings, err := cstestutils.CreateBindings(testMgr.GetConstellationServiceProvider())
	require.NoError(t, err)
	disableCoverageTx, err := bindings.RplVault.SetEnforceWETHCoverageRatio(false, deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, disableCoverageTx, deployerOpts, "Disabled RPL coverage limitation")

	// Mint 1000 xrETH + 1000 ETH of xRPL
	ethAmount := eth.EthToWei(1000)
	// TODO: Make sure to disable the coverage limitation
	// Also add a require that it's not disabled prior to this
	cstestutils.DepositToWethVault(t, testMgr, bindings.WethVault, bindings.Weth, ethAmount, deployerOpts)

	var rplPrice *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.PriceFetcher.GetRplPrice(mc, &rplPrice)
		return nil
	}, nil)

	require.NoError(t, err)
	t.Logf("RPL price: %v", rplPrice.String())
	rplAmount := ethAmount.Mul(ethAmount, rplPrice)
	rplAmount = rplAmount.Div(rplAmount, eth.EthToWei(1e18))
	t.Logf("Minting RPL: %v", rplAmount.String())
	cstestutils.DepositToRplVault(t, testMgr, bindings.RplVault, bindings.Rpl, rplAmount, deployerOpts)

	// Enable RPL coverage and set to 200%
	enableCoverageTx, err := bindings.RplVault.SetEnforceWETHCoverageRatio(true, deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, enableCoverageTx, deployerOpts, "Enabled RPL coverage limitation")

	// Set the coverage ratio to 200%
	coverageRatio := big.NewInt(2e18)
	t.Logf("Setting RPL coverage limitation: %v", coverageRatio.String())
	setCoverageTx, err := bindings.RplVault.SetWETHCoverageRatio(coverageRatio, deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, setCoverageTx, deployerOpts, "Set RPL coverage limitation to 200%")

	// Attempt to mint 1 xrETH (should fail)
	ethAmount = eth.EthToWei(1)
	err = testMgr.Constellation_DepositToWethVault(bindings.Weth, bindings.WethVault, ethAmount, deployerOpts)
	require.NoError(t, err)
	t.Logf("Minted 1 xrETH as expected: %v", err)
}

// Utility function to send ETH and advance blockchain time
func sendEthAndAdvanceTime(t *testing.T, address common.Address, amount *big.Int, slotsToAdvance int) {
	sp := testMgr.GetConstellationServiceProvider()
	txMgr := sp.GetTransactionManager()

	sendEthOpts := &bind.TransactOpts{
		From:  deployerOpts.From,
		Value: amount,
	}

	sendEthTx := txMgr.CreateTransactionInfoRaw(address, nil, sendEthOpts)
	testMgr.MineTx(t, sendEthTx, deployerOpts, "Sent ETH")

	err := testMgr.AdvanceSlots(uint(slotsToAdvance), false)
	require.NoError(t, err)
	t.Logf("Advanced %d slots", slotsToAdvance)
}

// Run a check to make sure depositing with duplicate salts fails
func TestDuplicateSalts(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

	// Make the bindings
	bindings, err := cstestutils.CreateBindings(testMgr.GetConstellationServiceProvider())
	require.NoError(t, err)
	t.Log("Created contract bindings")

	// Get some services
	cs := testMgr.GetApiClient()

	// Make a normal minipool
	depositAndStakeMinipool(t, bindings)

	// Deposit RPL to the RPL vault
	rplAmount := eth.EthToWei(3200)
	cstestutils.DepositToRplVault(t, testMgr, bindings.RplVault, bindings.Rpl, rplAmount, deployerOpts)

	// Deposit WETH to the WETH vault
	wethAmount := eth.EthToWei(90)
	cstestutils.DepositToWethVault(t, testMgr, bindings.WethVault, bindings.Weth, wethAmount, deployerOpts)

	// Try making another one with the same salt, it should fail
	_, err = cs.Minipool.Create(salt)
	require.Error(t, err)
	t.Logf("Failed to create minipool with duplicate salt as expected: %v", err)
}

// Simulate an ETH reward getting deposited to YieldDistributor
func simulateEthRewardToYieldDistributor(t *testing.T, bindings *cstestutils.ContractBindings) {
	sp := testMgr.GetConstellationServiceProvider()
	qMgr := sp.GetQueryManager()
	slotsToAdvance := 1200 * 60 * 60 / 12

	// Get balances before harvest
	var wethBalanceNodeBefore *big.Int
	var wethBalanceTreasuryBefore *big.Int

	err := qMgr.Query(func(mc *batch.MultiCaller) error {
		bindings.Weth.BalanceOf(mc, &wethBalanceNodeBefore, nodeAddress)
		bindings.Weth.BalanceOf(mc, &wethBalanceTreasuryBefore, bindings.TreasuryAddress)
		return nil
	}, nil)
	require.NoError(t, err)

	oneEth := big.NewInt(1e18)

	// Send 1 ETH to the deposit pool
	sendEthAndAdvanceTime(t, bindings.DepositPoolAddress, oneEth, slotsToAdvance)

	// Send 1 ETH to the yield distributor
	sendEthAndAdvanceTime(t, bindings.YieldDistributor.Address, oneEth, 0)

	// Call harvest()
	harvestTx, err := bindings.YieldDistributor.Harvest(nodeAddress, common.Big0, common.Big1, deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, harvestTx, deployerOpts, "Called harvest from YieldDistributor")

	// Again - to simulate an interval tick for rewards to go to treasury

	// Send 1 ETH to the deposit pool
	sendEthAndAdvanceTime(t, bindings.DepositPoolAddress, oneEth, slotsToAdvance)

	// Send 1 ETH to the yield distributor
	sendEthAndAdvanceTime(t, bindings.YieldDistributor.Address, oneEth, 0)

	// Call harvest()
	harvestTx, err = bindings.YieldDistributor.Harvest(nodeAddress, common.Big1, common.Big2, deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, harvestTx, deployerOpts, "Called harvest from YieldDistributor")

	// Get balances after harvest
	var wethBalanceNodeAfter *big.Int
	var wethBalanceTreasuryAfter *big.Int

	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		bindings.Weth.BalanceOf(mc, &wethBalanceNodeAfter, nodeAddress)
		bindings.Weth.BalanceOf(mc, &wethBalanceTreasuryAfter, bindings.TreasuryAddress)
		return nil
	}, nil)
	require.NoError(t, err)

	// Verify balances
	require.Equal(t, 1, wethBalanceNodeAfter.Cmp(wethBalanceNodeBefore))
	require.Equal(t, 1, wethBalanceTreasuryAfter.Cmp(wethBalanceTreasuryBefore))
}

// Makes a minipool and stakes it
func depositAndStakeMinipool(t *testing.T, bindings *cstestutils.ContractBindings) {
	// Get some services
	sp := testMgr.GetConstellationServiceProvider()
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()

	// Query some details
	var rplPrice *big.Int
	var totalEthStaking *big.Int
	var minipoolBond *big.Int
	err := qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.PriceFetcher.GetRplPrice(mc, &rplPrice)
		csMgr.SuperNodeAccount.TotalEthStaking(mc, &totalEthStaking)
		csMgr.SuperNodeAccount.Bond(mc, &minipoolBond)
		return nil
	}, nil,
		bindings.RpSuperNode.Exists,
		bindings.RpSuperNode.RplStake,
		bindings.DepositPoolManager.Balance,
		bindings.ProtocolDaoManager.Settings.Deposit.MaximumDepositPoolSize,
		bindings.OracleDaoManager.Settings.Minipool.ScrubPeriod,
	)
	require.NoError(t, err)

	// Verify some details
	require.True(t, bindings.RpSuperNode.Exists.Get())
	t.Log("Supernode account is registered with RP")
	require.Equal(t, 0, bindings.RpSuperNode.RplStake.Get().Cmp(common.Big0))
	t.Log("Supernode account does not have any RPL staked")
	require.Equal(t, 0, bindings.DepositPoolManager.Balance.Get().Cmp(common.Big0))
	t.Log("Deposit pool balance is zero")
	require.Equal(t, 1, rplPrice.Cmp(common.Big0))
	t.Logf("RPL price is %.6f RPL/ETH (%s wei)", eth.WeiToEth(rplPrice), rplPrice.String())

	// Send ETH to the RP deposit pool
	fundOpts := &bind.TransactOpts{
		From:  deployerOpts.From,
		Value: bindings.ProtocolDaoManager.Settings.Deposit.MaximumDepositPoolSize.Get(), // Deposit the maximum amount
	}
	txInfo, err := bindings.DepositPoolManager.Deposit(fundOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, deployerOpts, "Funded the RP deposit pool")

	// ===
	/*
		newLockThreshold := eth.EthToWei(2)
		txInfo, err = csMgr.SuperNodeAccount.SetLockAmount(newLockThreshold, adminOpts)
		require.NoError(t, err)
		testMgr.MineTx(t, txInfo, adminOpts, "Set the lock amount")
	*/
	// ===

	// Get the RPL requirement
	var rplShortfall *big.Int
	ethAmount := new(big.Int).Add(totalEthStaking, minipoolBond)
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.OperatorDistributor.CalculateRplStakeShortfall(mc, &rplShortfall, bindings.RpSuperNode.RplStake.Get(), ethAmount)
		return nil
	}, nil)
	require.NoError(t, err)
	t.Logf("RPL shortfall for %.2f ETH bond is %.6f RPL (%s wei)", eth.WeiToEth(minipoolBond), eth.WeiToEth(rplShortfall), rplShortfall.String())

	// Deposit RPL to the RPL vault
	rplRequired := new(big.Int).Mul(rplShortfall, big.NewInt(1e18))
	rplRequired.Div(rplRequired, eth.EthToWei(0.98)) // TEMP: Add 2%, the required collateral - get this from the contracts later
	rplRequired.Add(rplRequired, common.Big1)        // Add 1 wei to the required amount to make it pass the greater check
	//t.Logf("RPL required for 8 ETH bond is %.6f RPL (%s wei)", eth.WeiToEth(rplShortfall), rplShortfall.String())
	rplAmount := rplRequired // eth.EthToWei(3200)
	cstestutils.DepositToRplVault(t, testMgr, bindings.RplVault, bindings.Rpl, rplAmount, deployerOpts)

	// Deposit WETH to the WETH vault
	ethRequired := new(big.Int).Mul(minipoolBond, big.NewInt(1e18))
	ethRequired.Div(ethRequired, eth.EthToWei(0.9)) // TEMP: Add 10%, the required collateral - get this from the contracts later
	ethRequired.Add(ethRequired, common.Big1)       // Add 1 wei to the required amount to make it pass the greater check
	wethAmount := ethRequired                       // eth.EthToWei(90)
	cstestutils.DepositToWethVault(t, testMgr, bindings.WethVault, bindings.Weth, wethAmount, deployerOpts)

	// Register with Constellation
	cstestutils.RegisterWithConstellation(t, testMgr)

	// Set the available minipool count
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	nsMgr.SetAvailableConstellationMinipoolCount(nsEmail, 1)
	t.Log("Set up the NodeSet mock server")

	// Deposit to make a minipool
	mp := cstestutils.CreateMinipoolViaDeposit(t, testMgr, salt, bindings.RpSuperNode, bindings.MinipoolManager)

	// Stake the minipool
	cstestutils.StakeMinipool(t, testMgr, nodeAddress, mp, bindings.OracleDaoManager.Settings.Minipool.ScrubPeriod.Formatted())
}
