package multi_node

import (
	"math/big"
	"runtime/debug"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	cstestutils "github.com/nodeset-org/hyperdrive-constellation/internal/tests/utils"
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
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
func TestComplexRoundTrip(t *testing.T) {
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
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()
	t.Log("Created services")

	// Create some subnodes
	nodes := []*cstesting.ConstellationNode{mainNode}
	nodeAddresses := []common.Address{mainNodeAddress}
	subnodes, subAddresses, err := createSubNodes(mainNode, 4, eth.EthToWei(100))
	require.NoError(t, err)
	nodes = append(nodes, subnodes...)
	nodeAddresses = append(nodeAddresses, subAddresses...)

	// Register the nodes with Constellation
	for _, node := range nodes {
		cstestutils.RegisterWithConstellation(t, testMgr, node)
	}

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
	datas := make([]*csapi.MinipoolCreateData, len(nodes))
	for i, node := range nodes {
		salt := big.NewInt(int64(i))
		data := cstestutils.BuildAndVerifyCreateMinipoolTx(t, node, salt, bindings.RpSuperNode)
		datas[i] = data
	}
	t.Log("Built minipool creation TXs")

	// Submit each TX
	hashes := make([]common.Hash, len(nodes))
	for i, node := range nodes {
		hd := node.GetHyperdriveNode().GetApiClient()
		data := datas[i]
		submission, _ := eth.CreateTxSubmissionFromInfo(data.TxInfo, nil)
		response, err := hd.Tx.SubmitTx(submission, nil, eth.GweiToWei(10), eth.GweiToWei(0.5))
		require.NoError(t, err)
		hashes[i] = response.Data.TxHash
	}
	t.Log("Submitted minipool creation TXs")

	// Mine a block
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined a block")

	// Wait for each TX
	for i, node := range nodes {
		hd := node.GetHyperdriveNode().GetApiClient()
		_, err = hd.Tx.WaitForTransaction(hashes[i])
		require.NoError(t, err)
	}
	t.Log("Finished waiting for minipool creation TXs")

	// Save the pubkeys
	for i, node := range nodes {
		cstestutils.SaveValidatorKey(t, node, datas[i])
	}
	t.Log("Saved the validator keys")

	// Verify minipools
	mps := make([]minipool.IMinipool, len(nodes))
	for i, data := range datas {
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
	sp := mainNode.GetServiceProvider()
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()
	t.Log("Created services")

	// Create some subnodes
	nodes := []*cstesting.ConstellationNode{mainNode}
	nodeAddresses := []common.Address{mainNodeAddress}
	subnodes, subAddresses, err := createSubNodes(mainNode, 1, eth.EthToWei(100))
	require.NoError(t, err)
	nodes = append(nodes, subnodes...)
	nodeAddresses = append(nodeAddresses, subAddresses...)

	// Register the nodes with Constellation
	for _, node := range nodes {
		cstestutils.RegisterWithConstellation(t, testMgr, node)
	}

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

	// Deposit RPL to the RPL vault
	rplAmount := eth.EthToWei(1225)
	cstestutils.DepositToRplVault(t, testMgr, bindings.RplVault, bindings.Rpl, rplAmount, deployerOpts)

	// Deposit WETH to the WETH vault
	wethAmount := eth.EthToWei(10)
	cstestutils.DepositToWethVault(t, testMgr, bindings.WethVault, bindings.Weth, wethAmount, deployerOpts)

	// Set the available minipool count for the user
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	err = nsMgr.SetAvailableConstellationMinipoolCount(nsEmail, 1)
	require.NoError(t, err)
	t.Log("Set up the NodeSet mock server")

	// Build the minipool creation TXs
	datas := make([]*csapi.MinipoolCreateData, len(nodes))
	for i, node := range nodes {
		salt := big.NewInt(int64(i))
		data := cstestutils.BuildAndVerifyCreateMinipoolTx(t, node, salt, bindings.RpSuperNode)
		datas[i] = data
	}
	t.Log("Built minipool creation TXs")

	// Submit each TX
	hashes := make([]common.Hash, len(nodes))
	for i, node := range nodes {
		hd := node.GetHyperdriveNode().GetApiClient()
		data := datas[i]
		submission, _ := eth.CreateTxSubmissionFromInfo(data.TxInfo, nil)
		response, err := hd.Tx.SubmitTx(submission, nil, eth.GweiToWei(10), eth.GweiToWei(0.5))
		require.NoError(t, err)
		hashes[i] = response.Data.TxHash
	}
	t.Log("Submitted minipool creation TXs")

	// Mine a block
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined a block")

	// The first one should succeed
	hd := mainNode.GetHyperdriveNode().GetApiClient()
	_, err = hd.Tx.WaitForTransaction(hashes[0])
	require.NoError(t, err)
	t.Log("First minipool creation TX succeeded")

	// The second one should fail
	_, err = hd.Tx.WaitForTransaction(hashes[1])
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed with status 0")
	t.Logf("Second minipool creation TX failed as expected: %v", err)
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