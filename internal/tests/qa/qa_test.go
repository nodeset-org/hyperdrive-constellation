package qa

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log/slog"
	"math/big"
	"path/filepath"
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
	"github.com/nodeset-org/nodeset-client-go/utils"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/beacon"
	"github.com/rocket-pool/node-manager-core/beacon/ssz_types"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/node-manager-core/node/validator"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
	"github.com/rocket-pool/rocketpool-go/v2/types"
	"github.com/stretchr/testify/require"
	eth2types "github.com/wealdtech/go-eth2-types/v2"
)

var (
	shouldPrintTickInfo bool = false
)

// Run test 3 of the QA suite
func Test3_ComplexRoundTrip(t *testing.T) {
	err := testMgr.DependsOnConstellationBaseline()
	require.NoError(t, err)

	// Get some services
	bindings, err := cstestutils.CreateBindings(testMgr.GetNode().GetServiceProvider())
	require.NoError(t, err)
	sp := testMgr.GetNode().GetServiceProvider()
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

	// Make sure the contract state is clean
	runPreflightChecks(t, bindings)

	// Deposit RPL to the RPL vault
	rplAmount := eth.EthToWei(4000)
	cstestutils.DepositToRplVault(t, testMgr, csMgr.RplVault, bindings.Rpl, rplAmount, testMgr.GetDeployerOpts())
	printTickInfo(t, sp)

	// Deposit WETH to the WETH vault
	oneEth := big.NewInt(1e18)
	wethAmount := eth.EthToWei(100)
	var xrEthBalance *big.Int
	var mintFee *big.Int
	cstestutils.DepositToWethVault(t, testMgr, csMgr.WethVault, bindings.Weth, wethAmount, testMgr.GetDeployerOpts())
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.WethVault.BalanceOf(mc, &xrEthBalance, testMgr.GetDeployerOpts().From)
		csMgr.WethVault.GetMintFee(mc, &mintFee)
		return nil
	}, nil)
	require.NoError(t, err)
	feeAmount := new(big.Int).Mul(wethAmount, mintFee)
	feeAmount.Div(feeAmount, oneEth)
	expectedAmount := new(big.Int).Sub(wethAmount, feeAmount)
	require.Equal(t, expectedAmount, xrEthBalance)
	t.Logf("Deployer xrETH balance is now %.6f (%s wei)", eth.WeiToEth(xrEthBalance), xrEthBalance.String())
	printTickInfo(t, sp)

	// Build the minipool creation TXs
	minipoolsPerNode := 1
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	nsDB := nsMgr.GetDatabase()
	res := sp.GetResources()
	deployment := nsDB.Constellation.GetDeployment(res.DeploymentName)
	datas, hashes := cstestutils.BuildAndSubmitCreateMinipoolTxs(t, deployment, nodes, nodeAddresses, minipoolsPerNode, nil, bindings.RpSuperNode)

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
	expectedMpIndex := 1

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
	simulateBeaconRewards(t, sp, datas, elRewardsPerMinipool, uint64(beaconRewardsPerValidator), testMgr.GetDeployerOpts())
	totalYieldAccrued, oracleError := calculateXrEthOracleTotalYieldAccrued(t, sp, bindings)
	t.Logf(
		"The new total yield accrued to report is %.10f (%s wei) and the error is %.10f (%s wei)",
		eth.WeiToEth(totalYieldAccrued),
		totalYieldAccrued.String(),
		eth.WeiToEth(oracleError),
		oracleError.String(),
	)

	// Update the oracle report
	var expectedOracleError *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.OperatorDistributor.GetOracleError(mc, &expectedOracleError)
		return nil
	}, nil)
	require.NoError(t, err)
	chainID := new(big.Int).SetUint64(testMgr.GetBeaconMockManager().GetConfig().ChainID)
	newTime := time.Now().Add(timeToAdvance)
	sig, err := createXrEthOracleSignature(totalYieldAccrued, expectedOracleError, newTime, csMgr.PoAConstellationOracle.Address, chainID, testMgr.GetDeployerKey())
	require.NoError(t, err)
	txInfo, err := csMgr.PoAConstellationOracle.SetTotalYieldAccrued(totalYieldAccrued, oracleError, sig, newTime, testMgr.GetDeployerOpts())
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, testMgr.GetDeployerOpts(), "Updated the xrETH Oracle")
	printTickInfo(t, sp)

	// Verify the new ETH:xrETH price
	numerator := new(big.Int).Add(xrEthBalance, totalYieldAccrued)
	numerator.Mul(numerator, oneEth)
	expectedRatio := new(big.Int).Div(numerator, xrEthBalance)
	xrEthPriceAccordingToVault := getTokenPrice(t, qMgr, csMgr.WethVault)
	requireApproxEqual(t, expectedRatio, xrEthPriceAccordingToVault)
	t.Logf("The new ETH:xrETH price according to the token is %.10f (%s wei)", eth.WeiToEth(xrEthPriceAccordingToVault), xrEthPriceAccordingToVault.String())

	// Redeem 5 xrETH
	xrEthRedeemAmount := eth.EthToWei(5)
	wethReturned := redeemToken(t, qMgr, txMgr, csMgr.WethVault, xrEthRedeemAmount, false, testMgr.GetDeployerOpts())
	expectedAmount = new(big.Int).Mul(xrEthRedeemAmount, xrEthPriceAccordingToVault)
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
	rplReturned := redeemToken(t, qMgr, txMgr, csMgr.RplVault, xRplRedeemAmount, false, testMgr.GetDeployerOpts())
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
		setMinipoolToWithdrawn(t, sp, datas[i][0], testMgr.GetDeployerOpts())
	}
	t.Log("Exited the first 3 minipools")

	// Fast forward time by a week
	seconds := uint64(24 * 60 * 60 * 7)
	secondsDuration := time.Duration(seconds) * time.Second
	slots := secondsDuration / secondsPerSlot
	err = testMgr.AdvanceSlots(uint(slots), false)
	require.NoError(t, err)
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Logf("Fast forwarded 1 week")

	// Run the tick 3 times
	for i := 0; i < 3; i++ {
		var mpCount *big.Int
		var nextMpIndex *big.Int
		err := qMgr.Query(func(mc *batch.MultiCaller) error {
			csMgr.OperatorDistributor.GetNextMinipool(mc, &nextMinipoolAddress)
			csMgr.SuperNodeAccount.GetMinipoolCount(mc, &mpCount)
			csMgr.OperatorDistributor.GetNextMinipoolIndex(mc, &nextMpIndex)
			return nil
		}, nil)
		require.NoError(t, err)
		require.Equal(t, datas[expectedMpIndex][0].MinipoolAddress, nextMinipoolAddress)
		t.Logf("The next minipool to tick is %s as expected (index %d)", nextMinipoolAddress.Hex(), expectedMpIndex)
		t.Logf("The minipool count is %d, next index = %d", mpCount.Int64(), nextMpIndex.Int64())

		txInfo, err := csMgr.OperatorDistributor.ProcessNextMinipool(testMgr.GetDeployerOpts())
		require.NoError(t, err)
		testMgr.MineTx(t, txInfo, testMgr.GetDeployerOpts(), fmt.Sprintf("Processed the next minipool (tick %d)", i+1))

		printTickInfo(t, sp)
		expectedMpIndex++
		if expectedMpIndex >= len(datas) { // Handling the fact that MP2 exited and got removed
			expectedMpIndex = 0
		}
	}

	// Update the xrETH Oracle again
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.OperatorDistributor.GetOracleError(mc, &expectedOracleError)
		return nil
	}, nil)
	require.NoError(t, err)
	totalYieldAccrued, oracleError = calculateXrEthOracleTotalYieldAccrued(t, sp, bindings)
	newTime = newTime.Add(time.Hour)
	t.Logf("The new total yield accrued to report is %.10f (%s wei)", eth.WeiToEth(totalYieldAccrued), totalYieldAccrued.String())
	sig, err = createXrEthOracleSignature(totalYieldAccrued, expectedOracleError, newTime, csMgr.PoAConstellationOracle.Address, chainID, testMgr.GetDeployerKey())
	require.NoError(t, err)
	txInfo, err = csMgr.PoAConstellationOracle.SetTotalYieldAccrued(totalYieldAccrued, oracleError, sig, newTime, testMgr.GetDeployerOpts())
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, testMgr.GetDeployerOpts(), "Updated the xrETH Oracle")

	// Verify the new ETH:xrETH price
	xrEthPriceAccordingToVault = getTokenPrice(t, qMgr, csMgr.WethVault)
	requireApproxEqual(t, expectedRatio, xrEthPriceAccordingToVault)
	t.Logf("The new ETH:xrETH price according to the token is %.10f (%s wei)", eth.WeiToEth(xrEthPriceAccordingToVault), xrEthPriceAccordingToVault.String())

	// Make sure the rewards contract got the correct amount
	rewardsContractBalance, err := ec.BalanceAt(context.Background(), bindings.NodeSetOperatorRewardsDistributorAddress, nil)
	require.NoError(t, err)
	expectedShare := 0.14788 * 0.3625 * (0.005 + 0.005 + 0.005 + 0.005 + 0.015) // Quick and dirty; CS NO share * RP NO share * (MP1-EL + MP2-EL + MP3 + MP4 + MP0-EL+CL)
	expectedShareBig := eth.EthToWei(expectedShare)
	requireApproxEqual(t, expectedShareBig, rewardsContractBalance)
	nodeOpShare := new(big.Int).Div(rewardsContractBalance, big.NewInt(int64(len(nodes))))
	t.Logf("Rewards contract has %.6f ETH (%s wei) across %d operators for %.6f ETH (%s wei) each",
		eth.WeiToEth(rewardsContractBalance),
		rewardsContractBalance.String(),
		len(nodes),
		eth.WeiToEth(nodeOpShare),
		nodeOpShare.String(),
	)

	// Run a treasury claim
	treasuryRecipient := testMgr.GetOdaoOpts()[0].From
	preBalance, err := ec.BalanceAt(context.Background(), treasuryRecipient, nil)
	require.NoError(t, err)

	txInfo, err = csMgr.Treasury.ClaimEth(treasuryRecipient, testMgr.GetAdminOpts())
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, testMgr.GetAdminOpts(), "Treasury claimed ETH rewards")

	postBalance, err := ec.BalanceAt(context.Background(), treasuryRecipient, nil)
	require.NoError(t, err)
	treasuryRewards := new(big.Int).Sub(postBalance, preBalance)
	requireApproxEqual(t, expectedShareBig, treasuryRewards) // Expect the same amount as the NO share
	t.Logf("Treasury claimed ETH rewards and received %.6f ETH (%s wei)", eth.WeiToEth(treasuryRewards), treasuryRewards.String())
}

