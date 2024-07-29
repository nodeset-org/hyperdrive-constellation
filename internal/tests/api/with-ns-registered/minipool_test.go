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
	nsMgr.SetAvailableConstellationMinipoolCount(nodeAddress, expectedMinipoolCount)

	// Check the available minipool count
	cs := testMgr.GetApiClient()
	countResponse, err := cs.Minipool.GetAvailableMinipoolCount()
	require.NoError(t, err)
	require.Equal(t, expectedMinipoolCount, countResponse.Data.Count)
}

// Run a full cycle test of provisioning RP and Constellation, then depositing and staking a minipool
func TestMinipoolDepositAndStake(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

	depositAndStakeMinipool(t)
	simulateEthRewardToYieldDistributor(t)
}

// Simulate an ETH reward getting deposited to YieldDistributor
func simulateEthRewardToYieldDistributor(t *testing.T) {
	sp := testMgr.GetConstellationServiceProvider()
	csMgr := sp.GetConstellationManager()
	txMgr := sp.GetTransactionManager()
	qMgr := sp.GetQueryManager()
	// ec := sp.GetEthClient()

	bindings, err := cstestutils.CreateBindings(testMgr.GetConstellationServiceProvider())

	// Get balances before harvest
	var wethBalanceNodeBefore *big.Int
	var wethBalanceYieldDistributorBefore *big.Int
	var wethBalanceTreasuryBefore *big.Int
	// ethBalanceNodeBefore, err := ec.BalanceAt(context.Background(), nodeAddress, nil)
	// ethBalanceYieldDistributorBefore, err := ec.BalanceAt(context.Background(), bindings.YieldDistributor.Address, nil)
	// ethBalanceTreasuryBefore, err := ec.BalanceAt(context.Background(), bindings.TreasuryAddress, nil)

	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		bindings.Weth.BalanceOf(mc, &wethBalanceNodeBefore, nodeAddress)
		bindings.Weth.BalanceOf(mc, &wethBalanceYieldDistributorBefore, bindings.YieldDistributor.Address)
		bindings.Weth.BalanceOf(mc, &wethBalanceTreasuryBefore, bindings.TreasuryAddress)
		return nil
	}, nil)

	sendEthOpts := &bind.TransactOpts{
		From:  deployerOpts.From,
		Value: big.NewInt(1e18),
	}

	// Send 1 ETH to the deposit pool
	sendEthTx := txMgr.CreateTransactionInfoRaw(bindings.DepositPoolAddress, nil, sendEthOpts)
	testMgr.MineTx(t, sendEthTx, deployerOpts, "Sent ETH to deposit pool")

	// Advance blockchain time
	slotsToAdvance := 1200 * 60 * 60 / 12
	err = testMgr.AdvanceSlots(uint(slotsToAdvance), false)
	require.NoError(t, err)
	t.Logf("Advanced %d slots", slotsToAdvance)

	require.NoError(t, err)
	t.Log("Created contract bindings")

	// Send 1 ETH to the yield distributor
	sendEthTx = txMgr.CreateTransactionInfoRaw(csMgr.YieldDistributor.Address, nil, sendEthOpts)
	testMgr.MineTx(t, sendEthTx, deployerOpts, "Sent ETH to deposit pool")

	// Call harvest()
	harvestTx, err := bindings.YieldDistributor.Harvest(nodeAddress, big.NewInt(0), big.NewInt(1), deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, harvestTx, deployerOpts, "Called harvest from YieldDistributor")

	// Get balances after harvest
	var wethBalanceNodeAfter *big.Int
	var wethBalanceYieldDistributorAfter *big.Int
	var wethBalanceTreasuryAfter *big.Int
	// ethBalanceNodeAfter, err := ec.BalanceAt(context.Background(), nodeAddress, nil)
	// ethBalanceYieldDistributorAfter, err := ec.BalanceAt(context.Background(), bindings.YieldDistributor.Address, nil)
	// ethBalanceTreasuryAfter, err := ec.BalanceAt(context.Background(), bindings.TreasuryAddress, nil)

	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		bindings.Weth.BalanceOf(mc, &wethBalanceNodeAfter, nodeAddress)
		bindings.Weth.BalanceOf(mc, &wethBalanceYieldDistributorAfter, bindings.YieldDistributor.Address)
		bindings.Weth.BalanceOf(mc, &wethBalanceTreasuryAfter, bindings.TreasuryAddress)
		return nil
	}, nil)

	// Verify balances
	require.Equal(t, 1, wethBalanceNodeAfter.Cmp(wethBalanceNodeBefore))
	// require.Equal(t, 1, ethBalanceYieldDistributorAfter.Cmp(ethBalanceYieldDistributorBefore))
	// require.Equal(t, 1, ethBalanceTreasuryAfter.Cmp(ethBalanceTreasuryBefore))
}

// Makes a minipool and stakes it
func depositAndStakeMinipool(t *testing.T) {
	// Get some services
	sp := testMgr.GetConstellationServiceProvider()
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()

	// Make the bindings
	bindings, err := cstestutils.CreateBindings(testMgr.GetConstellationServiceProvider())
	require.NoError(t, err)
	t.Log("Created contract bindings")

	// Query some details
	var rplPrice *big.Int
	var totalEthStaking *big.Int
	var minipoolBond *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
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
	nsMgr.SetAvailableConstellationMinipoolCount(nodeAddress, 1)
	t.Log("Set up the NodeSet mock server")

	// Deposit to make a minipool
	salt := big.NewInt(0x90de5e7)
	mp := cstestutils.CreateMinipoolViaDeposit(t, testMgr, salt, bindings.RpSuperNode, bindings.MinipoolManager)

	// Stake the minipool
	cstestutils.StakeMinipool(t, testMgr, mp, bindings.OracleDaoManager.Settings.Minipool.ScrubPeriod.Formatted())
}
