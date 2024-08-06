package qa

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log/slog"
	"math/big"
	"path/filepath"
	"runtime/debug"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	"github.com/nodeset-org/hyperdrive-constellation/common/contracts"
	"github.com/nodeset-org/hyperdrive-constellation/common/contracts/constellation"
	cstestutils "github.com/nodeset-org/hyperdrive-constellation/internal/tests/utils"
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	cstesting "github.com/nodeset-org/hyperdrive-constellation/testing"
	hdtesting "github.com/nodeset-org/hyperdrive-daemon/testing"
	"github.com/nodeset-org/nodeset-client-go/utils"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/beacon"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/node-manager-core/node/validator"
	"github.com/rocket-pool/rocketpool-go/v2/dao/protocol"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
	"github.com/rocket-pool/rocketpool-go/v2/rewards"
	"github.com/rocket-pool/rocketpool-go/v2/types"
	"github.com/stretchr/testify/require"
	"github.com/wealdtech/go-merkletree"
	"github.com/wealdtech/go-merkletree/keccak256"
)

var (
	shouldPrintTickInfo bool = false
)

// Run test 3 of the QA suite
func Test3_ComplexRoundTrip(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer qa_cleanup(snapshotName)

	// Get some services
	bindings, err := cstestutils.CreateBindings(mainNode.GetServiceProvider())
	require.NoError(t, err)
	sp := mainNode.GetServiceProvider()
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()
	txMgr := sp.GetTransactionManager()
	ec := sp.GetEthClient()
	t.Log("Created services")
	printTickInfo(t, sp)

	// Create some subnodes
	nodes, nodeAddresses, err := createNodesForTest(t, 4, eth.EthToWei(100))
	require.NoError(t, err)
	printTickInfo(t, sp)

	// Get the current interval
	expectedInterval := common.Big1
	var currentInterval *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.YieldDistributor.GetCurrentInterval(mc, &currentInterval)
		return nil
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 0, expectedInterval.Cmp(currentInterval))
	t.Logf("The current Constellation interval is %d as expected", currentInterval.Uint64())

	// Make sure the contract state is clean
	runPreflightChecks(t, bindings)

	// Deposit RPL to the RPL vault
	rplAmount := eth.EthToWei(4000)
	cstestutils.DepositToRplVault(t, testMgr, bindings.RplVault, bindings.Rpl, rplAmount, deployerOpts)
	printTickInfo(t, sp)

	// Deposit WETH to the WETH vault
	wethAmount := eth.EthToWei(100)
	cstestutils.DepositToWethVault(t, testMgr, bindings.WethVault, bindings.Weth, wethAmount, deployerOpts)
	printTickInfo(t, sp)

	// Set the available minipool count for the user
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	err = nsMgr.SetAvailableConstellationMinipoolCount(nsEmail, 1)
	require.NoError(t, err)
	t.Log("Set up the NodeSet mock server")

	// Build the minipool creation TXs
	minipoolsPerNode := 1
	datas, hashes := cstestutils.BuildAndSubmitCreateMinipoolTxs(t, nodes, minipoolsPerNode, nil, bindings.RpSuperNode)

	// Mine a block
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined a block")

	// Wait for each TX
	for i, node := range nodes {
		hashesPerNode := hashes[i]
		hd := node.GetHyperdriveNode().GetApiClient()
		for _, hash := range hashesPerNode {
			_, err = hd.Tx.WaitForTransaction(hash)
			require.NoError(t, err)
		}
	}
	t.Log("Finished waiting for minipool creation TXs")

	// Verify minipools
	mps := make([]minipool.IMinipool, len(nodes))
	for i, dataForNode := range datas {
		for j, data := range dataForNode {
			index := i*minipoolsPerNode + j
			mp := cstestutils.VerifyMinipoolAfterCreation(t, qMgr, bindings.RpSuperNode, uint64(index), data.MinipoolAddress, bindings.MinipoolManager)
			mps[index] = mp
		}
	}
	t.Log("Verified minipools")
	printTickInfo(t, sp)
	expectedMpIndex := 0

	// Get some state
	var nextMinipoolAddress common.Address
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.OperatorDistributor.GetNextMinipool(mc, &nextMinipoolAddress)
		return nil
	}, nil,
		bindings.OracleDaoManager.Settings.Minipool.ScrubPeriod,
	)
	require.NoError(t, err)
	require.Equal(t, datas[expectedMpIndex][0].MinipoolAddress, nextMinipoolAddress)
	t.Logf("The next minipool to tick is %s as expected (index %d)", nextMinipoolAddress.Hex(), expectedMpIndex)

	// Fast forward time
	timeToAdvance := bindings.OracleDaoManager.Settings.Minipool.ScrubPeriod.Formatted()
	secondsPerSlot := time.Duration(testMgr.GetBeaconMockManager().GetConfig().SecondsPerSlot) * time.Second
	slotsToAdvance := uint(timeToAdvance / secondsPerSlot)
	err = testMgr.AdvanceSlots(slotsToAdvance, false)
	require.NoError(t, err)
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Logf("Advanced %d slots", slotsToAdvance)

	// Stake the minipools
	for i, mp := range mps {
		cstestutils.StakeMinipool(t, testMgr, nodes[i], nodeAddresses[i], mp)
	}
	t.Log("Staked the minipools")
	printTickInfo(t, sp)

	// Submit 0.010 ETH in rewards on Beacon and 0.005 on the EL per validator
	elRewardsPerMinipool := eth.EthToWei(0.005)
	beaconRewardsPerValidator := 1e7 // Beacon is in gwei
	simulateBeaconRewards(t, sp, datas, elRewardsPerMinipool, uint64(beaconRewardsPerValidator), deployerOpts)
	totalYieldAccrued := calculateXrEthOracleTotalYieldAccrued(t, sp, bindings)
	t.Logf("The new total yield accrued to report is %.10f (%s wei)", eth.WeiToEth(totalYieldAccrued), totalYieldAccrued.String())

	// Update the oracle report
	chainID := new(big.Int).SetUint64(testMgr.GetBeaconMockManager().GetConfig().ChainID)
	newTime := time.Now().Add(timeToAdvance)
	sig, err := createXrEthOracleSignature(totalYieldAccrued, newTime, csMgr.PoABeaconOracle.Address, chainID, deployerKey)
	require.NoError(t, err)
	txInfo, err := csMgr.PoABeaconOracle.SetTotalYieldAccrued(totalYieldAccrued, sig, newTime, deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, deployerOpts, "Updated the xrETH Oracle")
	printTickInfo(t, sp)

	// Verify the new ETH:xrETH price
	oneEth := big.NewInt(1e18)
	numerator := new(big.Int).Add(wethAmount, totalYieldAccrued)
	numerator.Mul(numerator, oneEth)
	expectedRatio := new(big.Int).Div(numerator, wethAmount)
	xrEthPriceAccordingToVault := getTokenPrice(t, qMgr, csMgr.WethVault)
	requireApproxEqual(t, expectedRatio, xrEthPriceAccordingToVault)
	t.Logf("The new ETH:xrETH price according to the token is %.10f (%s wei)", eth.WeiToEth(xrEthPriceAccordingToVault), xrEthPriceAccordingToVault.String())

	// Redeem 5 xrETH
	xrEthRedeemAmount := eth.EthToWei(5)
	wethReturned := redeemToken(t, qMgr, txMgr, bindings.WethVault, xrEthRedeemAmount, false, deployerOpts)
	expectedAmount := new(big.Int).Mul(xrEthRedeemAmount, xrEthPriceAccordingToVault)
	expectedAmount.Div(expectedAmount, oneEth)
	requireApproxEqual(t, expectedAmount, wethReturned)
	t.Logf("Redeemed %.6f xrETH (%s wei) for %.6f WETH (%s wei)", eth.WeiToEth(xrEthRedeemAmount), xrEthRedeemAmount.String(), eth.WeiToEth(wethReturned), wethReturned.String())
	expectedMpIndex++
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.OperatorDistributor.GetNextMinipool(mc, &nextMinipoolAddress)
		return nil
	}, nil)
	require.NoError(t, err)
	require.Equal(t, datas[expectedMpIndex][0].MinipoolAddress, nextMinipoolAddress)
	t.Logf("The next minipool to tick is %s as expected (index %d)", nextMinipoolAddress.Hex(), expectedMpIndex)

	printTickInfo(t, sp)

	// Redeem 5 xRPL
	xRplRedeemAmount := eth.EthToWei(5)
	rplReturned := redeemToken(t, qMgr, txMgr, bindings.RplVault, xRplRedeemAmount, false, deployerOpts)
	expectedAmount = xRplRedeemAmount
	require.Equal(t, expectedAmount, rplReturned)
	t.Logf("Redeemed %.6f xRPL (%s wei) for %.6f RPL (%s wei)", eth.WeiToEth(xRplRedeemAmount), xRplRedeemAmount.String(), eth.WeiToEth(rplReturned), rplReturned.String())
	expectedMpIndex++
	//nextMinipoolAddress, err = csMgr.OperatorDistributor.GetNextMinipoolDebug()
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.OperatorDistributor.GetNextMinipool(mc, &nextMinipoolAddress)
		return nil
	}, nil)
	require.NoError(t, err)
	require.Equal(t, datas[expectedMpIndex][0].MinipoolAddress, nextMinipoolAddress)
	t.Logf("The next minipool to tick is %s as expected (index %d)", nextMinipoolAddress.Hex(), expectedMpIndex)
	printTickInfo(t, sp)

	// Exit the first 3 minipools and set their balance as withdrawn
	for i := 0; i < 3; i++ {
		setMinipoolToWithdrawn(t, sp, datas[i][0], deployerOpts)
	}

	// Attempt an NO claim - should fail since an interval isn't finalized yet
	claimResp, err := nodes[0].GetApiClient().Node.ClaimRewards(common.Big1, common.Big1)
	require.NoError(t, err)
	require.True(t, claimResp.Data.TxInfo.SimulationResult.IsSimulated)
	require.NotEmpty(t, claimResp.Data.TxInfo.SimulationResult.SimulationError)
	t.Logf("Attempt to claim rewards for node 0 failed as expected: %s", claimResp.Data.TxInfo.SimulationResult.SimulationError)

	// Fast forward time by a week
	seconds := uint64(24 * 60 * 60 * 7)
	secondsDuration := time.Duration(seconds) * time.Second
	slots := secondsDuration / secondsPerSlot
	err = testMgr.AdvanceSlots(uint(slots), false)
	require.NoError(t, err)
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Logf("Fast forwarded 1 week")

	// Get the current interval pre-tick
	var preTickInterval *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.YieldDistributor.GetCurrentInterval(mc, &preTickInterval)
		return nil
	}, nil)
	require.NoError(t, err)
	require.Zero(t, expectedInterval.Cmp(preTickInterval))
	t.Logf("The current Constellation interval is %d", preTickInterval.Uint64())

	// Run the tick 3 times
	for i := 0; i < 3; i++ {
		err := qMgr.Query(func(mc *batch.MultiCaller) error {
			csMgr.OperatorDistributor.GetNextMinipool(mc, &nextMinipoolAddress)
			return nil
		}, nil)
		require.NoError(t, err)
		require.Equal(t, datas[expectedMpIndex][0].MinipoolAddress, nextMinipoolAddress)
		t.Logf("The next minipool to tick is %s as expected (index %d)", nextMinipoolAddress.Hex(), expectedMpIndex)

		txInfo, err := csMgr.OperatorDistributor.ProcessNextMinipool(deployerOpts)
		require.NoError(t, err)
		testMgr.MineTx(t, txInfo, deployerOpts, fmt.Sprintf("Processed the next minipool (tick %d)", i+1))

		var postTickInterval *big.Int
		err = qMgr.Query(func(mc *batch.MultiCaller) error {
			csMgr.YieldDistributor.GetCurrentInterval(mc, &postTickInterval)
			return nil
		}, nil)
		require.NoError(t, err)
		require.Equal(t, preTickInterval.Uint64()+1, postTickInterval.Uint64())
		if i == 0 {
			// Ensure the interval got incremented
			t.Logf("Constellation interval has been increased to %d as expected", postTickInterval.Uint64())
		} else {
			// Ensure the interval didn;t get incremented
			t.Logf("Constellation interval is still %d as expected", postTickInterval.Uint64())
		}
		printTickInfo(t, sp)
		expectedMpIndex++
		if expectedMpIndex >= len(datas) {
			expectedMpIndex = 0
		}
	}

	// Update the xrETH Oracle again
	totalYieldAccrued = calculateXrEthOracleTotalYieldAccrued(t, sp, bindings)
	newTime = newTime.Add(time.Hour)
	t.Logf("The new total yield accrued to report is %.10f (%s wei)", eth.WeiToEth(totalYieldAccrued), totalYieldAccrued.String())
	sig, err = createXrEthOracleSignature(totalYieldAccrued, newTime, csMgr.PoABeaconOracle.Address, chainID, deployerKey)
	require.NoError(t, err)
	txInfo, err = csMgr.PoABeaconOracle.SetTotalYieldAccrued(totalYieldAccrued, sig, newTime, deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, deployerOpts, "Updated the xrETH Oracle")

	// Verify the new ETH:xrETH price
	xrEthPriceAccordingToVault = getTokenPrice(t, qMgr, csMgr.WethVault)
	requireApproxEqual(t, expectedRatio, xrEthPriceAccordingToVault)
	t.Logf("The new ETH:xrETH price according to the token is %.10f (%s wei)", eth.WeiToEth(xrEthPriceAccordingToVault), xrEthPriceAccordingToVault.String())

	// Get the stats for interval 1
	var intervalStats constellation.Interval
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.YieldDistributor.GetIntervalByIndex(mc, &intervalStats, preTickInterval)
		return nil
	}, nil)
	require.NoError(t, err)
	expectedShare := 0.14788 * 0.3625 * (0.005 + 0.005 + 0.015) // Quick and dirty; CS NO share * RP NO share * (MP0 + MP1 + MP2)
	expectedShareBig := eth.EthToWei(expectedShare)
	requireApproxEqual(t, expectedShareBig, intervalStats.Amount)
	nodeOpShare := new(big.Int).Div(intervalStats.Amount, intervalStats.NumOperators)
	t.Logf("Interval %d had %.6f ETH (%s wei) across %d operators for %.6f ETH (%s wei) each",
		preTickInterval.Uint64(),
		eth.WeiToEth(intervalStats.Amount),
		intervalStats.Amount.String(),
		intervalStats.NumOperators.Uint64(),
		eth.WeiToEth(nodeOpShare),
		nodeOpShare.String(),
	)

	// Run NO claims
	for i, node := range nodes {
		preBalance, err := ec.BalanceAt(context.Background(), nodeAddresses[i], nil)
		require.NoError(t, err)

		cs := node.GetApiClient()
		claimResp, err := cs.Node.ClaimRewards(common.Big1, common.Big1)
		require.NoError(t, err)
		require.True(t, claimResp.Data.TxInfo.SimulationResult.IsSimulated)
		require.Empty(t, claimResp.Data.TxInfo.SimulationResult.SimulationError)

		testMgr.MineTx(t, claimResp.Data.TxInfo, deployerOpts, fmt.Sprintf("Node op %d claimed rewards", i))

		postBalance, err := ec.BalanceAt(context.Background(), nodeAddresses[i], nil)
		require.NoError(t, err)
		rewards := new(big.Int).Sub(postBalance, preBalance)
		requireApproxEqual(t, nodeOpShare, rewards)
		t.Logf("Node op %d claimed rewards and received %.6f ETH (%s wei)", i, eth.WeiToEth(rewards), rewards.String())
	}

	// Run a treasury claim
	treasuryRecipient := odaoOpts[0].From
	preBalance, err := ec.BalanceAt(context.Background(), treasuryRecipient, nil)
	require.NoError(t, err)

	txInfo, err = csMgr.Treasury.ClaimEth(treasuryRecipient, adminOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, adminOpts, "Treasury claimed ETH rewards")

	postBalance, err := ec.BalanceAt(context.Background(), treasuryRecipient, nil)
	require.NoError(t, err)
	treasuryRewards := new(big.Int).Sub(postBalance, preBalance)
	expectedShare = 0.14788 * 0.3625 * (0.005 + 0.005 + 0.015 + 0.005 + 0.005 + 0.01 + 0.01) // Quick and dirty; CS NO share * RP NO share * (MP0 + MP1 + MP2 + MP3 + MP4 + MP0 again + MP1 again)
	expectedShareBig = eth.EthToWei(expectedShare)
	requireApproxEqual(t, expectedShareBig, treasuryRewards)
	t.Logf("Treasury claimed ETH rewards and received %.6f ETH (%s wei)", eth.WeiToEth(treasuryRewards), treasuryRewards.String())
}