// Run test 4 of the QA suite
func Test4_SimpleNOConcurrency(t *testing.T) {
	err := testMgr.DependsOnConstellationBaseline()
	require.NoError(t, err)

	// Get some services
	sp := testMgr.GetNode().GetServiceProvider()
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	nsDB := nsMgr.GetDatabase()
	res := sp.GetResources()
	deployment := nsDB.Constellation.GetDeployment(res.DeploymentName)
	csMgr := sp.GetConstellationManager()
	bindings, err := cstestutils.CreateBindings(testMgr.GetNode().GetServiceProvider())
	require.NoError(t, err)
	t.Log("Created bindings")

	// Create some subnodes
	nodes, nodeAddresses, err := createNodesForTest(t, 1, eth.EthToWei(100))
	require.NoError(t, err)

	// Make sure the contract state is clean
	runPreflightChecks(t, bindings)

	// Get the deposit amounts
	wethAmount, rplAmount := getDepositAmounts(t, bindings, testMgr.GetNode().GetServiceProvider(), 1)

	// Deposit WETH to the WETH vault
	cstestutils.DepositToWethVault(t, testMgr, csMgr.WethVault, bindings.Weth, wethAmount, testMgr.GetDeployerOpts())

	// Deposit RPL to the RPL vault
	initialAmount := eth.EthToWei(300)
	cstestutils.DepositToRplVault(t, testMgr, csMgr.RplVault, bindings.Rpl, initialAmount, testMgr.GetDeployerOpts())

	// Deposit RPL to the RPL vault
	remainder := new(big.Int).Sub(rplAmount, initialAmount)
	cstestutils.DepositToRplVault(t, testMgr, csMgr.RplVault, bindings.Rpl, remainder, testMgr.GetDeployerOpts())

	// Build the minipool creation TXs
	_, hashes := cstestutils.BuildAndSubmitCreateMinipoolTxs(t, deployment, nodes, nodeAddresses, 1, nil, bindings.RpSuperNode)

	// Mine a block
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined a block")

	// The first one should succeed
	hd := testMgr.GetNode().GetHyperdriveNode().GetApiClient()
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
	err := testMgr.DependsOnConstellationBaseline()
	require.NoError(t, err)

	// Get some services
	bindings, err := cstestutils.CreateBindings(testMgr.GetNode().GetServiceProvider())
	sp := testMgr.GetNode().GetServiceProvider()
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	nsDB := nsMgr.GetDatabase()
	res := sp.GetResources()
	deployment := nsDB.Constellation.GetDeployment(res.DeploymentName)
	csMgr := sp.GetConstellationManager()
	require.NoError(t, err)
	t.Log("Created bindings")

	// Create some subnodes
	nodes, nodeAddresses, err := createNodesForTest(t, 14, eth.EthToWei(50))
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
	cstestutils.DepositToWethVault(t, testMgr, csMgr.WethVault, bindings.Weth, wethAmount, testMgr.GetDeployerOpts())

	// Deposit RPL to the RPL vault
	cstestutils.DepositToRplVault(t, testMgr, csMgr.RplVault, bindings.Rpl, rplAmount, testMgr.GetDeployerOpts())

	// Build the wave 1 minipool creation TXs
	wave1Nodes := nodes[:5]
	wave1Addresses := nodeAddresses[:5]
	wave1Salts := salts[:5]
	_, wave1Hashes := cstestutils.BuildAndSubmitCreateMinipoolTxs(t, deployment, wave1Nodes, wave1Addresses, 1, wave1Salts, bindings.RpSuperNode)

	// Mine a block
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined a block")

	// Wave 1 should succeed
	hd := testMgr.GetNode().GetHyperdriveNode().GetApiClient()
	for _, hashesPerNode := range wave1Hashes {
		_, err = hd.Tx.WaitForTransaction(hashesPerNode[0])
		require.NoError(t, err)
	}
	t.Log("First minipool creation wave succeeded")

	// Build the wave 2 minipool creation TXs
	wave2Nodes := nodes[5:10]
	wave2Addresses := nodeAddresses[5:10]
	wave2Salts := salts[5:10]
	_, wave2Hashes := cstestutils.BuildAndSubmitCreateMinipoolTxs(t, deployment, wave2Nodes, wave2Addresses, 1, wave2Salts, bindings.RpSuperNode)

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
		depositResponse, err := cs.Minipool.Create(salt, false, false)
		require.NoError(t, err)
		require.False(t, depositResponse.Data.CanCreate)
		require.True(t, depositResponse.Data.InsufficientLiquidity)
		t.Logf("Node %d correctly reported insufficient liquidity", i+10)
	}
	t.Log("Third minipool creation wave failed as expected")
}

