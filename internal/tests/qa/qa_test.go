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
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
	"github.com/rocket-pool/rocketpool-go/v2/types"
	"github.com/stretchr/testify/require"
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
	t.Log("Created services")

	// Create some subnodes
	nodes, nodeAddresses, err := createNodesForTest(t, 4, eth.EthToWei(100))
	require.NoError(t, err)

	// Make sure the contract state is clean
	runPreflightChecks(t, bindings)

	// Deposit RPL to the RPL vault
	rplAmount := eth.EthToWei(4000)
	cstestutils.DepositToRplVault(t, testMgr, bindings.RplVault, bindings.Rpl, rplAmount, deployerOpts)

	// Deposit WETH to the WETH vault
	wethAmount := eth.EthToWei(100)
	cstestutils.DepositToWethVault(t, testMgr, bindings.WethVault, bindings.Weth, wethAmount, deployerOpts)

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

	// Get the scrub period
	err = qMgr.Query(nil, nil,
		bindings.OracleDaoManager.Settings.Minipool.ScrubPeriod,
	)
	require.NoError(t, err)

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

	// Submit 0.010 ETH in rewards on Beacon and 0.005 on the EL per validator
	elRewardsPerMinipool := eth.EthToWei(0.005)
	beaconRewardsPerValidator := 1e7 // Beacon is in gwei
	simulateBeaconRewards(t, sp, datas, elRewardsPerMinipool, uint64(beaconRewardsPerValidator), deployerOpts)
	totalYieldAccrued := calculateXrEthOracleTotalYieldAccrued(t, sp, bindings)
	t.Logf("The new total yield accrued to report is %.10f (%s wei)", eth.WeiToEth(totalYieldAccrued), totalYieldAccrued.String())

	// Update the oracle report
	chainID := new(big.Int).SetUint64(testMgr.GetBeaconMockManager().GetConfig().ChainID)
	newTime := time.Now().Add(timeToAdvance)
	sig, err := createXrEthOracleSignature(totalYieldAccrued, newTime, csMgr.XrEthAdminOracle.Address, chainID, deployerKey)
	require.NoError(t, err)
	txInfo, err := csMgr.XrEthAdminOracle.SetTotalYieldAccrued(totalYieldAccrued, sig, newTime, deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, deployerOpts, "Updated the xrETH Oracle")

	// Verify the new ETH:xrETH price
	oneEth := big.NewInt(1e18)
	numerator := new(big.Int).Add(wethAmount, totalYieldAccrued)
	numerator.Mul(numerator, oneEth)
	expectedRatio := new(big.Int).Div(numerator, wethAmount)
	expectedRatio.Sub(expectedRatio, common.Big1) // Compensate for rounding errors
	xrEthPriceAccordingToVault := getTokenPrice(t, qMgr, csMgr.WethVault)
	require.Equal(t, expectedRatio, xrEthPriceAccordingToVault)
	t.Logf("The new ETH:xrETH price according to the token is %.10f (%s wei)", eth.WeiToEth(xrEthPriceAccordingToVault), xrEthPriceAccordingToVault.String())

	// Redeem 5 xrETH
	xrEthRedeemAmount := eth.EthToWei(5) // big.NewInt(5) //xrEthInAccount
	wethReturned := redeemToken(t, qMgr, txMgr, bindings.WethVault, xrEthRedeemAmount, false, deployerOpts)
	expectedAmount := new(big.Int).Mul(xrEthRedeemAmount, xrEthPriceAccordingToVault)
	expectedAmount.Div(expectedAmount, oneEth)
	expectedAmount.Add(expectedAmount, big.NewInt(4)) // Deal with integer division
	require.Equal(t, expectedAmount, wethReturned)
	t.Logf("Redeemed %d xrETH for %.10f WETH", xrEthRedeemAmount.Int64(), eth.WeiToEth(wethReturned))

	/*
		// Redeem all xRPL
		xRplRedeemAmount := rplAmount
		rplReturned := redeemToken(t, qMgr, txMgr, bindings.RplVault, xRplRedeemAmount, false, deployerOpts)
		expectedAmount = rplAmount
		require.Equal(t, rplAmount, rplReturned)
		t.Logf("Redeemed %d xRPL for %.10f RPL", xRplRedeemAmount.Int64(), eth.WeiToEth(rplReturned))
	*/
	// Claim NO rewards

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
	rplDepositAmount = redeemToken(t, qMgr, txMgr, bindings.RplVault, xRplRedeemAmount, false, deployerOpts)
	require.Equal(t, xRplRedeemAmount, rplDepositAmount)
	t.Logf("Redeemed %.6f xRPL (%s wei) for %.6f RPL (%s wei)", eth.WeiToEth(xRplRedeemAmount), xRplRedeemAmount.String(), eth.WeiToEth(rplDepositAmount), rplDepositAmount.String())

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
	sig, err := createXrEthOracleSignature(totalYieldAccrued, nodesetTime, csMgr.XrEthAdminOracle.Address, chainID, deployerKey)
	require.NoError(t, err)
	txInfo, err = csMgr.XrEthAdminOracle.SetTotalYieldAccrued(totalYieldAccrued, sig, nodesetTime, deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, deployerOpts, "Updated the xrETH Oracle")

	// Verify the new ETH:xrETH price
	originalAmount := new(big.Int).Sub(ethDepositAmount, wethReturned)
	numerator := new(big.Int).Add(originalAmount, totalYieldAccrued)
	numerator.Mul(numerator, oneEth)
	expectedRatio := new(big.Int).Div(numerator, originalAmount)
	//expectedRatio.Sub(expectedRatio, common.Big1) // Compensate for rounding errors
	xrEthPriceAccordingToVault := getTokenPrice(t, qMgr, csMgr.WethVault)
	require.Equal(t, expectedRatio, xrEthPriceAccordingToVault)
	t.Logf("The new ETH:xrETH price according to the token is %.10f (%s wei)", eth.WeiToEth(xrEthPriceAccordingToVault), xrEthPriceAccordingToVault.String())
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
	}
	t.Logf("Redemption calculated as %.6f token (%s wei)", eth.WeiToEth(amount), amount.String())
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
	t.Logf("Redeemed %s %s for %s %s", amount.String(), token.Symbol(), redeemedAmount.String(), asset.Symbol())
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
func createXrEthOracleSignature(newTotalYieldAccrued *big.Int, timestamp time.Time, xrEthAdminOracleAddress common.Address, chainID *big.Int, key *ecdsa.PrivateKey) ([]byte, error) {
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
		xrEthAdminOracleAddress[:],
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
	}, nil,
		bindings.MinipoolManager.LaunchBalance,
	)
	require.NoError(t, err)
	minipoolCount := int(minipoolCountBig.Uint64())
	mpLaunchBalance := bindings.MinipoolManager.LaunchBalance.Get()

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
	activeAddresses := make([]common.Address, len(activeMinipools))
	for i, mp := range activeMinipools {
		activeAddresses[i] = mp.RocketPoolBinding.Common().Address
	}
	activeBalances, err := bb.GetEthBalances(activeAddresses, nil)
	require.NoError(t, err)

	// Go through each detail and calculate the xrETH share of rewards
	oneEth := big.NewInt(1e18)
	oneGwei := big.NewInt(1e9)
	totalRewards := big.NewInt(0)
	for i, mp := range activeMinipools {
		mpCommon := mp.RocketPoolBinding.Common()
		pubkey := mpCommon.Pubkey.Get()

		// Get the aggregated balance
		elBalance := activeBalances[i]
		beaconBalance := beaconStatuses[pubkey].Balance
		beaconBalanceWei := new(big.Int).SetUint64(beaconBalance)
		beaconBalanceWei.Mul(beaconBalanceWei, oneGwei)
		mpBalance := new(big.Int).Add(elBalance, beaconBalanceWei)
		mpRewards := new(big.Int).Sub(mpBalance, mpLaunchBalance)

		// Get the xrETH share of rewards and add it to the total
		fees := new(big.Int).Add(mp.ConstellationData.NodeFee, mp.ConstellationData.TreasuryFee)
		xrEthShare := new(big.Int).Sub(oneEth, fees)
		xrEthRewards := new(big.Int).Mul(mpRewards, xrEthShare)
		xrEthRewards.Div(xrEthRewards, oneEth)
		totalRewards.Add(totalRewards, xrEthRewards)
	}

	return totalRewards
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