// Run test 4 of the QA suite
func Test4_SimpleNOConcurrency(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer qa_cleanup(snapshotName)

	// Get some services
	bindings, err := cstestutils.CreateBindings(mainNode.GetServiceProvider())
	require.NoError(t, err)
	t.Log("Created bindings")

	// Create some subnodes
	nodes, _, err := createNodesForTest(t, 1, eth.EthToWei(100))
	require.NoError(t, err)

	// Make sure the contract state is clean
	runPreflightChecks(t, bindings)

	// Get the deposit amounts
	wethAmount, rplAmount := getDepositAmounts(t, bindings, testMgr.GetNode().GetServiceProvider(), 1)

	// Deposit RPL to the RPL vault
	cstestutils.DepositToRplVault(t, testMgr, bindings.RplVault, bindings.Rpl, rplAmount, deployerOpts)

	// Deposit WETH to the WETH vault
	cstestutils.DepositToWethVault(t, testMgr, bindings.WethVault, bindings.Weth, wethAmount, deployerOpts)

	// Build the minipool creation TXs
	_, hashes := cstestutils.BuildAndSubmitCreateMinipoolTxs(t, nodes, 1, nil, bindings.RpSuperNode)

	// Mine a block
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined a block")

	// The first one should succeed
	hd := mainNode.GetHyperdriveNode().GetApiClient()
	_, err = hd.Tx.WaitForTransaction(hashes[0][0])
	require.NoError(t, err)
	t.Log("First minipool creation TX succeeded")

	// The second one should fail
	_, err = hd.Tx.WaitForTransaction(hashes[1][0])
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed with status 0")
	t.Logf("Second minipool creation TX failed as expected: %v", err)
}

// Run test 5 of the QA suite
func Test5_ComplexNOConcurrency(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer qa_cleanup(snapshotName)

	// Get some services
	bindings, err := cstestutils.CreateBindings(mainNode.GetServiceProvider())
	sp := testMgr.GetNode().GetServiceProvider()
	require.NoError(t, err)
	t.Log("Created bindings")

	// Create some subnodes
	nodes, _, err := createNodesForTest(t, 14, eth.EthToWei(50))
	require.NoError(t, err)

	// Make sure the contract state is clean
	runPreflightChecks(t, bindings)

	// Create salts
	salts := make([][]*big.Int, 15)
	for i := 0; i < 15; i++ {
		saltsPerNode := []*big.Int{
			big.NewInt(int64(i)),
		}
		salts[i] = saltsPerNode
	}

	// Get deposit amounts
	wethAmount, rplAmount := getDepositAmounts(t, bindings, sp, 10) // Enough for 10 minipools but no more

	// Deposit WETH to the WETH vault
	cstestutils.DepositToWethVault(t, testMgr, bindings.WethVault, bindings.Weth, wethAmount, deployerOpts)

	// Deposit RPL to the RPL vault
	cstestutils.DepositToRplVault(t, testMgr, bindings.RplVault, bindings.Rpl, rplAmount, deployerOpts)

	// Build the wave 1 minipool creation TXs
	wave1Nodes := nodes[:5]
	wave1Salts := salts[:5]
	_, wave1Hashes := cstestutils.BuildAndSubmitCreateMinipoolTxs(t, wave1Nodes, 1, wave1Salts, bindings.RpSuperNode)

	// Mine a block
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined a block")

	// Wave 1 should succeed
	hd := mainNode.GetHyperdriveNode().GetApiClient()
	for _, hashesPerNode := range wave1Hashes {
		_, err = hd.Tx.WaitForTransaction(hashesPerNode[0])
		require.NoError(t, err)
	}
	t.Log("First minipool creation wave succeeded")

	// Build the wave 2 minipool creation TXs
	wave2Nodes := nodes[5:10]
	wave2Salts := salts[5:10]
	_, wave2Hashes := cstestutils.BuildAndSubmitCreateMinipoolTxs(t, wave2Nodes, 1, wave2Salts, bindings.RpSuperNode)

	// Mine a block
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined a block")

	// Wave 2 should succeed
	for _, hashesPerNode := range wave2Hashes {
		_, err = hd.Tx.WaitForTransaction(hashesPerNode[0])
		require.NoError(t, err)
	}
	t.Log("Second minipool creation wave succeeded")

	// Attempt to build the wave 3 minipool creation TXs - they should all fail
	wave3Nodes := nodes[10:15]
	wave3Salts := salts[10:15]
	for i, node := range wave3Nodes {
		cs := node.GetApiClient()
		salt := wave3Salts[i][0]
		depositResponse, err := cs.Minipool.Create(salt)
		require.NoError(t, err)
		require.False(t, depositResponse.Data.CanCreate)
		require.True(t, depositResponse.Data.InsufficientLiquidity)
		t.Logf("Node %d correctly reported insufficient liquidity", i+10)
	}
	t.Log("Third minipool creation wave failed as expected")
}