// Run test 13 of the QA suite
func Test13_OrderlyStressTest(t *testing.T) {
	err := testMgr.DependsOnConstellationBaseline()
	require.NoError(t, err)

	// Get some services
	bindings, err := cstestutils.CreateBindings(testMgr.GetNode().GetServiceProvider())
	require.NoError(t, err)
	sp := testMgr.GetNode().GetServiceProvider()
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()
	txMgr := sp.GetTransactionManager()
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	nsDB := nsMgr.GetDatabase()
	res := sp.GetResources()
	deployment := nsDB.Constellation.GetDeployment(res.DeploymentName)
	//ec := sp.GetEthClient()
	t.Log("Created bindings")

	// Disable the ETH/RPL ratio enforcement
	minRatio := common.Big0
	maxRatio := eth.EthToWei(100000)
	setCoverageRatios(t, sp, minRatio, maxRatio)

	// Set the liquidity reserves
	tenPercent := big.NewInt(1e17) // 10%
	setLiquidityReservePercents(t, sp, tenPercent, tenPercent)

	// Get the current RPL price
	var rplPerEth *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.PriceFetcher.GetRplPrice(mc, &rplPerEth)
		return nil
	}, nil)
	require.NoError(t, err)

	// Deposit into the WETH Vault
	oneEth := big.NewInt(1e18)
	ethDepositAmount := eth.EthToWei(1000)
	var xrEthBalance *big.Int
	var mintFee *big.Int
	cstestutils.DepositToWethVault(t, testMgr, csMgr.WethVault, bindings.Weth, ethDepositAmount, testMgr.GetDeployerOpts())
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.WethVault.BalanceOf(mc, &xrEthBalance, testMgr.GetDeployerOpts().From)
		csMgr.WethVault.GetMintFee(mc, &mintFee)
		return nil
	}, nil)
	require.NoError(t, err)
	feeAmount := new(big.Int).Mul(ethDepositAmount, mintFee)
	feeAmount.Div(feeAmount, oneEth)
	expectedAmount := new(big.Int).Sub(ethDepositAmount, feeAmount)
	require.Equal(t, expectedAmount, xrEthBalance)
	t.Logf("Deployer xrETH balance is now %.6f (%s wei)", eth.WeiToEth(xrEthBalance), xrEthBalance.String())
	printTickInfo(t, sp)

	// Deposit into the RPL Vault
	twentyPercent := big.NewInt(2e17) // 20%
	rplDepositAmount := new(big.Int).Mul(ethDepositAmount, rplPerEth)
	rplDepositAmount.Mul(rplDepositAmount, twentyPercent)
	rplDepositAmount.Div(rplDepositAmount, oneEth)
	rplDepositAmount.Div(rplDepositAmount, oneEth)
	cstestutils.DepositToRplVault(t, testMgr, csMgr.RplVault, bindings.Rpl, rplDepositAmount, testMgr.GetDeployerOpts())

	// Create some subnodes
	nodes, nodeAddresses, err := createNodesForTest(t, 2, eth.EthToWei(50))
	require.NoError(t, err)

	// Set max minipools per node
	wave1MinipoolsPerNode := 4
	txInfo, err := csMgr.SuperNodeAccount.SetMaxValidators(big.NewInt(int64(wave1MinipoolsPerNode)), testMgr.GetDeployerOpts())
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, testMgr.GetDeployerOpts(), fmt.Sprintf("Set the max validators to %d", wave1MinipoolsPerNode))

	// Make the RP deposit pool way bigger to account for the minipool creation count
	depositPoolSize := eth.EthToWei(2000)
	txInfo, err = bindings.ProtocolDaoManager.Settings.Deposit.MaximumDepositPoolSize.Bootstrap(depositPoolSize, testMgr.GetDeployerOpts())
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, testMgr.GetDeployerOpts(), fmt.Sprintf("Set the maximum deposit pool size to %.2f ETH", eth.WeiToEth(depositPoolSize)))

	// Deposit into the RP deposit pool
	fundOpts := &bind.TransactOpts{
		From:  testMgr.GetDeployerOpts().From,
		Value: depositPoolSize,
	}
	fundTxInfo, err := bindings.DepositPoolManager.Deposit(fundOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, fundTxInfo, testMgr.GetDeployerOpts(), "Funded the RP deposit pool")

	// Create minipools
	wave1Data, wave1CreateHashes := cstestutils.BuildAndSubmitCreateMinipoolTxs(t, deployment, nodes, nodeAddresses, wave1MinipoolsPerNode, nil, bindings.RpSuperNode)

	// Mine a block
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined a block")

	// Wave 1 creation should succeed
	hd := testMgr.GetNode().GetHyperdriveNode().GetApiClient()
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
	printTickInfo(t, sp)

	// Redeem 10 xrETH
	xrEthRedeemAmount := eth.EthToWei(10)
	wethReturned := redeemToken(t, qMgr, txMgr, csMgr.WethVault, xrEthRedeemAmount, false, testMgr.GetDeployerOpts())
	require.Equal(t, xrEthRedeemAmount, wethReturned)
	t.Logf("Redeemed %.6f xrETH (%s wei) for %.6f WETH (%s wei)", eth.WeiToEth(xrEthRedeemAmount), xrEthRedeemAmount.String(), eth.WeiToEth(wethReturned), wethReturned.String())
	printTickInfo(t, sp)

	// Redeem 100 xrRPL
	xRplRedeemAmount := eth.EthToWei(100)
	rplReturned := redeemToken(t, qMgr, txMgr, csMgr.RplVault, xRplRedeemAmount, false, testMgr.GetDeployerOpts())
	require.Equal(t, xRplRedeemAmount, rplReturned)
	t.Logf("Redeemed %.6f xRPL (%s wei) for %.6f RPL (%s wei)", eth.WeiToEth(xRplRedeemAmount), xRplRedeemAmount.String(), eth.WeiToEth(rplReturned), rplReturned.String())
	printTickInfo(t, sp)

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

	// Update the timestamp for signatures
	sigTime := time.Now()
	sigTime = sigTime.Add(secondsDuration)

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
	printTickInfo(t, sp)

	// Fast forward 1 week
	seconds = uint64(24 * 60 * 60 * 7)
	secondsDuration = time.Duration(seconds) * time.Second
	slots = seconds / secondsPerSlot
	err = testMgr.AdvanceSlots(uint(slots), false)
	require.NoError(t, err)
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Fast forwarded 1 week")

	// Update the timestamp for signatures
	sigTime = sigTime.Add(secondsDuration)

	// Assume 0.010 ETH in rewards on Beacon and 0.005 on the EL per validator
	elRewardsPerMinipool := eth.EthToWei(0.005)
	beaconRewardsPerValidator := 1e7 // Beacon is in gwei
	simulateBeaconRewards(t, sp, wave1Data, elRewardsPerMinipool, uint64(beaconRewardsPerValidator), testMgr.GetDeployerOpts())
	totalYieldAccrued, oracleError := calculateXrEthOracleTotalYieldAccrued(t, sp, bindings)
	t.Logf("The new total yield accrued to report is %.10f (%s wei)", eth.WeiToEth(totalYieldAccrued), totalYieldAccrued.String())

	// Update the oracle report
	var expectedOracleError *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.OperatorDistributor.GetOracleError(mc, &expectedOracleError)
		return nil
	}, nil)
	require.NoError(t, err)
	chainID := new(big.Int).SetUint64(testMgr.GetBeaconMockManager().GetConfig().ChainID)
	sig, err := createXrEthOracleSignature(totalYieldAccrued, expectedOracleError, sigTime, csMgr.PoAConstellationOracle.Address, chainID, testMgr.GetDeployerKey())
	require.NoError(t, err)
	txInfo, err = csMgr.PoAConstellationOracle.SetTotalYieldAccrued(totalYieldAccrued, oracleError, sig, sigTime, testMgr.GetDeployerOpts())
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, testMgr.GetAdminOpts(), "Updated the xrETH Oracle")
	printTickInfo(t, sp)

	// Verify the new ETH:xrETH price
	originalAmount := new(big.Int).Sub(xrEthBalance, wethReturned)
	numerator := new(big.Int).Add(originalAmount, totalYieldAccrued)
	numerator.Mul(numerator, oneEth)
	expectedXrEthPrice := new(big.Int).Div(numerator, originalAmount)
	xrEthPriceAccordingToVault := getTokenPrice(t, qMgr, csMgr.WethVault)
	requireApproxEqual(t, expectedXrEthPrice, xrEthPriceAccordingToVault)
	t.Logf("The new ETH:xrETH price according to the token is %.10f (%s wei)", eth.WeiToEth(xrEthPriceAccordingToVault), xrEthPriceAccordingToVault.String())

	// == The rest of this test is OBE now with the new contract setup ==

	/*

		// Run an RP rewards interval
		rewardsMap, rewardsSubmission, slotsFastForwarded := executeRpRewardsInterval(t, sp, bindings)

		// Update the timestamp for signatures
		secondsDuration = time.Duration(slotsFastForwarded*secondsPerSlot) * time.Second
		sigTime = sigTime.Add(secondsDuration)

		// Do a merkle claim
		merkleCfg := createMerkleClaimConfig(t, sp, bindings, rewardsSubmission)
		constellationRewards := rewardsMap[csMgr.SuperNodeAccount.Address]
		txInfo, err = csMgr.SuperNodeAccount.MerkleClaim(
			[]*big.Int{rewardsSubmission.RewardIndex},
			[]*big.Int{constellationRewards.CollateralRpl},
			[]*big.Int{constellationRewards.SmoothingPoolEth},
			[][]common.Hash{constellationRewards.MerkleProof},
			merkleCfg,
			testMgr.GetDeployerOpts(),
		)
		require.NoError(t, err)
		testMgr.MineTx(t, txInfo, testMgr.GetDeployerOpts(), "Executed the Merkle claim")
		t.Logf("Rewards amount: %.6f ETH (%s wei), %.6f RPL (%s wei)",
			eth.WeiToEth(constellationRewards.SmoothingPoolEth),
			constellationRewards.SmoothingPoolEth.String(),
			eth.WeiToEth(constellationRewards.CollateralRpl),
			constellationRewards.CollateralRpl.String(),
		)
		printTickInfo(t, sp)

		// Verify post-tick interval details
		expectedMpIndex := 2
		var nextMinipoolAddress common.Address
		err = qMgr.Query(func(mc *batch.MultiCaller) error {
			csMgr.OperatorDistributor.GetNextMinipool(mc, &nextMinipoolAddress)
			return nil
		}, nil)
		require.NoError(t, err)
		require.Equal(t, wave1Data[0][expectedMpIndex].MinipoolAddress, nextMinipoolAddress)
		t.Logf("The next minipool to tick is %s as expected (index %d)", nextMinipoolAddress.Hex(), expectedMpIndex)

		// Get the split for RPL rewards
		treasuryShareOfRplRewards := new(big.Int).Mul(constellationRewards.CollateralRpl, merkleCfg.AverageRplTreasuryFee)
		treasuryShareOfRplRewards.Div(treasuryShareOfRplRewards, oneEth)
		xRplShareOfRewards := new(big.Int).Sub(constellationRewards.CollateralRpl, treasuryShareOfRplRewards)
		t.Logf("xRPL holders get %.6f RPL (%s wei), treasury gets %.6f RPL (%s wei)",
			eth.WeiToEth(xRplShareOfRewards), xRplShareOfRewards.String(),
			eth.WeiToEth(treasuryShareOfRplRewards), treasuryShareOfRplRewards.String(),
		)

		// Get the split for xrETH rewards
		nodeOpShareOfEthRewards := new(big.Int).Mul(constellationRewards.SmoothingPoolEth, merkleCfg.AverageEthOperatorFee)
		nodeOpShareOfEthRewards.Div(nodeOpShareOfEthRewards, oneEth)
		treasuryShareOfEthRewards := new(big.Int).Mul(constellationRewards.SmoothingPoolEth, merkleCfg.AverageEthTreasuryFee)
		treasuryShareOfEthRewards.Div(treasuryShareOfEthRewards, oneEth)
		xrEthShareOfRewards := new(big.Int).Sub(constellationRewards.SmoothingPoolEth, nodeOpShareOfEthRewards)
		xrEthShareOfRewards.Sub(xrEthShareOfRewards, treasuryShareOfEthRewards)
		t.Logf("xrETH holders get %.6f ETH (%s wei), node ops get %.6f ETH (%s wei), treasury gets %.6f ETH (%s wei)",
			eth.WeiToEth(xrEthShareOfRewards), xrEthShareOfRewards.String(),
			eth.WeiToEth(nodeOpShareOfEthRewards), nodeOpShareOfEthRewards.String(),
			eth.WeiToEth(treasuryShareOfEthRewards), treasuryShareOfEthRewards.String(),
		)

		// Verify current rewards
		rewardsContractBalance, err := ec.BalanceAt(context.Background(), bindings.NodeSetOperatorRewardsDistributorAddress, nil)
		requireApproxEqual(t, nodeOpShareOfEthRewards, rewardsContractBalance)
		nodeOpShare := new(big.Int).Div(rewardsContractBalance, big.NewInt(int64(len(nodes))))
		t.Logf("Rewards contract has %.6f ETH (%s wei) across %d operators for %.6f ETH (%s wei) each",
			eth.WeiToEth(rewardsContractBalance),
			rewardsContractBalance.String(),
			len(nodes),
			eth.WeiToEth(nodeOpShare),
			nodeOpShare.String(),
		)

		// Verify the RPL:xRPL ratio
		originalAmount = new(big.Int).Sub(rplDepositAmount, rplReturned)
		numerator = new(big.Int).Add(originalAmount, xRplShareOfRewards)
		numerator.Mul(numerator, oneEth)
		expectedXRplPrice := new(big.Int).Div(numerator, originalAmount)
		xRplPriceAccordingToVault := getTokenPrice(t, qMgr, csMgr.RplVault)
		requireApproxEqual(t, expectedXRplPrice, xRplPriceAccordingToVault)
		t.Logf("The new RPL:xRPL price according to the token is %.10f (%s wei), which matches the expected value", eth.WeiToEth(xRplPriceAccordingToVault), xRplPriceAccordingToVault.String())

		// Verify the ETH:xrETH ratio
		originalAmount = new(big.Int).Sub(xrEthBalance, wethReturned)
		numerator = new(big.Int).Add(originalAmount, totalYieldAccrued)
		numerator.Add(numerator, xrEthShareOfRewards)
		numerator.Mul(numerator, oneEth)
		expectedXrEthPrice = new(big.Int).Div(numerator, originalAmount)
		xrEthPriceAccordingToVault = getTokenPrice(t, qMgr, csMgr.WethVault)
		requireApproxEqual(t, expectedXrEthPrice, xrEthPriceAccordingToVault)
		t.Logf("The new ETH:xrETH price according to the token is %.10f (%s wei)", eth.WeiToEth(xrEthPriceAccordingToVault), xrEthPriceAccordingToVault.String())

		// Set the liquidity reserves
		onePercent := big.NewInt(1e16) // 1%
		setLiquidityReservePercents(t, sp, onePercent, onePercent)
		printTickInfo(t, sp)

		// Set the ETH/RPL min and max ratios to 10% and 30%
		setCoverageRatios(t, sp, big.NewInt(1e17), big.NewInt(3e17))
		printTickInfo(t, sp)

		// Set max minipools per node
		wave2MaxMinipoolsPerNode := 5
		txInfo, err = csMgr.SuperNodeAccount.SetMaxValidators(big.NewInt(int64(wave2MaxMinipoolsPerNode)), testMgr.GetDeployerOpts())
		require.NoError(t, err)
		testMgr.MineTx(t, txInfo, testMgr.GetDeployerOpts(), fmt.Sprintf("Set the max validators to %d", wave2MaxMinipoolsPerNode))
		printTickInfo(t, sp)

		// Node 1 and 2 should make 1 more minipool each
		wave2MinipoolsPerNode := wave2MaxMinipoolsPerNode - wave1MinipoolsPerNode
		wave2Nodes := nodes[:2]
		wave2NodeAddresses := nodeAddresses[:2]
		wave2Salts := make([][]*big.Int, len(wave2Nodes))
		wave2Offset := wave1MinipoolsPerNode * len(nodes)
		for i := 0; i < len(wave2Nodes); i++ {
			saltsPerNode := make([]*big.Int, wave2MinipoolsPerNode)
			for j := 0; j < wave2MinipoolsPerNode; j++ {
				saltsPerNode[j] = big.NewInt(int64(i*wave2MinipoolsPerNode + j + wave2Offset))
			}
			wave2Salts[i] = saltsPerNode
		}
		wave2Data, wave2CreateHashes := cstestutils.BuildAndSubmitCreateMinipoolTxs(t, nsMgr, wave2Nodes, wave2NodeAddresses, wave2MinipoolsPerNode, wave2Salts, bindings.RpSuperNode)

		// Mine a block
		err = testMgr.CommitBlock()
		require.NoError(t, err)
		t.Log("Mined a block")

		// Wave 2 creation should succeed
		for _, hashesPerNode := range wave2CreateHashes {
			for _, hash := range hashesPerNode {
				_, err = hd.Tx.WaitForTransaction(hash)
				require.NoError(t, err)
			}
		}
		t.Log("Wave 2 creation succeeded")
		printTickInfo(t, sp)

		// Verify minipools
		for i, dataForNode := range wave2Data {
			for j, data := range dataForNode {
				index := i*wave2MinipoolsPerNode + j + wave2Offset
				_ = cstestutils.VerifyMinipoolAfterCreation(t, qMgr, bindings.RpSuperNode, uint64(index), data.MinipoolAddress, bindings.MinipoolManager)
			}
		}
		t.Log("Verified wave 2 minipools")

		// Build the wave 3 minipool creation TXs
		wave3MinipoolCount := 1
		wave3Nodes := nodes[1:2]
		wave3Salts := make([][]*big.Int, len(wave3Nodes))
		wave3Offset := wave2Offset + (wave2MinipoolsPerNode * len(wave2Nodes))
		for i := 0; i < len(wave3Nodes); i++ {
			saltsPerNode := make([]*big.Int, wave3MinipoolCount)
			for j := 0; j < wave3MinipoolCount; j++ {
				saltsPerNode[j] = big.NewInt(int64(i*wave3MinipoolCount + j + wave3Offset))
			}
			wave3Salts[i] = saltsPerNode
		}

		// Attempt to build the wave 3 minipool creation TXs - they should all fail
		for i, node := range wave3Nodes {
			cs := node.GetApiClient()
			salt := wave3Salts[i][0]
			depositResponse, err := cs.Minipool.Create(salt)
			require.NoError(t, err)
			require.False(t, depositResponse.Data.CanCreate)
			require.True(t, depositResponse.Data.MaxMinipoolsReached)
			t.Logf("Node 1 correctly reported max minipools reached")
		}
		t.Log("Third minipool creation wave failed as expected")
		printTickInfo(t, sp)

		// Fast forward 1 day
		seconds = uint64(24 * 60 * 60)
		secondsDuration = time.Duration(seconds) * time.Second
		slots = seconds / secondsPerSlot
		err = testMgr.AdvanceSlots(uint(slots), false)
		require.NoError(t, err)
		err = testMgr.CommitBlock()
		require.NoError(t, err)
		t.Log("Fast forwarded 1 day")

		// Update the timestamp for signatures
		sigTime = sigTime.Add(secondsDuration)

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
		printTickInfo(t, sp)

		// Add wave 2 to Beacon
		bm := testMgr.GetBeaconMockManager()
		for _, wave2DataForNode := range wave2Data {
			for _, mp := range wave2DataForNode {
				pubkey := mp.ValidatorPubkey
				withdrawalCreds := validator.GetWithdrawalCredsFromAddress(mp.MinipoolAddress)
				val, err := bm.AddValidator(pubkey, withdrawalCreds)
				require.NoError(t, err)
				val.Status = beacon.ValidatorState_ActiveOngoing
				val.Balance = 32e9 // 32 ETH
			}
		}
		t.Logf("Added wave 2 to the Beacon Chain")

		// Make node 1 exit the new minipool and another one out of spite
		spiteMinipool := wave2Data[1][0]
		setMinipoolToWithdrawn(t, sp, spiteMinipool, testMgr.GetDeployerOpts())
		t.Logf("Node 1 exited minipool %s out of spite", spiteMinipool.MinipoolAddress.Hex())
		extraMinipool := wave1Data[1][0]
		setMinipoolToWithdrawn(t, sp, extraMinipool, testMgr.GetDeployerOpts())
		t.Logf("Node 1 exited minipool %s as well", extraMinipool.MinipoolAddress.Hex())

		// Tick the spite minipool
		txInfo, err = csMgr.OperatorDistributor.ProcessMinipool(spiteMinipool.MinipoolAddress, testMgr.GetDeployerOpts())
		require.NoError(t, err)
		testMgr.MineTx(t, txInfo, testMgr.GetDeployerOpts(), fmt.Sprintf("Ticked minipool %s", spiteMinipool.MinipoolAddress.Hex()))

		// Verify the next minipool hasn't changed
		err = qMgr.Query(func(mc *batch.MultiCaller) error {
			csMgr.OperatorDistributor.GetNextMinipool(mc, &nextMinipoolAddress)
			return nil
		}, nil)
		require.NoError(t, err)
		require.Equal(t, wave1Data[0][expectedMpIndex].MinipoolAddress, nextMinipoolAddress)
		t.Logf("The next minipool to tick is %s as expected (index %d)", nextMinipoolAddress.Hex(), expectedMpIndex)

		// Deposit into the RPL vault
		rplDepositAmount = eth.EthToWei(1000)
		testMgr.GetDeployerOpts().Nonce = nil
		err = testMgr.Constellation_DepositToRplVault(csMgr.RplVault, rplDepositAmount, testMgr.GetDeployerOpts(), testMgr.GetDeployerOpts())
		require.NoError(t, err)
		t.Logf("Deposited %.6f ETH (%s wei) into the RPL vault", eth.WeiToEth(rplDepositAmount), rplDepositAmount.String())

		// Attempt to deposit into the WETH vault - should fail
		ethDepositAmount = eth.EthToWei(2000)
		testMgr.GetDeployerOpts().Nonce = nil
		err = testMgr.Constellation_DepositToWethVault(bindings.Weth, csMgr.WethVault, ethDepositAmount, testMgr.GetDeployerOpts())
		testMgr.GetDeployerOpts().Nonce = nil
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed with status 0")
		t.Logf("Depositing into the WETH vault failed as expected: %v", err)

		// Redeem 8 xrETH
		xrEthRedeemAmount = eth.EthToWei(8)
		wethReturned2 := redeemToken(t, qMgr, txMgr, csMgr.WethVault, xrEthRedeemAmount, false, testMgr.GetDeployerOpts())
		expectedAmount = new(big.Int).Mul(xrEthRedeemAmount, expectedXrEthPrice)
		expectedAmount.Div(expectedAmount, oneEth)
		requireApproxEqual(t, expectedAmount, wethReturned2)
		t.Logf("Redeemed %.6f xrETH (%s wei) for %.6f WETH (%s wei)", eth.WeiToEth(xrEthRedeemAmount), xrEthRedeemAmount.String(), eth.WeiToEth(wethReturned2), wethReturned2.String())

		// Redeem 100 xrRPL
		xRplRedeemAmount = eth.EthToWei(100)
		rplReturned2 := redeemToken(t, qMgr, txMgr, csMgr.RplVault, xRplRedeemAmount, false, testMgr.GetDeployerOpts())
		expectedAmount = new(big.Int).Mul(xRplRedeemAmount, expectedXRplPrice)
		expectedAmount.Div(expectedAmount, oneEth)
		requireApproxEqualWithTolerance(t, expectedAmount, rplReturned2, big.NewInt(100))
		t.Logf("Redeemed %.6f xRPL (%s wei) for %.6f RPL (%s wei)", eth.WeiToEth(xRplRedeemAmount), xRplRedeemAmount.String(), eth.WeiToEth(rplReturned2), rplReturned2.String())

		// Verify post-tick interval details
		expectedMpIndex = 5
		err = qMgr.Query(func(mc *batch.MultiCaller) error {
			csMgr.OperatorDistributor.GetNextMinipool(mc, &nextMinipoolAddress)
			return nil
		}, nil)
		require.NoError(t, err)
		require.Equal(t, wave1Data[expectedMpIndex/wave1MinipoolsPerNode][expectedMpIndex%wave1MinipoolsPerNode].MinipoolAddress, nextMinipoolAddress)
		t.Logf("The next minipool to tick is %s as expected (index %d)", nextMinipoolAddress.Hex(), expectedMpIndex)

		// Tick all the minipools to collect rewards
		totalMpCount := wave1MinipoolsPerNode*len(nodes) + wave2MinipoolsPerNode*len(wave2Nodes)
		for i := 0; i < totalMpCount; i++ {
			txInfo, err := csMgr.OperatorDistributor.ProcessNextMinipool(testMgr.GetDeployerOpts())
			require.NoError(t, err)
			testMgr.MineTx(t, txInfo, testMgr.GetDeployerOpts(), fmt.Sprintf("Executed tick %d", i))
		}

		// Verify post-tick details
		expectedMpIndex = 7 // Went up by 2 since we exited 2 minipools
		err = qMgr.Query(func(mc *batch.MultiCaller) error {
			csMgr.OperatorDistributor.GetNextMinipool(mc, &nextMinipoolAddress)
			return nil
		}, nil)
		require.NoError(t, err)
		require.Equal(t, wave1Data[expectedMpIndex/wave1MinipoolsPerNode][expectedMpIndex%wave1MinipoolsPerNode].MinipoolAddress, nextMinipoolAddress)
		t.Logf("The next minipool to tick is %s as expected (index %d)", nextMinipoolAddress.Hex(), expectedMpIndex)

		// Verify interval 2 amount
		rewardsContractBalance2, err := ec.BalanceAt(context.Background(), bindings.NodeSetOperatorRewardsDistributorAddress, nil)
		rewardsContractBalanceDelta := new(big.Int).Sub(rewardsContractBalance2, rewardsContractBalance)
		expectedAmountFloat := 0.14788 * 0.3625 * (0.005*12 + 0.01) // Quick and dirty; CS NO share * RP NO share * (MPs 0-11 EL rewards + MP5 BN rewards)
		expectedAmount = eth.EthToWei(expectedAmountFloat)
		requireApproxEqual(t, expectedAmount, rewardsContractBalanceDelta)
		t.Logf("Interval 2 had %.6f ETH (%s wei) as expected", eth.WeiToEth(rewardsContractBalanceDelta), rewardsContractBalanceDelta.String())

		// Run a treasury claim for ETH
		treasuryRecipient := testMgr.GetOdaoOpts()[0].From
		preBalance, err := ec.BalanceAt(context.Background(), treasuryRecipient, nil)
		require.NoError(t, err)

		txInfo, err = csMgr.Treasury.ClaimEth(treasuryRecipient, testMgr.GetAdminOpts())
		require.NoError(t, err)
		testMgr.MineTx(t, txInfo, testMgr.GetAdminOpts(), "Treasury claimed ETH rewards")

		postBalance, err := ec.BalanceAt(context.Background(), treasuryRecipient, nil)
		require.NoError(t, err)
		treasuryRewards := new(big.Int).Sub(postBalance, preBalance)

		expectedAmountFloat = 0.14788 * 0.3625 * (0.005*12 + 0.01) // Quick and dirty; CS NO share * RP NO share * (MPs 0-11 EL rewards + MP5 BN rewards)
		expectedAmount = eth.EthToWei(expectedAmountFloat)
		expectedAmount.Add(expectedAmount, treasuryShareOfEthRewards) // Add SP rewards from interval 1
		requireApproxEqual(t, expectedAmount, treasuryRewards)
		t.Logf("Treasury claimed ETH rewards and received %.6f ETH (%s wei)", eth.WeiToEth(treasuryRewards), treasuryRewards.String())

		// Run a treasury claim for RPL
		treasuryRecipient = testMgr.GetOdaoOpts()[0].From
		err = qMgr.Query(func(mc *batch.MultiCaller) error {
			bindings.Rpl.BalanceOf(mc, &preBalance, treasuryRecipient)
			return nil
		}, nil)
		require.NoError(t, err)

		rplContract, err := sp.GetRocketPoolManager().RocketPool.GetContract(rocketpool.ContractName_RocketTokenRPL)
		require.NoError(t, err)
		txInfo, err = csMgr.Treasury.ClaimToken(rplContract.Address, treasuryRecipient, testMgr.GetAdminOpts())
		require.NoError(t, err)
		testMgr.MineTx(t, txInfo, testMgr.GetAdminOpts(), "Treasury claimed RPL rewards")

		err = qMgr.Query(func(mc *batch.MultiCaller) error {
			bindings.Rpl.BalanceOf(mc, &postBalance, treasuryRecipient)
			return nil
		}, nil)
		require.NoError(t, err)
		treasuryRewards = new(big.Int).Sub(postBalance, preBalance)
		require.Equal(t, treasuryShareOfRplRewards, treasuryRewards)
		t.Logf("Treasury claimed RPL rewards and received %.6f RPL (%s wei)", eth.WeiToEth(treasuryRewards), treasuryRewards.String())
	*/
}

