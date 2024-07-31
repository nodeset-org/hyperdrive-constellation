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
	cstestutils "github.com/nodeset-org/hyperdrive-constellation/internal/tests/utils"
	cstesting "github.com/nodeset-org/hyperdrive-constellation/testing"
	hdtesting "github.com/nodeset-org/hyperdrive-daemon/testing"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
	"github.com/stretchr/testify/require"
)

var (
	standardSalt *big.Int = big.NewInt(0x90de5e7)
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

	// Deposit RPL to the RPL vault
	rplAmount := eth.EthToWei(1225)
	cstestutils.DepositToRplVault(t, testMgr, bindings.RplVault, bindings.Rpl, rplAmount, deployerOpts)

	// Deposit WETH to the WETH vault
	wethAmount := eth.EthToWei(10)
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
	require.NoError(t, err)
	t.Log("Created bindings")

	// Create some subnodes
	nodes, _, err := createNodesForTest(t, 14, eth.EthToWei(50))
	require.NoError(t, err)

	// Make sure the contract state is clean
	runPreflightChecks(t, bindings)

	// Deposit RPL to the RPL vault
	rplAmount := eth.EthToWei(4000)
	cstestutils.DepositToRplVault(t, testMgr, bindings.RplVault, bindings.Rpl, rplAmount, deployerOpts)

	// Deposit WETH to the WETH vault
	wethAmount := eth.EthToWei(100)
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

	// Build the wave 3 minipool creation TXs
	wave3Nodes := nodes[10:15]
	wave3Salts := salts[10:15]
	_, wave3Hashes := cstestutils.BuildAndSubmitCreateMinipoolTxs(t, wave3Nodes, 1, wave3Salts, bindings.RpSuperNode)

	// Mine a block
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined a block")

	// Wave 3 should fail
	for _, hashesPerNode := range wave3Hashes {
		_, err = hd.Tx.WaitForTransaction(hashesPerNode[0])
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed with status 0")
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

	// Deposit RPL to the RPL vault
	rplAmount := eth.EthToWei(4000)
	cstestutils.DepositToRplVault(t, testMgr, bindings.RplVault, bindings.Rpl, rplAmount, deployerOpts)

	// Deposit WETH to the WETH vault
	wethAmount := eth.EthToWei(100)
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

	wave3Nodes := nodes[10:15]
	wave3Salts := salts[10:15]
	_, wave3CreationHashes := cstestutils.BuildAndSubmitCreateMinipoolTxs(t, wave3Nodes, 1, wave3Salts, bindings.RpSuperNode)

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

	// Wave 3 creation should fail
	for _, hashesPerNode := range wave3CreationHashes {
		_, err = hd.Tx.WaitForTransaction(hashesPerNode[0])
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed with status 0")
	}
	t.Log("Wave 3 creation failed as expected")
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