// Run test 13 of the QA suite
func Test13_OrderlyStressTest(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer qa_cleanup(snapshotName)

	// Get some services
	bindings, err := cstestutils.CreateBindings(mainNode.GetServiceProvider())
	require.NoError(t, err)
	sp := testMgr.GetNode().GetServiceProvider()
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()
	txMgr := sp.GetTransactionManager()
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	t.Log("Created bindings")

	// Disable the ETH/RPL ratio enforcement
	minRatio := common.Big0
	maxRatio := eth.EthToWei(100000)
	setCoverageRatios(t, sp, minRatio, maxRatio)

	// Set the liquidity reserves
	tenPercent := eth.EthToWei(0.1) // 10%
	setLiquidityReserveRatios(t, sp, tenPercent, tenPercent)

	// Get the current RPL price
	var rplPerEth *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.PriceFetcher.GetRplPrice(mc, &rplPerEth)
		return nil
	}, nil)
	require.NoError(t, err)

	// Deposit into the WETH Vault
	ethDepositAmount := eth.EthToWei(1000)
	cstestutils.DepositToWethVault(t, testMgr, bindings.WethVault, bindings.Weth, ethDepositAmount, deployerOpts)

	// Deposit into the RPL Vault
	oneEth := big.NewInt(1e18)
	twentyPercent := big.NewInt(2e17) // 20%
	rplDepositAmount := new(big.Int).Mul(ethDepositAmount, rplPerEth)
	rplDepositAmount.Mul(rplDepositAmount, twentyPercent)
	rplDepositAmount.Div(rplDepositAmount, oneEth)
	rplDepositAmount.Div(rplDepositAmount, oneEth)
	cstestutils.DepositToRplVault(t, testMgr, bindings.RplVault, bindings.Rpl, rplDepositAmount, deployerOpts)

	// Set the nodeset timestamp
	nodesetTime := time.Now()
	nsMgr.SetManualSignatureTimestamp(&nodesetTime)
	t.Logf("Set the nodeset timestamp to %s", nodesetTime)

	// Create some subnodes
	nodes, _, err := createNodesForTest(t, 2, eth.EthToWei(50))
	require.NoError(t, err)

	// Set max minipools per node
	wave1MinipoolsPerNode := 4
	txInfo, err := csMgr.SuperNodeAccount.SetMaxValidators(big.NewInt(int64(wave1MinipoolsPerNode)), deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, deployerOpts, fmt.Sprintf("Set the max validators to %d", wave1MinipoolsPerNode))

	// Make the RP deposit pool way bigger to account for the minipool creation count
	depositPoolSize := eth.EthToWei(2000)
	txInfo, err = bindings.ProtocolDaoManager.Settings.Deposit.MaximumDepositPoolSize.Bootstrap(depositPoolSize, deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, deployerOpts, fmt.Sprintf("Set the maximum deposit pool size to %.2f ETH", eth.WeiToEth(depositPoolSize)))

	// Deposit into the RP deposit pool
	fundOpts := &bind.TransactOpts{
		From:  deployerOpts.From,
		Value: depositPoolSize,
	}
	fundTxInfo, err := bindings.DepositPoolManager.Deposit(fundOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, fundTxInfo, deployerOpts, "Funded the RP deposit pool")

	// Create minipools
	wave1Data, wave1CreateHashes := cstestutils.BuildAndSubmitCreateMinipoolTxs(t, nodes, wave1MinipoolsPerNode, nil, bindings.RpSuperNode)

	// Mine a block
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined a block")

	// Wave 1 creation should succeed
	hd := mainNode.GetHyperdriveNode().GetApiClient()
	for _, hashesPerNode := range wave1CreateHashes {
		for _, hash := range hashesPerNode {
			_, err = hd.Tx.WaitForTransaction(hash)
			require.NoError(t, err)
		}
	}
	t.Log("Wave 1 creation succeeded")

	// Verify minipools
	for i, dataForNode := range wave1Data {
		for j, data := range dataForNode {
			index := i*wave1MinipoolsPerNode + j
			_ = cstestutils.VerifyMinipoolAfterCreation(t, qMgr, bindings.RpSuperNode, uint64(index), data.MinipoolAddress, bindings.MinipoolManager)
		}
	}
	t.Log("Verified wave 1 minipools")

	// Redeem 10 xrETH
	xrEthRedeemAmount := eth.EthToWei(10)
	wethReturned := redeemToken(t, qMgr, txMgr, bindings.WethVault, xrEthRedeemAmount, false, deployerOpts)
	require.Equal(t, xrEthRedeemAmount, wethReturned)
	t.Logf("Redeemed %.6f xrETH (%s wei) for %.6f WETH (%s wei)", eth.WeiToEth(xrEthRedeemAmount), xrEthRedeemAmount.String(), eth.WeiToEth(wethReturned), wethReturned.String())

	// Redeem 100 xrRPL
	xRplRedeemAmount := eth.EthToWei(100)
	rplReturned := redeemToken(t, qMgr, txMgr, bindings.RplVault, xRplRedeemAmount, false, deployerOpts)
	require.Equal(t, xRplRedeemAmount, rplReturned)
	t.Logf("Redeemed %.6f xRPL (%s wei) for %.6f RPL (%s wei)", eth.WeiToEth(xRplRedeemAmount), xRplRedeemAmount.String(), eth.WeiToEth(rplReturned), rplReturned.String())

	// Fast forward 1 day
	secondsPerSlot := testMgr.GetBeaconMockManager().GetConfig().SecondsPerSlot
	seconds := uint64(24 * 60 * 60)
	secondsDuration := time.Duration(seconds) * time.Second
	slots := seconds / secondsPerSlot
	err = testMgr.AdvanceSlots(uint(slots), false)
	require.NoError(t, err)
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Fast forwarded 1 day")

	// Set the nodeset timestamp
	nodesetTime = nodesetTime.Add(secondsDuration)
	nsMgr.SetManualSignatureTimestamp(&nodesetTime)
	t.Logf("Set the nodeset timestamp to %s", nodesetTime)

	// Build wave 1 minipools stake TXs
	wave1StakeHashes := cstestutils.BuildAndSubmitStakeMinipoolTxs(t, nodes, wave1Data)

	// Mine a block
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined a block")

	// Wave 1 should succeed staking
	for _, hashesPerNode := range wave1StakeHashes {
		_, err = hd.Tx.WaitForTransaction(hashesPerNode[0])
		require.NoError(t, err)
	}
	t.Log("Wave 1 staking succeeded")

	// Fast forward 1 week
	seconds = uint64(24 * 60 * 60 * 7)
	secondsDuration = time.Duration(seconds) * time.Second
	slots = seconds / secondsPerSlot
	err = testMgr.AdvanceSlots(uint(slots), false)
	require.NoError(t, err)
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Fast forwarded 1 week")

	// Set the nodeset timestamp
	nodesetTime = nodesetTime.Add(secondsDuration)
	nsMgr.SetManualSignatureTimestamp(&nodesetTime)
	t.Logf("Set the nodeset timestamp to %s", nodesetTime)

	// Assume 0.010 ETH in rewards on Beacon and 0.005 on the EL per validator
	elRewardsPerMinipool := eth.EthToWei(0.005)
	beaconRewardsPerValidator := 1e7 // Beacon is in gwei
	simulateBeaconRewards(t, sp, wave1Data, elRewardsPerMinipool, uint64(beaconRewardsPerValidator), deployerOpts)
	totalYieldAccrued := calculateXrEthOracleTotalYieldAccrued(t, sp, bindings)
	t.Logf("The new total yield accrued to report is %.10f (%s wei)", eth.WeiToEth(totalYieldAccrued), totalYieldAccrued.String())

	// Update the oracle report
	chainID := new(big.Int).SetUint64(testMgr.GetBeaconMockManager().GetConfig().ChainID)
	sig, err := createXrEthOracleSignature(totalYieldAccrued, nodesetTime, csMgr.PoABeaconOracle.Address, chainID, deployerKey)
	require.NoError(t, err)
	txInfo, err = csMgr.PoABeaconOracle.SetTotalYieldAccrued(totalYieldAccrued, sig, nodesetTime, deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, deployerOpts, "Updated the xrETH Oracle")

	// Verify the new ETH:xrETH price
	originalAmount := new(big.Int).Sub(ethDepositAmount, wethReturned)
	numerator := new(big.Int).Add(originalAmount, totalYieldAccrued)
	numerator.Mul(numerator, oneEth)
	expectedRatio := new(big.Int).Div(numerator, originalAmount)
	xrEthPriceAccordingToVault := getTokenPrice(t, qMgr, csMgr.WethVault)
	requireApproxEqual(t, expectedRatio, xrEthPriceAccordingToVault)
	t.Logf("The new ETH:xrETH price according to the token is %.10f (%s wei)", eth.WeiToEth(xrEthPriceAccordingToVault), xrEthPriceAccordingToVault.String())

	// Run an RP rewards interval
	rewardsMap, rewardsSubmission := executeRpRewardsInterval(t, sp, bindings)

	// Do a merkle claim
	merkleCfg := createMerkleClaimConfig(t, sp, bindings, rewardsSubmission)
	constellationRewards := rewardsMap[csMgr.SuperNodeAccount.Address]
	txInfo, err = csMgr.SuperNodeAccount.MerkleClaim(
		[]*big.Int{rewardsSubmission.RewardIndex},
		[]*big.Int{constellationRewards.CollateralRpl},
		[]*big.Int{constellationRewards.SmoothingPoolEth},
		[][]common.Hash{constellationRewards.MerkleProof},
		merkleCfg,
		deployerOpts,
	)
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, deployerOpts, "Executed the Merkle claim")
	t.Logf("Rewards amount: %.6f ETH (%s wei), %.6f RPL (%s wei)",
		eth.WeiToEth(constellationRewards.SmoothingPoolEth),
		constellationRewards.SmoothingPoolEth.String(),
		eth.WeiToEth(constellationRewards.CollateralRpl),
		constellationRewards.CollateralRpl.String(),
	)

	// Get the split for RPL rewards
	originalAmount = new(big.Int).Sub(rplDepositAmount, rplReturned)
	nodeOpShareOfRplRewards := new(big.Int).Mul(constellationRewards.CollateralRpl, merkleCfg.AverageRplTreasuryFee)
	nodeOpShareOfRplRewards.Div(nodeOpShareOfRplRewards, oneEth)
	xRplShareOfRewards := new(big.Int).Sub(constellationRewards.CollateralRpl, nodeOpShareOfRplRewards)
	t.Logf("xRPL holders get %.6f RPL (%s wei), node ops get %.6f RPL (%s wei)",
		eth.WeiToEth(xRplShareOfRewards), xRplShareOfRewards.String(),
		eth.WeiToEth(nodeOpShareOfRplRewards), nodeOpShareOfRplRewards.String(),
	)

	// Verify the RPL:xRPL ratio
	numerator = new(big.Int).Add(originalAmount, xRplShareOfRewards)
	numerator.Mul(numerator, oneEth)
	expectedRatio = new(big.Int).Div(numerator, originalAmount)
	xRplPriceAccordingToVault := getTokenPrice(t, qMgr, csMgr.RplVault)
	requireApproxEqual(t, expectedRatio, xRplPriceAccordingToVault)
	t.Logf("The new RPL:xRPL price according to the token is %.10f (%s wei), which matches the expected value", eth.WeiToEth(xRplPriceAccordingToVault), xRplPriceAccordingToVault.String())

}