// Run test 15 of the QA suite
func Test15_StakingTest(t *testing.T) {
	err := testMgr.DependsOnConstellationBaseline()
	require.NoError(t, err)

	// Get some services
	bindings, err := cstestutils.CreateBindings(testMgr.GetNode().GetServiceProvider())
	require.NoError(t, err)
	sp := testMgr.GetNode().GetServiceProvider()
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	nsDB := nsMgr.GetDatabase()
	res := sp.GetResources()
	deployment := nsDB.Constellation.GetDeployment(res.DeploymentName)
	t.Log("Created bindings")

	// Create some subnodes
	nodes, nodeAddresses, err := createNodesForTest(t, 14, eth.EthToWei(1.1))
	require.NoError(t, err)

	// Make sure the contract state is clean
	runPreflightChecks(t, bindings)

	// Get the deposit amounts
	wethAmount, rplDepositAmount := getDepositAmounts(t, bindings, sp, 10) // Enough for 10 minipools

	// Deposit RPL to the RPL vault
	cstestutils.DepositToRplVault(t, testMgr, csMgr.RplVault, bindings.Rpl, rplDepositAmount, testMgr.GetDeployerOpts())

	// Deposit WETH to the WETH vault
	cstestutils.DepositToWethVault(t, testMgr, csMgr.WethVault, bindings.Weth, wethAmount, testMgr.GetDeployerOpts())

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
	wave1NodeAddresses := nodeAddresses[:5]
	wave1Salts := salts[:5]
	wave1Data, wave1CreateHashes := cstestutils.BuildAndSubmitCreateMinipoolTxs(t, deployment, wave1Nodes, wave1NodeAddresses, 1, wave1Salts, bindings.RpSuperNode)

	// Mine a block
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined a block")

	// Wave 1 creation should succeed
	hd := testMgr.GetNode().GetHyperdriveNode().GetApiClient()
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
	slots := seconds / secondsPerSlot
	err = testMgr.AdvanceSlots(uint(slots), false)
	require.NoError(t, err)
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined a block")

	// Send ETH to the RP deposit pool again
	err = qMgr.Query(nil, nil, bindings.DepositPoolManager.Balance)
	require.NoError(t, err)
	t.Logf("RP deposit pool balance: %.2f ETH", eth.WeiToEth(bindings.DepositPoolManager.Balance.Get()))
	fundOpts := &bind.TransactOpts{
		From:  testMgr.GetDeployerOpts().From,
		Value: eth.EthToWei(120),
	}
	fundTxInfo, err := bindings.DepositPoolManager.Deposit(fundOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, fundTxInfo, testMgr.GetDeployerOpts(), "Funded the RP deposit pool again")

	// Build wave 1 minipools stake TXs
	wave1StakeHashes := cstestutils.BuildAndSubmitStakeMinipoolTxs(t, wave1Nodes, wave1Data)

	// Build the wave 2 minipool creation TXs
	wave2Nodes := nodes[5:10]
	wave2NodeAddresses := nodeAddresses[5:10]
	wave2Salts := salts[5:10]
	wave2Data, wave2CreationHashes := cstestutils.BuildAndSubmitCreateMinipoolTxs(t, deployment, wave2Nodes, wave2NodeAddresses, 1, wave2Salts, bindings.RpSuperNode)

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

	// Send ETH to the RP deposit pool again
	fundTxInfo, err = bindings.DepositPoolManager.Deposit(fundOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, fundTxInfo, testMgr.GetDeployerOpts(), "Funded the RP deposit pool again")

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
		depositResponse, err := cs.Minipool.Create(salt, false, false)
		require.NoError(t, err)
		require.False(t, depositResponse.Data.CanCreate)
		require.True(t, depositResponse.Data.InsufficientLiquidity)
		t.Logf("Node %d correctly reported insufficient liquidity", i+10)
	}
	t.Log("Third minipool creation wave failed as expected")
}

