package multi_node

import (
	"fmt"
	"log/slog"
	"math/big"
	"path/filepath"
	"runtime/debug"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	cstestutils "github.com/nodeset-org/hyperdrive-constellation/internal/tests/utils"
	cstesting "github.com/nodeset-org/hyperdrive-constellation/testing"
	hdtesting "github.com/nodeset-org/hyperdrive-daemon/testing"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
	"github.com/stretchr/testify/require"
)

// Run test 3 of the QA suite
func Test3_ComplexRoundTrip(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

	// Get some services
	bindings, err := cstestutils.CreateBindings(mainNode.GetServiceProvider())
	require.NoError(t, err)
	sp := mainNode.GetServiceProvider()
	qMgr := sp.GetQueryManager()
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
	datas, hashes := cstestutils.BuildAndSubmitCreateMinipoolTxs(t, nodes, 1, nil, bindings.RpSuperNode)

	// Mine a block
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined a block")

	// Wait for each TX
	for i, node := range nodes {
		hd := node.GetHyperdriveNode().GetApiClient()
		_, err = hd.Tx.WaitForTransaction(hashes[i][0])
		require.NoError(t, err)
	}
	t.Log("Finished waiting for minipool creation TXs")

	// Save the pubkeys
	for i, node := range nodes {
		cstestutils.SaveValidatorKey(t, node, datas[i][0])
	}
	t.Log("Saved the validator keys")

	// Verify minipools
	mps := make([]minipool.IMinipool, len(nodes))
	for i, dataForNode := range datas {
		data := dataForNode[0]
		mp := cstestutils.VerifyMinipoolAfterCreation(t, qMgr, bindings.RpSuperNode, uint64(i), data.MinipoolAddress, bindings.MinipoolManager)
		mps[i] = mp
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

	// TODO: examine rewards
}

// Run test 4 of the QA suite
func Test4_SimpleNOConcurrency(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

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
	defer nodeset_cleanup(snapshotName)

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
	wave3Salts := salts[5:10]
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

// Run test 15 of the QA suite
func Test15_StakingTest(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

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
	nodes, _, err := createNodesForTest(t, 14, eth.EthToWei(50))
	require.NoError(t, err)

	// Make sure the contract state is clean
	runPreflightChecks(t, bindings)

	// Get the deposit amounts
	wethAmount, rplAmount := getDepositAmounts(t, bindings, sp, 10) // Enough for 10 minipools

	// Deposit RPL to the RPL vault
	cstestutils.DepositToRplVault(t, testMgr, bindings.RplVault, bindings.Rpl, rplAmount, deployerOpts)

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

	// Save the wave 1 pubkeys
	for i, node := range wave1Nodes {
		cstestutils.SaveValidatorKey(t, node, wave1Data[i][0])
	}
	t.Log("Saved wave 1 validator keys")

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

	// Save the wave 2 pubkeys
	for i, node := range wave2Nodes {
		cstestutils.SaveValidatorKey(t, node, wave2Data[i][0])
	}
	t.Log("Saved wave 2 validator keys")

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
	testMgr.MineTx(t, txInfo, deployerOpts, "Funded the RP deposit pool")
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

// Cleanup after a unit test
func nodeset_cleanup(snapshotName string) {
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

	// Reload the SW wallet to undo any changes made during the test
	err = mainNode.GetServiceProvider().GetWallet().Reload()
	if err != nil {
		fail("Error reloading constellation wallet: %v", err)
	}
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
	collateralBase := big.NewInt(1e5)
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