// Run test 15 of the QA suite
func Test15_StakingTest(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer qa_cleanup(snapshotName)

	// Get some services
	bindings, err := cstestutils.CreateBindings(mainNode.GetServiceProvider())
	require.NoError(t, err)
	sp := testMgr.GetNode().GetServiceProvider()
	qMgr := sp.GetQueryManager()
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	t.Log("Created bindings")

	// Set the nodeset timestamp
	nodesetTime := time.Now()
	nsMgr.SetManualSignatureTimestamp(&nodesetTime)
	t.Logf("Set the nodeset timestamp to %s", nodesetTime)

	// Create some subnodes
	nodes, _, err := createNodesForTest(t, 14, eth.EthToWei(1.1))
	require.NoError(t, err)

	// Make sure the contract state is clean
	runPreflightChecks(t, bindings)

	// Get the deposit amounts
	wethAmount, rplDepositAmount := getDepositAmounts(t, bindings, sp, 10) // Enough for 10 minipools

	// Deposit RPL to the RPL vault
	cstestutils.DepositToRplVault(t, testMgr, bindings.RplVault, bindings.Rpl, rplDepositAmount, deployerOpts)

	// Deposit WETH to the WETH vault
	cstestutils.DepositToWethVault(t, testMgr, bindings.WethVault, bindings.Weth, wethAmount, deployerOpts)

	// Create salts
	salts := make([][]*big.Int, 15)
	for i := 0; i < 15; i++ {
		saltsPerNode := []*big.Int{
			big.NewInt(int64(i)),
		}
		salts[i] = saltsPerNode
	}

	// Build the wave 1 minipool creation TXs
	wave1Nodes := nodes[:5]
	wave1Salts := salts[:5]
	wave1Data, wave1CreateHashes := cstestutils.BuildAndSubmitCreateMinipoolTxs(t, wave1Nodes, 1, wave1Salts, bindings.RpSuperNode)

	// Mine a block
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined a block")

	// Wave 1 creation should succeed
	hd := mainNode.GetHyperdriveNode().GetApiClient()
	for _, hashesPerNode := range wave1CreateHashes {
		_, err = hd.Tx.WaitForTransaction(hashesPerNode[0])
		require.NoError(t, err)
	}
	t.Log("Wave 1 creation succeeded")

	// Verify minipools
	for i, dataForNode := range wave1Data {
		data := dataForNode[0]
		_ = cstestutils.VerifyMinipoolAfterCreation(t, qMgr, bindings.RpSuperNode, uint64(i), data.MinipoolAddress, bindings.MinipoolManager)
	}
	t.Log("Verified wave 1 minipools")

	// Fast forward 1 day
	secondsPerSlot := testMgr.GetBeaconMockManager().GetConfig().SecondsPerSlot
	seconds := uint64(24 * 60 * 60)
	secondsDuration := time.Duration(seconds) * time.Second
	slots := seconds / secondsPerSlot
	err = testMgr.AdvanceSlots(uint(slots), false)
	require.NoError(t, err)
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined a block")

	// Set the nodeset timestamp
	nodesetTime = nodesetTime.Add(secondsDuration)
	nsMgr.SetManualSignatureTimestamp(&nodesetTime)
	t.Logf("Set the nodeset timestamp to %s", nodesetTime)

	// Send ETH to the RP deposit pool again
	err = qMgr.Query(nil, nil, bindings.DepositPoolManager.Balance)
	require.NoError(t, err)
	t.Logf("RP deposit pool balance: %.2f ETH", eth.WeiToEth(bindings.DepositPoolManager.Balance.Get()))
	fundOpts := &bind.TransactOpts{
		From:  deployerOpts.From,
		Value: eth.EthToWei(120),
	}
	fundTxInfo, err := bindings.DepositPoolManager.Deposit(fundOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, fundTxInfo, deployerOpts, "Funded the RP deposit pool again")

	// Build wave 1 minipools stake TXs
	wave1StakeHashes := cstestutils.BuildAndSubmitStakeMinipoolTxs(t, wave1Nodes, wave1Data)

	// Build the wave 2 minipool creation TXs
	wave2Nodes := nodes[5:10]
	wave2Salts := salts[5:10]
	wave2Data, wave2CreationHashes := cstestutils.BuildAndSubmitCreateMinipoolTxs(t, wave2Nodes, 1, wave2Salts, bindings.RpSuperNode)

	// Mine a block
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined a block")

	// Wave 1 staking should succeed
	for _, hashesPerNode := range wave1StakeHashes {
		_, err = hd.Tx.WaitForTransaction(hashesPerNode[0])
		require.NoError(t, err)
	}
	t.Log("Wave 1 staking succeeded")

	// Wave 2 creation should succeed
	for _, hashesPerNode := range wave2CreationHashes {
		_, err = hd.Tx.WaitForTransaction(hashesPerNode[0])
		require.NoError(t, err)
	}
	t.Log("Wave 2 creation succeeded")

	// Verify minipools
	for i, dataForNode := range wave2Data {
		data := dataForNode[0]
		_ = cstestutils.VerifyMinipoolAfterCreation(t, qMgr, bindings.RpSuperNode, uint64(i+len(wave1Data)), data.MinipoolAddress, bindings.MinipoolManager)
	}
	t.Log("Verified wave 2 minipools")

	// Fast forward 1 day
	err = testMgr.AdvanceSlots(uint(slots), false)
	require.NoError(t, err)
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined a block")

	// Set the nodeset timestamp
	nodesetTime = nodesetTime.Add(secondsDuration)
	nsMgr.SetManualSignatureTimestamp(&nodesetTime)
	t.Logf("Set the nodeset timestamp to %s", nodesetTime)

	// Send ETH to the RP deposit pool again
	fundTxInfo, err = bindings.DepositPoolManager.Deposit(fundOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, fundTxInfo, deployerOpts, "Funded the RP deposit pool again")

	// Build wave 2 minipools stake TXs
	wave2StakeHashes := cstestutils.BuildAndSubmitStakeMinipoolTxs(t, wave2Nodes, wave2Data)

	// Mine a block
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined a block")

	// Wave 2 should succeed staking
	for _, hashesPerNode := range wave2StakeHashes {
		_, err = hd.Tx.WaitForTransaction(hashesPerNode[0])
		require.NoError(t, err)
	}
	t.Log("Wave 2 staking succeeded")

	// Attempt to build the wave 3 minipool creation TXs - they should all fail
	wave3Nodes := nodes[10:15]
	wave3Salts := salts[10:15]
	for i, node := range wave3Nodes {
		cs := node.GetApiClient()
		salt := wave3Salts[i][0]
		depositResponse, err := cs.Minipool.Create(salt)
		require.NoError(t, err)
		require.False(t, depositResponse.Data.CanCreate)
		require.True(t, depositResponse.Data.InsufficientLiquidity)
		t.Logf("Node %d correctly reported insufficient liquidity", i+10)
	}
	t.Log("Third minipool creation wave failed as expected")
}

// Do some initial sanity checks on the state of Constellation before running a test
// Also sends ETH to the RP deposit pool for convenience
func runPreflightChecks(t *testing.T, bindings *cstestutils.ContractBindings) {
	// Services
	sp := mainNode.GetServiceProvider()
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()

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
		bindings.DepositPoolManager.Balance,
		bindings.ProtocolDaoManager.Settings.Deposit.MaximumDepositPoolSize,
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
	testMgr.MineTx(t, txInfo, deployerOpts, fmt.Sprintf("Funded the RP deposit pool with %.6f ETH (%s wei)", eth.WeiToEth(fundOpts.Value), fundOpts.Value.String()))
}

// Create a set of subnodes running HD and CS, register them with the nodeset, and send them some ETH.
// Returns a collection of all nodes, including the original main node and the subnodes that were created.
func createNodesForTest(t *testing.T, subnodeCount int, initialFunding *big.Int) ([]*cstesting.ConstellationNode, []common.Address, error) {
	// Make the subnodes
	basePath := testMgr.GetTestDir()
	subNodes := make([]*cstesting.ConstellationNode, subnodeCount)
	subnodeAddresses := make([]common.Address, subnodeCount)
	for i := 0; i < subnodeCount; i++ {
		var err error
		nodeDir := filepath.Join(basePath, fmt.Sprintf("node%d", i+1))
		subNodes[i], subnodeAddresses[i], err = createNewNode(mainNode, nodeDir)
		require.NoError(t, err)
		logger.Info(
			"Created subnode",
			slog.Int("index", i+1),
			slog.String("address", subnodeAddresses[i].Hex()),
		)
	}

	// Send the subnodes some ETH
	hd := mainNode.GetHyperdriveNode().GetApiClient()
	submissions := make([]*eth.TransactionSubmission, len(subnodeAddresses))
	for i, addr := range subnodeAddresses {
		resp, err := hd.Wallet.Send(initialFunding, "eth", addr)
		require.NoError(t, err)
		if !resp.Data.CanSend {
			fail("can't send ETH to subnode %d: insufficient balance", i)
		}
		submission, _ := eth.CreateTxSubmissionFromInfo(resp.Data.TxInfo, nil)
		submissions[i] = submission
	}
	submitResp, err := hd.Tx.SubmitTxBatch(submissions, nil, eth.GweiToWei(10), eth.GweiToWei(0.5))
	require.NoError(t, err)

	// Mine the block
	err = testMgr.CommitBlock()
	require.NoError(t, err)

	// Wait for the transactions to be mined
	for _, hash := range submitResp.Data.TxHashes {
		_, err = hd.Tx.WaitForTransaction(hash)
		require.NoError(t, err)
	}

	// Amend the main node to the subnodes
	nodes := append([]*cstesting.ConstellationNode{mainNode}, subNodes...)
	addresses := append([]common.Address{mainNodeAddress}, subnodeAddresses...)

	// Register the nodes with Constellation
	for _, node := range nodes {
		cstestutils.RegisterWithConstellation(t, testMgr, node)
	}
	return nodes, addresses, nil
}