func TestGetMinipools(t *testing.T) {
	err := testMgr.DependsOnConstellationBaseline()
	require.NoError(t, err)

	// Get some services
	bindings, err := cstestutils.CreateBindings(testMgr.GetNode().GetServiceProvider())
	require.NoError(t, err)
	sp := testMgr.GetNode().GetServiceProvider()
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()
	txMgr := sp.GetTransactionManager()
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	nsDB := nsMgr.GetDatabase()
	beaconCfg := testMgr.GetBeaconMockManager().GetConfig()
	res := sp.GetResources()
	deployment := nsDB.Constellation.GetDeployment(res.DeploymentName)
	t.Log("Created bindings")

	// Register the main node with Constellation
	cstestutils.RegisterWithConstellation(t, testMgr, testMgr.GetNode())

	// Set the max validator count
	minipoolCount := 1000
	txInfo, err := csMgr.SuperNodeAccount.SetMaxValidators(big.NewInt(int64(minipoolCount)), testMgr.GetDeployerOpts())
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, testMgr.GetDeployerOpts(), fmt.Sprintf("Set the max validators to %d", minipoolCount))

	// Make the deposit pool large enough
	dpSize := eth.EthToWei(float64(minipoolCount) * 24)
	txInfo, err = bindings.ProtocolDaoManager.Settings.Deposit.MaximumDepositPoolSize.Bootstrap(dpSize, testMgr.GetDeployerOpts())
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, testMgr.GetDeployerOpts(), fmt.Sprintf("Set the deposit pool size to %.2f ETH", eth.WeiToEth(dpSize)))

	// Make the global minipool count large enough
	txInfo, err = bindings.ProtocolDaoManager.Settings.Minipool.MaximumCount.Bootstrap(big.NewInt(int64(minipoolCount)), testMgr.GetDeployerOpts())
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, testMgr.GetDeployerOpts(), fmt.Sprintf("Set the global minipool count to %d", minipoolCount))

	// Do enough deposits to fill the deposit pool
	maxDeposit := eth.EthToWei(8000)
	deposited := big.NewInt(0)
	chainID := big.NewInt(int64(beaconCfg.ChainID))
	for i := 10; i < 20; i++ {
		// Make the private key
		key, err := testMgr.GetKeyGenerator().GetEthPrivateKey(uint(i))
		require.NoError(t, err)
		opts, err := bind.NewKeyedTransactorWithChainID(key, chainID)
		require.NoError(t, err)

		// Deposit into the pool
		remainder := new(big.Int).Sub(dpSize, deposited)
		depositAmount := maxDeposit
		if depositAmount.Cmp(remainder) > 0 {
			depositAmount = remainder
		}
		opts.Value = depositAmount
		txInfo, err = bindings.DepositPoolManager.Deposit(opts)
		require.NoError(t, err)
		testMgr.MineTx(t, txInfo, opts, fmt.Sprintf("Deposited %.2f ETH into the deposit pool", eth.WeiToEth(depositAmount)))
		deposited.Add(deposited, depositAmount)

		// Stop if we're done
		if deposited.Cmp(dpSize) == 0 {
			break
		}
	}
	if deposited.Cmp(dpSize) != 0 {
		fail("Deposit pool not filled")
	}

	// Get the deposit amounts
	wethAmount, rplAmount := getDepositAmounts(t, bindings, testMgr.GetNode().GetServiceProvider(), minipoolCount)

	// Deposit WETH to the WETH vault
	cstestutils.DepositToWethVault(t, testMgr, csMgr.WethVault, bindings.Weth, wethAmount, testMgr.GetDeployerOpts())

	// Deposit RPL to the RPL vault
	cstestutils.DepositToRplVault(t, testMgr, csMgr.RplVault, bindings.Rpl, rplAmount, testMgr.GetDeployerOpts())

	// Make the salts
	salts := make([]*big.Int, minipoolCount)
	for i := 0; i < minipoolCount; i++ {
		salts[i] = big.NewInt(int64(i + 1))
	}

	// Get the expected addresses
	addresses := make([]common.Address, minipoolCount)
	maxQuerySize := 250
	mainNodeAddress := testMgr.GetMainNodeAddress()
	err = qMgr.BatchQuery(minipoolCount, maxQuerySize, func(mc *batch.MultiCaller, index int) error {
		salt := salts[index]
		saltBytes := [32]byte{}
		salt.FillBytes(saltBytes[:])
		saltWithNodeAddress := crypto.Keccak256(saltBytes[:], mainNodeAddress[:])
		internalSalt := new(big.Int).SetBytes(saltWithNodeAddress)
		bindings.RpSuperNode.GetExpectedMinipoolAddress(mc, &addresses[index], internalSalt)
		return nil
	}, nil)
	require.NoError(t, err)
	t.Log("Got the expected minipool addresses")

	// Make the first BLS key
	blsKey, err := testMgr.GetKeyGenerator().GetBlsPrivateKey(0)
	require.NoError(t, err)

	// Build the minipool creation TXs
	prelaunchValueGwei := 1e9
	mainNodeOpts := testMgr.GetMainNodeOpts()
	mainNodeOpts.Value = eth.GweiToWei(prelaunchValueGwei)
	txInfos := make([]*eth.TransactionInfo, minipoolCount)
	for i := 0; i < minipoolCount; i++ {
		// Make the pubkey based on the salt
		salt := salts[i]
		pubkey := beacon.ValidatorPubkey{}
		salt.FillBytes(pubkey[:])

		// Get the withdrawal creds
		expectedAddress := addresses[i]
		withdrawalCreds := validator.GetWithdrawalCredsFromAddress(expectedAddress)

		// Get a signature and increment the node nonce
		sig, err := deployment.GetMinipoolDepositSignature(testMgr.GetMainNodeAddress(), expectedAddress, salt)
		require.NoError(t, err)
		deployment.IncrementSuperNodeNonce(testMgr.GetMainNodeAddress())

		// Make a dummy deposit data
		depositData, err := createDepositData(blsKey, pubkey, withdrawalCreds, beaconCfg.GenesisForkVersion, uint64(prelaunchValueGwei), res.EthNetworkName)
		require.NoError(t, err)
		root := common.Hash(depositData.DepositDataRoot)
		depositDataRoot := root
		blsSig := beacon.ValidatorSignature(depositData.Signature)

		// Make the TX, reusing the dummy deposit data for all validators for speed
		txInfos[i], err = csMgr.SuperNodeAccount.CreateMinipool(
			pubkey,
			blsSig,
			depositDataRoot,
			salt,
			expectedAddress,
			sig,
			mainNodeOpts,
		)
		require.NoError(t, err)
		//testMgr.MineTx(t, txInfos[i], mainNodeOpts, fmt.Sprintf("Created minipool %d", i))
	}

	t.Log("Built the minipool creation TXs")

	// Submit 10 TXs at a time
	batchSize := 10
	mainNodeOpts.Nonce = big.NewInt(1)
	mainNodeOpts.Value = nil
	for start := 0; start < minipoolCount; start += batchSize {
		end := start + batchSize
		if end > minipoolCount {
			end = minipoolCount
		}

		// Submit a batch of TXs
		txInfoBatch := txInfos[start:end]
		submissions := make([]*eth.TransactionSubmission, len(txInfoBatch))
		for i, txInfo := range txInfoBatch {
			submission, _ := eth.CreateTxSubmissionFromInfo(txInfo, nil)
			submission.GasLimit = 2500000 // Hard code to 2.5 million
			/*
				if submission.GasLimit == 0 {
					submission.GasLimit = 2.8e7 // Hard code to 3 million for failed simulations
				}
			*/
			submissions[i] = submission
		}
		txs, err := txMgr.BatchExecuteTransactions(submissions, mainNodeOpts)
		require.NoError(t, err)

		// Mine the block
		err = testMgr.CommitBlock()
		require.NoError(t, err)

		// Wait for the transactions to be mined
		for _, tx := range txs {
			err = txMgr.WaitForTransaction(tx)
			require.NoError(t, err)
		}
		t.Logf("Mined minipool creation TXs %d-%d", start+1, end)
		//mainNodeOpts.Nonce.Add(mainNodeOpts.Nonce, big.NewInt(int64(len(txs))))
	}

	// Get the list of minipools from SNA
	var minipoolAddressesFromSna []common.Address
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.SuperNodeAccount.GetSubNodeMinipools(mc, &minipoolAddressesFromSna, testMgr.GetMainNodeAddress())
		return nil
	}, nil)
	require.NoError(t, err)
	t.Logf("Got %d minipool addresses from the SNA", len(minipoolAddressesFromSna))

	// Verify the minipool addresses
	require.Equal(t, addresses, minipoolAddressesFromSna)
	t.Log("Verified the minipool addresses")
}