// Get the amount of ETH and RPL to deposit into the WETH and RPL vaults respectively in order to launch the given number of minipools
func getDepositAmounts(t *testing.T, bindings *cstestutils.ContractBindings, sp cscommon.IConstellationServiceProvider, minipoolCount int) (*big.Int, *big.Int) {
	// Get some services
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()
	countBig := big.NewInt(int64(minipoolCount))

	// Query some details
	var rplPerEth *big.Int
	var minipoolBond *big.Int
	var ethReserveRatio *big.Int
	var rplReserveRatio *big.Int
	err := qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.PriceFetcher.GetRplPrice(mc, &rplPerEth)
		csMgr.SuperNodeAccount.Bond(mc, &minipoolBond)
		csMgr.WethVault.GetLiquidityReserveRatio(mc, &ethReserveRatio)
		csMgr.RplVault.GetLiquidityReserveRatio(mc, &rplReserveRatio)
		return nil
	}, nil,
		bindings.RpSuperNode.RplStake,
		bindings.RpSuperNode.EthMatched,
		bindings.MinipoolManager.LaunchBalance,
	)
	require.NoError(t, err)

	// Get the total ETH bond and borrow amounts
	launchRequirement := bindings.MinipoolManager.LaunchBalance.Get()
	totalEthBond := new(big.Int).Mul(minipoolBond, countBig)
	totalEthBorrow := new(big.Int).Sub(launchRequirement, minipoolBond)
	totalEthBorrow.Mul(totalEthBorrow, countBig)
	t.Logf("Calculating RPL shortfall for %d minipools with %.2f ETH bond and %.2f ETH borrow", minipoolCount, eth.WeiToEth(totalEthBond), eth.WeiToEth(totalEthBorrow))

	// Get the RPL requirement
	var rplShortfall *big.Int
	totalEthMatched := bindings.RpSuperNode.EthMatched.Get()
	ethAmount := new(big.Int).Add(totalEthMatched, totalEthBorrow)
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.OperatorDistributor.CalculateRplStakeShortfall(mc, &rplShortfall, bindings.RpSuperNode.RplStake.Get(), ethAmount)
		return nil
	}, nil)
	require.NoError(t, err)
	t.Logf("RPL shortfall is %.6f RPL (%s wei)", eth.WeiToEth(rplShortfall), rplShortfall.String())

	// Fix the ETH amount based on the liquidity reserve
	collateralBase := big.NewInt(1e18)
	ethCollateral := new(big.Int).Sub(collateralBase, ethReserveRatio)
	ethDepositRequirement := new(big.Int).Mul(totalEthBond, collateralBase)
	ethDepositRequirement.Div(ethDepositRequirement, ethCollateral)
	ethDepositRequirement.Add(ethDepositRequirement, common.Big1)

	// Fix the RPL amount based on the liquidity reserve
	rplCollateral := new(big.Int).Sub(collateralBase, rplReserveRatio)
	rplDepositRequirement := new(big.Int).Mul(rplShortfall, collateralBase)
	rplDepositRequirement.Div(rplDepositRequirement, rplCollateral)
	rplDepositRequirement.Add(rplDepositRequirement, common.Big1)

	t.Logf("Total deposit requirements are %.2f ETH (%s wei) and %.6f RPL (%s wei)", eth.WeiToEth(ethDepositRequirement), ethDepositRequirement.String(), eth.WeiToEth(rplDepositRequirement), rplDepositRequirement.String())
	return ethDepositRequirement, rplDepositRequirement
}

// Set the min and max WETH:RPL ratios on the WETH and RPL vaults. Nil means don't change the setting for that vault.
func setCoverageRatios(t *testing.T, sp cscommon.IConstellationServiceProvider, minWethRplRatio *big.Int, maxWethRplRatio *big.Int) {
	// Services
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()
	txMgr := sp.GetTransactionManager()

	submissions := []*eth.TransactionSubmission{}
	if minWethRplRatio != nil {
		txInfo, err := csMgr.RplVault.SetMinWethRplRatio(minWethRplRatio, deployerOpts)
		submission, err := eth.CreateTxSubmissionFromInfo(txInfo, err)
		require.NoError(t, err)
		submissions = append(submissions, submission)
	}
	if maxWethRplRatio != nil {
		txInfo, err := csMgr.WethVault.SetMaxWethRplRatio(maxWethRplRatio, deployerOpts)
		submission, err := eth.CreateTxSubmissionFromInfo(txInfo, err)
		require.NoError(t, err)
		submissions = append(submissions, submission)
	}

	// Submit the transactions
	txs, err := txMgr.BatchExecuteTransactions(submissions, deployerOpts)
	require.NoError(t, err)

	// Mine the block
	err = testMgr.CommitBlock()
	require.NoError(t, err)

	// Wait for the transactions to be mined
	err = txMgr.WaitForTransactions(txs)
	require.NoError(t, err)

	// Verify the settings
	var newMin *big.Int
	var newMax *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		if minWethRplRatio != nil {
			csMgr.RplVault.GetMinWethRplRatio(mc, &newMin)
		}
		if maxWethRplRatio != nil {
			csMgr.WethVault.GetMaxWethRplRatio(mc, &newMax)
		}
		return nil
	}, nil)
	require.NoError(t, err)

	// Log the results
	msg := "Set the WETH:RPL coverage ratios"
	if minWethRplRatio != nil {
		require.Zero(t, minWethRplRatio.Cmp(newMin))
		msg += fmt.Sprintf("; Min: %.6f (%s wei),", eth.WeiToEth(newMin), newMin.String())
	}
	if maxWethRplRatio != nil {
		require.Zero(t, maxWethRplRatio.Cmp(newMax))
		msg += fmt.Sprintf("; Max: %.6f (%s wei)", eth.WeiToEth(newMax), newMax.String())
	}
	t.Log(msg)
}

// Set the liquidity reserve ratios on the WETH and RPL vaults. Nil means don't change the setting for that vault.
func setLiquidityReserveRatios(t *testing.T, sp cscommon.IConstellationServiceProvider, wethVault *big.Int, rplVault *big.Int) {
	// Services
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()
	txMgr := sp.GetTransactionManager()

	submissions := []*eth.TransactionSubmission{}
	if wethVault != nil {
		txInfo, err := csMgr.WethVault.SetLiquidityReserveRatio(wethVault, deployerOpts)
		submission, err := eth.CreateTxSubmissionFromInfo(txInfo, err)
		require.NoError(t, err)
		submissions = append(submissions, submission)
	}
	if rplVault != nil {
		txInfo, err := csMgr.RplVault.SetLiquidityReserveRatio(rplVault, deployerOpts)
		submission, err := eth.CreateTxSubmissionFromInfo(txInfo, err)
		require.NoError(t, err)
		submissions = append(submissions, submission)
	}

	// Submit the transactions
	txs, err := txMgr.BatchExecuteTransactions(submissions, deployerOpts)
	require.NoError(t, err)

	// Mine the block
	err = testMgr.CommitBlock()
	require.NoError(t, err)

	// Wait for the transactions to be mined
	err = txMgr.WaitForTransactions(txs)
	require.NoError(t, err)

	// Verify the settings
	var newWethVaultSetting *big.Int
	var newRplVaultSetting *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		if wethVault != nil {
			csMgr.WethVault.GetLiquidityReserveRatio(mc, &newWethVaultSetting)
		}
		if rplVault != nil {
			csMgr.RplVault.GetLiquidityReserveRatio(mc, &newRplVaultSetting)
		}
		return nil
	}, nil)
	require.NoError(t, err)

	// Log the results
	msg := "Set the liquidity reserve ratios"
	if wethVault != nil {
		require.Zero(t, wethVault.Cmp(newWethVaultSetting))
		msg += fmt.Sprintf("; WETH: %.6f (%s wei),", eth.WeiToEth(newWethVaultSetting), newWethVaultSetting.String())
	}
	if rplVault != nil {
		require.Zero(t, rplVault.Cmp(newRplVaultSetting))
		msg += fmt.Sprintf("; RPL: %.6f (%s wei),", eth.WeiToEth(newRplVaultSetting), newRplVaultSetting.String())
	}
	t.Log(msg)
}

// Redeems an ERC4626 token for the underlying asset and returns the amount of the asset redeemed
func redeemToken(t *testing.T, qMgr *eth.QueryManager, txMgr *eth.TransactionManager, token contracts.IErc4626Token, amount *big.Int, humanReadable bool, opts *bind.TransactOpts) *big.Int {
	// Get the amount of the underlying asset before redeeming
	asset := token.Asset()
	var beforeBalance *big.Int
	err := qMgr.Query(func(mc *batch.MultiCaller) error {
		asset.BalanceOf(mc, &beforeBalance, opts.From)
		return nil
	}, nil)
	require.NoError(t, err)

	// Make the TX
	if humanReadable {
		decimals := token.Decimals()
		offset := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
		amount.Mul(amount, offset) // Convert to the native share count
		t.Logf("Redemption calculated as %.6f token (%s wei)", eth.WeiToEth(amount), amount.String())
	}
	txInfo, err := token.Redeem(amount, opts.From, opts.From, opts)
	require.NoError(t, err)

	// Submit the TX
	submitOpts := &bind.TransactOpts{
		From:     opts.From,
		Signer:   opts.Signer,
		Value:    txInfo.Value,
		GasLimit: txInfo.SimulationResult.SafeGasLimit,
		Context:  opts.Context,
	}
	tx, err := txMgr.ExecuteTransaction(txInfo, submitOpts)
	require.NoError(t, err)

	// Mine the block
	err = testMgr.CommitBlock()
	require.NoError(t, err)

	// Wait for the transaction to be mined
	err = txMgr.WaitForTransaction(tx)
	require.NoError(t, err)

	// Get the amount of the underlying asset after redeeming
	var afterBalance *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		asset.BalanceOf(mc, &afterBalance, opts.From)
		return nil
	}, nil)
	require.NoError(t, err)

	// Return the amount redeemed
	redeemedAmount := new(big.Int).Sub(afterBalance, beforeBalance)
	return redeemedAmount
}

// Get the price of the token, in terms of the asset : token ratio
func getTokenPrice(t *testing.T, qMgr *eth.QueryManager, token contracts.IErc4626Token) *big.Int {
	// Make the TX
	decimals := token.Decimals()
	shares := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil) // Amount of 1 token
	var assetsPerShare *big.Int
	err := qMgr.Query(func(mc *batch.MultiCaller) error {
		token.ConvertToAssets(mc, &assetsPerShare, shares)
		return nil
	}, nil)
	require.NoError(t, err)
	return assetsPerShare
}

// Make a signature for xrETH oracle updates
func createXrEthOracleSignature(newTotalYieldAccrued *big.Int, timestamp time.Time, poaBeaconOracleAddress common.Address, chainID *big.Int, key *ecdsa.PrivateKey) ([]byte, error) {
	amountBytes := [32]byte{}
	newTotalYieldAccrued.FillBytes(amountBytes[:])

	timestampBig := big.NewInt(timestamp.Unix())
	timestampBytes := [32]byte{}
	timestampBig.FillBytes(timestampBytes[:])

	chainIdBytes := [32]byte{}
	chainID.FillBytes(chainIdBytes[:])

	message := crypto.Keccak256(
		amountBytes[:],
		timestampBytes[:],
		poaBeaconOracleAddress[:],
		chainIdBytes[:],
	)

	// Sign the message
	signature, err := utils.CreateSignature(message, key)
	if err != nil {
		return nil, fmt.Errorf("error creating signature: %w", err)
	}
	return signature, nil
}

// Simulate rewards being earned on the Beacon Chain. If the validators don't exist, they're created and put into the `active_staking` status for convenience.
func simulateBeaconRewards(t *testing.T, sp cscommon.IConstellationServiceProvider, minipools [][]*csapi.MinipoolCreateData, elAmount *big.Int, beaconAmount uint64, opts *bind.TransactOpts) {
	// Services
	txMgr := sp.GetTransactionManager()
	opts.Nonce = nil

	// Send ETH to each minipool on the EL
	sendOpts := &bind.TransactOpts{
		From:  opts.From,
		Value: elAmount,
	}
	submissions := []*eth.TransactionSubmission{}
	for _, mpsForNode := range minipools {
		for _, mp := range mpsForNode {
			txInfo := txMgr.CreateTransactionInfoRaw(mp.MinipoolAddress, nil, sendOpts)
			submission, _ := eth.CreateTxSubmissionFromInfo(txInfo, nil)
			submissions = append(submissions, submission)
		}
	}
	txs, err := txMgr.BatchExecuteTransactions(submissions, opts)
	require.NoError(t, err)

	// Mine the block
	err = testMgr.CommitBlock()
	require.NoError(t, err)

	// Wait for the transactions to be mined
	err = txMgr.WaitForTransactions(txs)
	require.NoError(t, err)
	t.Logf("Sent %.4f ETH to %d minipools on the EL", eth.WeiToEth(elAmount), len(submissions))

	// "Mint" ETH on the Beacon Chain
	bm := testMgr.GetBeaconMockManager()
	for _, mpsForNode := range minipools {
		for _, mp := range mpsForNode {
			pubkey := mp.ValidatorPubkey
			val, err := bm.GetValidator(pubkey.Hex())
			require.NoError(t, err)
			if val == nil {
				withdrawalCreds := validator.GetWithdrawalCredsFromAddress(mp.MinipoolAddress)
				val, err = bm.AddValidator(pubkey, withdrawalCreds)
				require.NoError(t, err)
				val.Status = beacon.ValidatorState_ActiveOngoing
				val.Balance = 32e9 // 32 ETH
			}
			val.Balance += beaconAmount
		}
	}
	t.Logf("Added %.4f ETH to %d validators on the Beacon Chain", eth.GweiToEth(float64(beaconAmount)), len(submissions))
}

// Reference implementation for the xrETH oracle
func calculateXrEthOracleTotalYieldAccrued(t *testing.T, sp cscommon.IConstellationServiceProvider, bindings *cstestutils.ContractBindings) *big.Int {
	// Services
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()

	// Get the total minipool count and minipool launch balance
	var minipoolCountBig *big.Int
	err := qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.SuperNodeAccount.GetMinipoolCount(mc, &minipoolCountBig)
		return nil
	}, nil)
	require.NoError(t, err)
	minipoolCount := int(minipoolCountBig.Uint64())

	// Get the minipool addresses
	addressBatchSize := 1000
	addresses := make([]common.Address, minipoolCount)
	err = qMgr.BatchQuery(minipoolCount, addressBatchSize, func(mc *batch.MultiCaller, index int) error {
		indexBig := big.NewInt(int64(index))
		csMgr.SuperNodeAccount.GetMinipoolAddress(mc, &addresses[index], indexBig)
		return nil
	}, nil)
	require.NoError(t, err)

	type ConstellationMinipool struct {
		RocketPoolBinding minipool.IMinipool
		ConstellationData constellation.MinipoolData
	}

	// Get the RP minipool bindings
	rpMinipools, err := bindings.MinipoolManager.CreateMinipoolsFromAddresses(addresses, false, nil)
	require.NoError(t, err)

	// Get the RP minipool details and CS details
	detailsBatchSize := 100
	csMinipools := make([]ConstellationMinipool, minipoolCount)
	err = qMgr.BatchQuery(minipoolCount, detailsBatchSize, func(mc *batch.MultiCaller, index int) error {
		rpMinipool := rpMinipools[index]
		csMinipools[index].RocketPoolBinding = rpMinipool
		mpCommon := rpMinipool.Common()
		eth.AddQueryablesToMulticall(mc,
			mpCommon.Status,
			mpCommon.Pubkey,
			mpCommon.IsFinalised,
			mpCommon.NodeDepositBalance,
			mpCommon.NodeRefundBalance,
		)
		csMgr.SuperNodeAccount.GetMinipoolData(mc, &csMinipools[index].ConstellationData, mpCommon.Address)
		return nil
	}, nil)
	require.NoError(t, err)

	// Filter by minipool status
	candidateMinipools := make([]*ConstellationMinipool, 0, minipoolCount)
	for i, mp := range csMinipools {
		rpMinipool := mp.RocketPoolBinding
		mpCommon := rpMinipool.Common()
		if mpCommon.IsFinalised.Get() {
			continue
		}
		if mpCommon.Status.Formatted() != types.MinipoolStatus_Staking {
			continue
		}
		candidateMinipools = append(candidateMinipools, &csMinipools[i])
	}

	// Get the Beacon statuses for each validator
	pubkeys := make([]beacon.ValidatorPubkey, len(candidateMinipools))
	for i, mp := range candidateMinipools {
		mpCommon := mp.RocketPoolBinding.Common()
		pubkey := mpCommon.Pubkey.Get()
		pubkeys[i] = pubkey
	}
	bn := sp.GetBeaconClient()
	beaconStatuses, err := bn.GetValidatorStatuses(context.Background(), pubkeys, nil)
	require.NoError(t, err)

	// Filter by Beacon status
	activeMinipools := []*ConstellationMinipool{}
	for i, mp := range candidateMinipools {
		mpCommon := mp.RocketPoolBinding.Common()
		pubkey := mpCommon.Pubkey.Get()
		beaconStatus, exists := beaconStatuses[pubkey]
		if !exists {
			continue
		}
		if beaconStatus.Status == beacon.ValidatorState_PendingInitialized {
			continue
		}
		activeMinipools = append(activeMinipools, candidateMinipools[i])
	}

	// Get the EL balances
	bb, err := batch.NewBalanceBatcher(sp.GetEthClient(), sp.GetResources().BalanceBatcherAddress, 1000, 2)
	require.NoError(t, err)
	activeCount := len(activeMinipools)
	activeAddresses := make([]common.Address, activeCount)
	for i, mp := range activeMinipools {
		activeAddresses[i] = mp.RocketPoolBinding.Common().Address
	}
	activeBalances, err := bb.GetEthBalances(activeAddresses, nil)
	require.NoError(t, err)

	// Get the total balance for the minipool, minus the RP node refund
	oneGwei := big.NewInt(1e9)
	mpBalances := make([]*big.Int, activeCount)
	for i, mp := range activeMinipools {
		mpCommon := mp.RocketPoolBinding.Common()
		pubkey := mpCommon.Pubkey.Get()

		// Get the aggregated balance
		elBalance := activeBalances[i]
		beaconBalance := beaconStatuses[pubkey].Balance
		beaconBalanceWei := new(big.Int).SetUint64(beaconBalance)
		beaconBalanceWei.Mul(beaconBalanceWei, oneGwei)
		mpBalance := new(big.Int).Add(elBalance, beaconBalanceWei)
		mpBalance.Sub(mpBalance, mpCommon.NodeRefundBalance.Get()) // Remove the node refund from the total balance
		mpBalances[i] = mpBalance
		t.Logf("MP %s has a total balance (minus refund) of %.6f ETH (%s wei)",
			mpCommon.Address.Hex(),
			eth.WeiToEth(mpBalance),
			mpBalance.String(),
		)
	}

	// Calculate the RP node op portions of the balances
	rpNodeShares := make([]*big.Int, activeCount)
	err = qMgr.BatchQuery(activeCount, 100, func(mc *batch.MultiCaller, i int) error {
		mp := activeMinipools[i]
		mpCommon := mp.RocketPoolBinding.Common()
		mpBalance := mpBalances[i]
		mpCommon.CalculateNodeShare(mc, &rpNodeShares[i], mpBalance)
		return nil
	}, nil)
	require.NoError(t, err)

	// Calculate the xrETH share of rewards
	oneEth := big.NewInt(1e18)
	totalRewards := big.NewInt(0)
	for i, mp := range activeMinipools {
		mpCommon := mp.RocketPoolBinding.Common()
		rpNodeShare := rpNodeShares[i]
		mpRewards := new(big.Int).Sub(rpNodeShare, mpCommon.NodeDepositBalance.Get())
		t.Logf("MP %s node share is %.6f ETH (%s wei), so rewards are %.6f ETH (%s wei)",
			mpCommon.Address.Hex(),
			eth.WeiToEth(rpNodeShare), rpNodeShare.String(),
			eth.WeiToEth(mpRewards), mpRewards.String(),
		)

		// Get the xrETH share of rewards and add it to the total
		fees := new(big.Int).Add(mp.ConstellationData.NodeFee, mp.ConstellationData.EthTreasuryFee)
		xrEthShare := new(big.Int).Sub(oneEth, fees)
		xrEthRewards := new(big.Int).Mul(mpRewards, xrEthShare)
		xrEthRewards.Div(xrEthRewards, oneEth)
		t.Logf("xrETH share is %.6f ETH (%s wei)", eth.WeiToEth(xrEthRewards), xrEthRewards.String())
		totalRewards.Add(totalRewards, xrEthRewards)
	}

	return totalRewards
}