// Do some initial sanity checks on the state of Constellation before running a test
// Also sends ETH to the RP deposit pool for convenience
func runPreflightChecks(t *testing.T, bindings *cstestutils.ContractBindings) {
	// Services
	sp := testMgr.GetNode().GetServiceProvider()
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
		From:  testMgr.GetDeployerOpts().From,
		Value: bindings.ProtocolDaoManager.Settings.Deposit.MaximumDepositPoolSize.Get(), // Deposit the maximum amount
	}
	txInfo, err := bindings.DepositPoolManager.Deposit(fundOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, testMgr.GetDeployerOpts(), fmt.Sprintf("Funded the RP deposit pool with %.6f ETH (%s wei)", eth.WeiToEth(fundOpts.Value), fundOpts.Value.String()))
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
		subNodes[i], subnodeAddresses[i], err = createNewNode(testMgr.GetNode(), nodeDir)
		require.NoError(t, err)
		logger.Info(
			"Created subnode",
			slog.Int("index", i+1),
			slog.String("address", subnodeAddresses[i].Hex()),
		)
	}

	// Send the subnodes some ETH
	hd := testMgr.GetNode().GetHyperdriveNode().GetApiClient()
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
	nodes := append([]*cstesting.ConstellationNode{testMgr.GetNode()}, subNodes...)
	addresses := append([]common.Address{testMgr.GetMainNodeAddress()}, subnodeAddresses...)

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
	if maxWethRplRatio != nil {
		txInfo, err := csMgr.WethVault.SetMaxWethRplRatio(maxWethRplRatio, testMgr.GetDeployerOpts())
		submission, err := eth.CreateTxSubmissionFromInfo(txInfo, err)
		require.NoError(t, err)
		submissions = append(submissions, submission)
	}
	if minWethRplRatio != nil {
		txInfo, err := csMgr.RplVault.SetMinWethRplRatio(minWethRplRatio, testMgr.GetDeployerOpts())
		submission, err := eth.CreateTxSubmissionFromInfo(txInfo, err)
		require.NoError(t, err)
		submissions = append(submissions, submission)
	}

	// Submit the transactions
	txs, err := txMgr.BatchExecuteTransactions(submissions, testMgr.GetDeployerOpts())
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

// Set the liquidity reserve percents on the WETH and RPL vaults. Nil means don't change the setting for that vault.
func setLiquidityReservePercents(t *testing.T, sp cscommon.IConstellationServiceProvider, wethVault *big.Int, rplVault *big.Int) {
	// Services
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()
	txMgr := sp.GetTransactionManager()

	submissions := []*eth.TransactionSubmission{}
	if wethVault != nil {
		txInfo, err := csMgr.WethVault.SetLiquidityReservePercent(wethVault, testMgr.GetDeployerOpts())
		submission, err := eth.CreateTxSubmissionFromInfo(txInfo, err)
		require.NoError(t, err)
		submissions = append(submissions, submission)
	}
	if rplVault != nil {
		txInfo, err := csMgr.RplVault.SetLiquidityReservePercent(rplVault, testMgr.GetDeployerOpts())
		submission, err := eth.CreateTxSubmissionFromInfo(txInfo, err)
		require.NoError(t, err)
		submissions = append(submissions, submission)
	}

	// Submit the transactions
	testMgr.GetDeployerOpts().Nonce = nil
	txs, err := txMgr.BatchExecuteTransactions(submissions, testMgr.GetDeployerOpts())
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
			csMgr.WethVault.GetLiquidityReservePercent(mc, &newWethVaultSetting)
		}
		if rplVault != nil {
			csMgr.RplVault.GetLiquidityReservePercent(mc, &newRplVaultSetting)
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
func createXrEthOracleSignature(newTotalYieldAccrued *big.Int, expectedOracleError *big.Int, timestamp time.Time, poaBeaconOracleAddress common.Address, chainID *big.Int, key *ecdsa.PrivateKey) ([]byte, error) {
	amountBytes := [32]byte{}
	newTotalYieldAccrued.FillBytes(amountBytes[:])

	expectedOracleErrorBytes := [32]byte{}
	expectedOracleError.FillBytes(expectedOracleErrorBytes[:])

	timestampBig := big.NewInt(timestamp.Unix())
	timestampBytes := [32]byte{}
	timestampBig.FillBytes(timestampBytes[:])

	chainIdBytes := [32]byte{}
	chainID.FillBytes(chainIdBytes[:])

	message := crypto.Keccak256(
		amountBytes[:],
		expectedOracleErrorBytes[:],
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
func calculateXrEthOracleTotalYieldAccrued(t *testing.T, sp cscommon.IConstellationServiceProvider, bindings *cstestutils.ContractBindings) (*big.Int, *big.Int) {
	// Services
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()

	// Get the total minipool count and minipool launch balance
	var minipoolCountBig *big.Int
	var oracleError *big.Int
	err := qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.SuperNodeAccount.GetMinipoolCount(mc, &minipoolCountBig)
		csMgr.OperatorDistributor.GetOracleError(mc, &oracleError)
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

	return totalRewards, oracleError
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

/*
// Generates a rewards snapshot for the Rewards Pool
func executeRpRewardsInterval(t *testing.T, sp cscommon.IConstellationServiceProvider, bindings *cstestutils.ContractBindings) (map[common.Address]*rewardsInfo, rewards.RewardSubmission, uint64) {
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
	txInfo, err := rplBinding.MintInflationRPL(testMgr.GetOdaoOpts()[0])
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, testMgr.GetOdaoOpts()[0], "Minted RPL inflation")

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
	sender := testMgr.GetOdaoOpts()[0]
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
		testMgr.GetOdaoOpts()[0].From: {
			CollateralRpl:    common.Big0,
			OracleDaoRpl:     odaoAmountPerNode,
			SmoothingPoolEth: common.Big0,
		},
		testMgr.GetOdaoOpts()[1].From: {
			CollateralRpl:    common.Big0,
			OracleDaoRpl:     odaoAmountPerNode,
			SmoothingPoolEth: common.Big0,
		},
		testMgr.GetOdaoOpts()[2].From: {
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
	txInfo, err = rewardsPool.SubmitRewardSnapshot(rewardSnapshot, testMgr.GetOdaoOpts()[0])
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, testMgr.GetOdaoOpts()[0], "Submitted rewards snapshot from ODAO 1")
	txInfo, err = rewardsPool.SubmitRewardSnapshot(rewardSnapshot, testMgr.GetOdaoOpts()[1])
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, testMgr.GetOdaoOpts()[1], "Submitted rewards snapshot from ODAO 2")

	// Ensure the interval was incremented and the snapshot is canon
	err = qMgr.Query(nil, nil, rewardsPool.RewardIndex)
	require.NoError(t, err)
	interval := rewardsPool.RewardIndex.Formatted()
	require.NotEqual(t, oldInterval, interval)
	t.Logf("Interval incremented to %d successfully", interval)

	return rewardsMap, rewardSnapshot, slots
}
*/

/*
type rewardsInfo struct {
	CollateralRpl    *big.Int
	OracleDaoRpl     *big.Int
	SmoothingPoolEth *big.Int
	MerkleData       []byte
	MerkleProof      []common.Hash
}

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
	signature, err := utils.CreateSignature(message, testMgr.GetDeployerKey())
	require.NoError(t, err)

	return &constellation.MerkleRewardsConfig{
		Signature:             signature,
		SignatureGenesisTime:  currentTimeBig,
		AverageEthTreasuryFee: ethTreasuryFee,
		AverageEthOperatorFee: nodeFee,
		AverageRplTreasuryFee: rplTreasuryFee,
	}
}
*/

/*
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
		rplTreasuryFee.Add(rplTreasuryFee, mp.ConstellationData.EthTreasuryFee) // TODO: Figure out how to do this IRL
	}

	// Return the averages
	ethTreasuryFee.Div(ethTreasuryFee, mpCount)
	nodeFee.Div(nodeFee, mpCount)
	rplTreasuryFee.Div(rplTreasuryFee, mpCount)
	return ethTreasuryFee, nodeFee, rplTreasuryFee
}
*/

// Checks if two big.Ints are approximately equal within a small tolerance
func requireApproxEqual(t *testing.T, expected *big.Int, actual *big.Int) {
	t.Helper()
	delta := new(big.Int).Sub(expected, actual)
	delta = delta.Abs(delta)
	tolerance := big.NewInt(5) // 5 wei
	require.True(t, delta.Cmp(tolerance) <= 0, "delta is too high - expected %s, got %s (diff %s)", expected.String(), actual.String(), delta.String())
}

/*
// Checks if two big.Ints are approximately equal within a small tolerance
func requireApproxEqualWithTolerance(t *testing.T, expected *big.Int, actual *big.Int, tolerance *big.Int) {
	t.Helper()
	delta := new(big.Int).Sub(expected, actual)
	delta = delta.Abs(delta)
	require.True(t, delta.Cmp(tolerance) <= 0, "delta is too high - expected %s, got %s (diff %s)", expected.String(), actual.String(), delta.String())
}
*/

// Print information about the current tick
func printTickInfo(t *testing.T, sp cscommon.IConstellationServiceProvider) {
	if !shouldPrintTickInfo {
		return
	}

	// Services
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()

	var nextMinipool common.Address
	err := qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.OperatorDistributor.GetNextMinipool(mc, &nextMinipool)
		return nil
	}, nil)
	require.NoError(t, err)
	t.Logf("Next minipool to tick is %s", nextMinipool.Hex())
}

/*
func calculateNodeOpRewardsFactor(t *testing.T, validatorCount float64, maxValidators float64, k float64) *big.Int {
	// Quick and dirty calculation with float64 math
	x := validatorCount / maxValidators
	val := (math.Pow(math.E, k*(x-1)) - math.Pow(math.E, -k)) / (1 - math.Pow(math.E, -k))
	return eth.EthToWei(val)
}
*/

// Make a custom signed deposit data with the provided pubkey instead of the pubkey for the private key. Will fail validation but useful for testing
func createDepositData(validatorKey *eth2types.BLSPrivateKey, pubkey beacon.ValidatorPubkey, withdrawalCredentials common.Hash, genesisForkVersion []byte, depositAmount uint64, networkName string) (beacon.ExtendedDepositData, error) {
	// Build deposit data
	dd := ssz_types.DepositDataNoSignature{
		PublicKey:             pubkey[:],
		WithdrawalCredentials: withdrawalCredentials[:],
		Amount:                depositAmount,
	}
	domain, err := eth2types.ComputeDomain(eth2types.DomainDeposit, genesisForkVersion, eth2types.ZeroGenesisValidatorsRoot)
	if err != nil {
		return beacon.ExtendedDepositData{}, fmt.Errorf("error computing domain: %w", err)
	}

	// Get signing root
	messageRoot, err := dd.HashTreeRoot()
	if err != nil {
		return beacon.ExtendedDepositData{}, fmt.Errorf("error getting message root: %w", err)
	}
	dataRoot := ssz_types.SigningRoot{
		ObjectRoot: messageRoot[:],
		Domain:     domain,
	}

	// Get signing root with domain
	dataRootHash, err := dataRoot.HashTreeRoot()
	if err != nil {
		return beacon.ExtendedDepositData{}, err
	}

	// Build deposit data struct (with signature)
	var depositData = ssz_types.DepositData{
		PublicKey:             dd.PublicKey,
		WithdrawalCredentials: dd.WithdrawalCredentials,
		Amount:                dd.Amount,
		Signature:             validatorKey.Sign(dataRootHash[:]).Marshal(),
	}

	// Get deposit data root
	depositDataRoot, err := depositData.HashTreeRoot()
	if err != nil {
		return beacon.ExtendedDepositData{}, err
	}

	// Create the extended data
	return beacon.ExtendedDepositData{
		PublicKey:             depositData.PublicKey,
		WithdrawalCredentials: depositData.WithdrawalCredentials,
		Amount:                depositData.Amount,
		Signature:             depositData.Signature,
		DepositMessageRoot:    messageRoot[:],
		DepositDataRoot:       depositDataRoot[:],
		ForkVersion:           genesisForkVersion,
		NetworkName:           networkName,
	}, nil
}