// Sets a minipool to withdrawn on Beacon and sends the Beacon balance to it to simulate a full withdrawal
func setMinipoolToWithdrawn(t *testing.T, sp cscommon.IConstellationServiceProvider, minipool *csapi.MinipoolCreateData, opts *bind.TransactOpts) {
	// Services
	txMgr := sp.GetTransactionManager()
	bm := testMgr.GetBeaconMockManager()

	// Mark it as withdrawn on Beacon
	val, err := bm.GetValidator(minipool.ValidatorPubkey.Hex())
	require.NoError(t, err)
	require.NotNil(t, val)
	beaconBalance := val.Balance
	beaconBalanceWei := eth.GweiToWei(float64(beaconBalance))
	val.Balance = 0
	val.Status = beacon.ValidatorState_WithdrawalDone

	// Send the balance to the minipool to simulate a full withdrawal
	sendOpts := &bind.TransactOpts{
		From:  opts.From,
		Value: beaconBalanceWei,
	}
	txInfo := txMgr.CreateTransactionInfoRaw(minipool.MinipoolAddress, nil, sendOpts)
	testMgr.MineTx(t, txInfo, opts, fmt.Sprintf("Emulated a Beacon withdraw of %.6f ETH for minipool %s", eth.WeiToEth(beaconBalanceWei), minipool.MinipoolAddress.Hex()))
}

// Generates a rewards snapshot for the Rewards Pool
func executeRpRewardsInterval(t *testing.T, sp cscommon.IConstellationServiceProvider, bindings *cstestutils.ContractBindings) (map[common.Address]*rewardsInfo, rewards.RewardSubmission) {
	// Services
	ec := sp.GetEthClient()
	qMgr := sp.GetQueryManager()
	txMgr := sp.GetTransactionManager()
	csMgr := sp.GetConstellationManager()
	rplBinding := bindings.Rpl
	vault := bindings.RocketVault
	rewardsPool := bindings.RewardsPool
	smoothingPool := bindings.SmoothingPool

	// Query some initial settings
	var initialVaultRpl *big.Int
	var rewardsPercentages protocol.RplRewardsPercentages
	err := qMgr.Query(func(mc *batch.MultiCaller) error {
		rplBinding.BalanceOf(mc, &initialVaultRpl, vault.Address)
		bindings.ProtocolDaoManager.GetRewardsPercentages(mc, &rewardsPercentages)
		eth.AddQueryablesToMulticall(mc,
			rplBinding.InflationInterval,
			rplBinding.InflationIntervalStartTime,
			rewardsPool.RewardIndex,
			bindings.ProtocolDaoManager.Settings.Network.MaximumNodeFee,
			bindings.NodeManager.NodeCount,
		)
		return nil
	}, nil)
	if err != nil {
		t.Fatal(fmt.Errorf("error querying initial settings: %w", err))
	}

	// Fast forward to the RPL inflation time
	latestHeader, err := ec.HeaderByNumber(context.Background(), nil)
	require.NoError(t, err)
	currentTime := time.Unix(int64(latestHeader.Time), 0)
	timeUntilStart := rplBinding.InflationIntervalStartTime.Formatted().Sub(currentTime)
	timeToWait := timeUntilStart + rplBinding.InflationInterval.Formatted()
	secondsPerSlot := testMgr.GetBeaconMockManager().GetConfig().SecondsPerSlot
	slots := uint64(timeToWait.Seconds()) / secondsPerSlot
	err = testMgr.AdvanceSlots(uint(slots), false)
	require.NoError(t, err)
	t.Logf("Fast forwarded %d slots", slots)

	// Mint the RPL inflation
	txInfo, err := rplBinding.MintInflationRPL(odaoOpts[0])
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, odaoOpts[0], "Minted RPL inflation")

	// Make sure the vault has the new inflation
	var vaultRpl *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		rplBinding.BalanceOf(mc, &vaultRpl, vault.Address)
		return nil
	}, nil)
	require.NoError(t, err)
	rplInflationAmount := new(big.Int).Sub(vaultRpl, initialVaultRpl)
	require.Equal(t, 1, rplInflationAmount.Cmp(common.Big0))
	t.Logf("Inflation occurred, %.6f new RPL (%s wei) minted", eth.WeiToEth(rplInflationAmount), rplInflationAmount.String())

	// Send some ETH to the Smoothing Pool
	smoothingPoolEth := 10.0
	smoothingPoolEthWei := eth.EthToWei(smoothingPoolEth)
	sender := odaoOpts[0]
	newOpts := &bind.TransactOpts{
		From:  sender.From,
		Value: smoothingPoolEthWei,
	}
	txInfo = txMgr.CreateTransactionInfoRaw(smoothingPool.Address, nil, newOpts)
	testMgr.MineTx(t, txInfo, sender, fmt.Sprintf("Sent %.0f ETH to the Smoothing Pool", smoothingPoolEth))

	// Get some stats of the current state
	latestHeader, err = ec.HeaderByNumber(context.Background(), nil)
	require.NoError(t, err)

	// Get the RPL rewards for each category
	oneEth := big.NewInt(1e18)
	odaoAmount := new(big.Int).Mul(rplInflationAmount, rewardsPercentages.OdaoPercentage)
	odaoAmount.Div(odaoAmount, oneEth)
	odaoAmountPerNode := new(big.Int).Div(odaoAmount, big.NewInt(3))
	nodeAmount := new(big.Int).Mul(rplInflationAmount, rewardsPercentages.NodePercentage)
	nodeAmount.Div(nodeAmount, oneEth)
	pdaoAmount := new(big.Int).Sub(rplInflationAmount, odaoAmount)
	pdaoAmount.Sub(pdaoAmount, nodeAmount)

	// Get the node op share of the SP ETH
	halfSp := new(big.Int).Div(smoothingPoolEthWei, common.Big2)
	nodeCommission := new(big.Int).Mul(halfSp, bindings.ProtocolDaoManager.Settings.Network.MaximumNodeFee.Raw())
	nodeCommission.Div(nodeCommission, oneEth)
	nodeSpShare := new(big.Int).Add(halfSp, nodeCommission)
	userSpShare := new(big.Int).Sub(smoothingPoolEthWei, nodeSpShare)

	// Make the rewards map
	rewardsMap := map[common.Address]*rewardsInfo{
		odaoOpts[0].From: {
			CollateralRpl:    common.Big0,
			OracleDaoRpl:     odaoAmountPerNode,
			SmoothingPoolEth: common.Big0,
		},
		odaoOpts[1].From: {
			CollateralRpl:    common.Big0,
			OracleDaoRpl:     odaoAmountPerNode,
			SmoothingPoolEth: common.Big0,
		},
		odaoOpts[2].From: {
			CollateralRpl:    common.Big0,
			OracleDaoRpl:     odaoAmountPerNode,
			SmoothingPoolEth: common.Big0,
		},
		csMgr.SuperNodeAccount.Address: {
			CollateralRpl:    nodeAmount,
			OracleDaoRpl:     common.Big0,
			SmoothingPoolEth: nodeSpShare,
		},
	}

	// Create a new rewards snapshot
	oldInterval := rewardsPool.RewardIndex.Formatted()
	root, err := generateMerkleTree(rewardsMap)
	require.NoError(t, err)
	odaoRpl := big.NewInt(0)
	collateralRpl := big.NewInt(0)
	spEth := big.NewInt(0)
	for _, rewards := range rewardsMap {
		odaoRpl.Add(odaoRpl, rewards.OracleDaoRpl)
		collateralRpl.Add(collateralRpl, rewards.CollateralRpl)
		spEth.Add(spEth, rewards.SmoothingPoolEth)
	}
	rewardSnapshot := rewards.RewardSubmission{
		RewardIndex:     rewardsPool.RewardIndex.Raw(),
		ExecutionBlock:  latestHeader.Number,
		ConsensusBlock:  latestHeader.Number,
		MerkleRoot:      root,
		MerkleTreeCID:   "",
		IntervalsPassed: common.Big1,
		TreasuryRPL:     pdaoAmount,
		TrustedNodeRPL: []*big.Int{
			odaoRpl,
		},
		NodeRPL: []*big.Int{
			collateralRpl,
		},
		NodeETH: []*big.Int{
			spEth,
		},
		UserETH: userSpShare,
	}
	t.Log("Rewards submission created")

	// Submit it with 2 Oracles
	txInfo, err = rewardsPool.SubmitRewardSnapshot(rewardSnapshot, odaoOpts[0])
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, odaoOpts[0], "Submitted rewards snapshot from ODAO 1")
	txInfo, err = rewardsPool.SubmitRewardSnapshot(rewardSnapshot, odaoOpts[1])
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, odaoOpts[1], "Submitted rewards snapshot from ODAO 2")

	// Ensure the interval was incremented and the snapshot is canon
	err = qMgr.Query(nil, nil, rewardsPool.RewardIndex)
	require.NoError(t, err)
	interval := rewardsPool.RewardIndex.Formatted()
	require.NotEqual(t, oldInterval, interval)
	t.Logf("Interval incremented to %d successfully", interval)

	return rewardsMap, rewardSnapshot
}

type rewardsInfo struct {
	CollateralRpl    *big.Int
	OracleDaoRpl     *big.Int
	SmoothingPoolEth *big.Int
	MerkleData       []byte
	MerkleProof      []common.Hash
}

/*
func createRewardsMap(t *testing.T, sp cscommon.IConstellationServiceProvider, bindings *cstestutils.ContractBindings) map[common.Address]*rewardsInfo {
	// Services
	qMgr := sp.GetQueryManager()
	txMgr := sp.GetTransactionManager()
	rp := sp.GetRocketPoolManager().RocketPool
	rplBinding := bindings.Rpl
	vault := bindings.RocketVault
	rewardsPool := bindings.RewardsPool
	smoothingPool := bindings.SmoothingPool

	// Get the node count
	err := qMgr.Query(nil, nil,
		bindings.
			bindings.NodeManager.NodeCount,
		bindings.OracleDaoManager.MemberCount,
	)
	require.NoError(t, err)

	// Get the oDAO addresses
	odaoAddresses, err := bindings.OracleDaoManager.GetMemberAddresses(bindings.OracleDaoManager.MemberCount.Formatted(), nil)
	require.NoError(t, err)

	// Get the node addresses
	nodeCount := bindings.NodeManager.NodeCount.Formatted()
	nodeAddresses := make([]common.Address, nodeCount)
	err = qMgr.BatchQuery(int(nodeCount), 1000, func(mc *batch.MultiCaller, index int) error {
		bindings.NodeManager.GetNodeAddress(mc, &nodeAddresses[index], uint64(index))
		return nil
	}, nil)
	require.NoError(t, err)

	// Make node bindings
	nodes := make([]*node.Node, nodeCount)
	for i, address := range nodeAddresses {
		node, err := node.NewNode(rp, address)
		require.NoError(t, err)
		nodes[i] = node
	}

	// Get the list of minipools per node

}
*/

// Generates a Merkle tree for the given rewards map and creates the Merkle proofs for each claimer
func generateMerkleTree(rewards map[common.Address]*rewardsInfo) (common.Hash, error) {
	// Generate the leaf data for each claimer
	totalData := make([][]byte, 0, len(rewards))
	for address, rewardsForClaimer := range rewards {
		// Ignore claimers that didn't receive any rewards
		if rewardsForClaimer.CollateralRpl.Cmp(common.Big0) == 0 && rewardsForClaimer.OracleDaoRpl.Cmp(common.Big0) == 0 && rewardsForClaimer.SmoothingPoolEth.Cmp(common.Big0) == 0 {
			continue
		}

		// Claimer data is address[20] :: network[32] :: RPL[32] :: ETH[32]
		claimerData := make([]byte, 0, 20+32*3)

		// Claimer address
		addressBytes := address.Bytes()
		claimerData = append(claimerData, addressBytes...)

		// Claimer network
		network := big.NewInt(0)
		networkBytes := make([]byte, 32)
		network.FillBytes(networkBytes)
		claimerData = append(claimerData, networkBytes...)

		// RPL rewards
		rplRewards := big.NewInt(0)
		rplRewards.Add(rewardsForClaimer.CollateralRpl, rewardsForClaimer.OracleDaoRpl)
		rplRewardsBytes := make([]byte, 32)
		rplRewards.FillBytes(rplRewardsBytes)
		claimerData = append(claimerData, rplRewardsBytes...)

		// ETH rewards
		ethRewardsBytes := make([]byte, 32)
		rewardsForClaimer.SmoothingPoolEth.FillBytes(ethRewardsBytes)
		claimerData = append(claimerData, ethRewardsBytes...)

		// Assign it to the claimer rewards tracker and add it to the leaf data slice
		rewardsForClaimer.MerkleData = claimerData
		totalData = append(totalData, claimerData)
	}

	// Generate the tree
	tree, err := merkletree.NewUsing(totalData, keccak256.New(), false, true)
	if err != nil {
		return common.Hash{}, fmt.Errorf("error generating Merkle Tree: %w", err)
	}

	// Generate the proofs for each claimer
	for address, rewardsForClaimer := range rewards {
		// Get the proof
		proof, err := tree.GenerateProof(rewardsForClaimer.MerkleData, 0)
		if err != nil {
			return common.Hash{}, fmt.Errorf("error generating proof for claimer %s: %w", address.Hex(), err)
		}

		// Convert the proof into hex strings
		proofHashes := make([]common.Hash, len(proof.Hashes))
		for i, hash := range proof.Hashes {
			proofHashes[i] = common.BytesToHash(hash)
		}

		// Assign the proof hashes to the claimer rewards struct
		rewardsForClaimer.MerkleProof = proofHashes
	}

	merkleRoot := common.BytesToHash(tree.Root())
	return merkleRoot, nil
}

// Creates a Merkle claim config for the given rewards submission
func createMerkleClaimConfig(t *testing.T, sp cscommon.IConstellationServiceProvider, bindings *cstestutils.ContractBindings, intervalInfo rewards.RewardSubmission) *constellation.MerkleRewardsConfig {
	// Services
	csMgr := sp.GetConstellationManager()

	// Get the current time
	latestHeader, err := sp.GetEthClient().HeaderByNumber(context.Background(), nil)
	require.NoError(t, err)
	currentTimeBig := big.NewInt(int64(latestHeader.Time))

	ethTreasuryFee, nodeFee, rplTreasuryFee := getAvgFeesForBlock(t, sp, bindings, intervalInfo.ExecutionBlock.Uint64())

	avgEthTreasuryFeeBytes := [32]byte{}
	ethTreasuryFee.FillBytes(avgEthTreasuryFeeBytes[:])

	avgNodeFeeBytes := [32]byte{}
	nodeFee.FillBytes(avgNodeFeeBytes[:])

	avgRplTreasuryFeeBytes := [32]byte{}
	rplTreasuryFee.FillBytes(avgRplTreasuryFeeBytes[:])

	sigGenesisTimeBytes := [32]byte{}
	currentTimeBig.FillBytes(sigGenesisTimeBytes[:])

	nonceBytes := [32]byte{}

	chainIDBytes := [32]byte{}
	chainID := testMgr.GetBeaconMockManager().GetConfig().ChainID
	chainIDBig := big.NewInt(int64(chainID))
	chainIDBig.FillBytes(chainIDBytes[:])

	// Create the hash to sign
	message := crypto.Keccak256(
		avgEthTreasuryFeeBytes[:],
		avgNodeFeeBytes[:],
		avgRplTreasuryFeeBytes[:],
		sigGenesisTimeBytes[:],
		csMgr.SuperNodeAccount.Address[:],
		nonceBytes[:],
		chainIDBytes[:],
	)

	// Sign the message
	signature, err := utils.CreateSignature(message, deployerKey)
	require.NoError(t, err)

	return &constellation.MerkleRewardsConfig{
		Signature:             signature,
		SignatureGenesisTime:  currentTimeBig,
		AverageEthTreasuryFee: ethTreasuryFee,
		AverageEthOperatorFee: nodeFee,
		AverageRplTreasuryFee: rplTreasuryFee,
	}
}

// Gets the average fees for the eligible minipools at the end of a rewards interval
func getAvgFeesForBlock(t *testing.T, sp cscommon.IConstellationServiceProvider, bindings *cstestutils.ContractBindings, blockNumber uint64) (*big.Int, *big.Int, *big.Int) {
	// Services
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()
	opts := &bind.CallOpts{
		BlockNumber: new(big.Int).SetUint64(blockNumber),
	}

	// Get the total minipool count and minipool launch balance
	var minipoolCountBig *big.Int
	err := qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.SuperNodeAccount.GetMinipoolCount(mc, &minipoolCountBig)
		return nil
	}, opts)
	require.NoError(t, err)
	minipoolCount := int(minipoolCountBig.Uint64())

	// Get the minipool addresses
	addressBatchSize := 1000
	addresses := make([]common.Address, minipoolCount)
	err = qMgr.BatchQuery(minipoolCount, addressBatchSize, func(mc *batch.MultiCaller, index int) error {
		indexBig := big.NewInt(int64(index))
		csMgr.SuperNodeAccount.GetMinipoolAddress(mc, &addresses[index], indexBig)
		return nil
	}, opts)
	require.NoError(t, err)

	type ConstellationMinipool struct {
		RocketPoolBinding minipool.IMinipool
		ConstellationData constellation.MinipoolData
	}

	// Get the RP minipool bindings
	rpMinipools, err := bindings.MinipoolManager.CreateMinipoolsFromAddresses(addresses, false, nil)
	require.NoError(t, err)

	// Get the RP minipool details and CS details
	detailsBatchSize := 100
	csMinipools := make([]ConstellationMinipool, minipoolCount)
	err = qMgr.BatchQuery(minipoolCount, detailsBatchSize, func(mc *batch.MultiCaller, index int) error {
		rpMinipool := rpMinipools[index]
		csMinipools[index].RocketPoolBinding = rpMinipool
		mpCommon := rpMinipool.Common()
		eth.AddQueryablesToMulticall(mc,
			mpCommon.Status,
			mpCommon.Pubkey,
			mpCommon.IsFinalised,
		)
		csMgr.SuperNodeAccount.GetMinipoolData(mc, &csMinipools[index].ConstellationData, mpCommon.Address)
		return nil
	}, opts)
	require.NoError(t, err)

	// Filter by minipool status
	eligibleMinipools := make([]*ConstellationMinipool, 0, minipoolCount)
	for i, mp := range csMinipools {
		rpMinipool := mp.RocketPoolBinding
		mpCommon := rpMinipool.Common()
		if mpCommon.IsFinalised.Get() {
			continue
		}
		if mpCommon.Status.Formatted() != types.MinipoolStatus_Staking {
			continue
		}
		eligibleMinipools = append(eligibleMinipools, &csMinipools[i])
	}

	// Get the fees for each minipool
	ethTreasuryFee := big.NewInt(0)
	nodeFee := big.NewInt(0)
	rplTreasuryFee := big.NewInt(0)
	mpCount := big.NewInt(int64(len(eligibleMinipools)))
	for _, mp := range eligibleMinipools {
		ethTreasuryFee.Add(ethTreasuryFee, mp.ConstellationData.EthTreasuryFee)
		nodeFee.Add(nodeFee, mp.ConstellationData.NodeFee)
		rplTreasuryFee.Add(rplTreasuryFee, mp.ConstellationData.RplTreasuryFee)
	}

	// Return the averages
	ethTreasuryFee.Div(ethTreasuryFee, mpCount)
	nodeFee.Div(nodeFee, mpCount)
	rplTreasuryFee.Div(rplTreasuryFee, mpCount)
	return ethTreasuryFee, nodeFee, rplTreasuryFee
}

// Checks if two big.Ints are approximately equal within a small tolerance
func requireApproxEqual(t *testing.T, expected *big.Int, actual *big.Int) {
	t.Helper()
	delta := new(big.Int).Sub(expected, actual)
	delta = delta.Abs(delta)
	tolerance := big.NewInt(5) // 5 wei
	require.True(t, delta.Cmp(tolerance) <= 0, "delta is too high - expected %s, got %s (diff %s)", expected.String(), actual.String(), delta.String())
}

// Print information about the current tick
func printTickInfo(t *testing.T, sp cscommon.IConstellationServiceProvider) {
	if !shouldPrintTickInfo {
		return
	}

	// Services
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()

	var currentInterval *big.Int
	var nextMinipool common.Address
	err := qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.YieldDistributor.GetCurrentInterval(mc, &currentInterval)
		csMgr.OperatorDistributor.GetNextMinipool(mc, &nextMinipool)
		return nil
	}, nil)
	require.NoError(t, err)
	t.Logf("Current interval is %d, next minipool to tick is %s", currentInterval.Uint64(), nextMinipool.Hex())
}

// Cleanup after a unit test
func qa_cleanup(snapshotName string) {
	// Handle panics
	r := recover()
	if r != nil {
		debug.PrintStack()
		fail("Recovered from panic: %v", r)
	}

	// Revert to the snapshot taken at the start of the test
	if snapshotName != "" {
		err := testMgr.RevertToCustomSnapshot(snapshotName)
		if err != nil {
			fail("Error reverting to custom snapshot: %v", err)
		}
	}

	// Reload the HD wallet to undo any changes made during the test
	err := mainNode.GetHyperdriveNode().GetServiceProvider().GetWallet().Reload(testMgr.GetLogger())
	if err != nil {
		fail("Error reloading hyperdrive wallet: %v", err)
	}

	// Reload the CS wallet to undo any changes made during the test
	err = mainNode.GetServiceProvider().GetWallet().Reload()
	if err != nil {
		fail("Error reloading constellation wallet: %v", err)
	}
}
