package with_minipool

import (
	"runtime/debug"
	"testing"

	cstestutils "github.com/nodeset-org/hyperdrive-constellation/internal/tests/utils"
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	cstasks "github.com/nodeset-org/hyperdrive-constellation/tasks"
	hdtesting "github.com/nodeset-org/hyperdrive-daemon/testing"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/stretchr/testify/require"
)

// Run a check to make sure depositing with duplicate salts fails
func TestDuplicateSalts(t *testing.T) {
	// Take a snapshot, revert at the end
	testMgr := harness.TestManager
	mainNode := harness.MainNode
	deployerOpts := harness.DeployerOpts
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

	// Get some services
	bindings := harness.Bindings
	sp := mainNode.GetServiceProvider()
	csMgr := sp.GetConstellationManager()
	cs := mainNode.GetApiClient()

	// Deposit RPL to the RPL vault
	rplAmount := eth.EthToWei(3200)
	cstestutils.DepositToRplVault(t, testMgr, csMgr.RplVault, bindings.Rpl, rplAmount, deployerOpts)

	// Deposit WETH to the WETH vault
	wethAmount := eth.EthToWei(90)
	cstestutils.DepositToWethVault(t, testMgr, csMgr.WethVault, bindings.Weth, wethAmount, deployerOpts)

	// Try making another one with the same salt, it should fail
	_, err = cs.Minipool.Create(standardSalt)
	require.Error(t, err)
	t.Logf("Failed to create minipool with duplicate salt as expected: %v", err)
}

// Check if the manual signed exit upload command works as expected
func TestSignedExitUpload_Manual(t *testing.T) {
	// Take a snapshot, revert at the end
	testMgr := harness.TestManager
	mainNode := harness.MainNode
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

	// Get some services
	sp := mainNode.GetServiceProvider()
	qMgr := sp.GetQueryManager()
	cs := mainNode.GetApiClient()
	res := sp.GetResources()
	bn := testMgr.GetBeaconMockManager()

	// Make sure MP details are populated
	mpCommon := mp.Common()
	err = qMgr.Query(nil, nil,
		mpCommon.Pubkey,
		mpCommon.WithdrawalCredentials,
	)
	require.NoError(t, err)

	// Set up the NS Mock
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	nsDB := nsMgr.GetDatabase()
	deployment := nsDB.Constellation.GetDeployment(res.DeploymentName)
	pubkey := mpCommon.Pubkey.Get()
	deployment.SetValidatorInfoForMinipool(mpCommon.Address, pubkey)
	_, err = bn.AddValidator(pubkey, mpCommon.WithdrawalCredentials.Get())
	require.NoError(t, err)

	// Make sure a signed exit hasn't been uploaded yet, but the validator is there
	statusResponse, err := cs.Minipool.Status()
	require.NoError(t, err)
	require.Len(t, statusResponse.Data.Minipools, 1)
	mpStatus := statusResponse.Data.Minipools[0]
	require.True(t, mpStatus.RequiresSignedExit)
	t.Logf("Minipool requires signed exit as expected")

	// Send one manually
	exitInfo := csapi.MinipoolValidatorInfo{
		Address: mpCommon.Address,
		Pubkey:  mpCommon.Pubkey.Get(),
		Index:   mpStatus.Validator.Index,
	}
	_, err = cs.Minipool.UploadSignedExits([]csapi.MinipoolValidatorInfo{exitInfo})
	require.NoError(t, err)

	// Check the status again
	statusResponse, err = cs.Minipool.Status()
	require.NoError(t, err)
	require.Len(t, statusResponse.Data.Minipools, 1)
	mpStatus = statusResponse.Data.Minipools[0]
	require.False(t, mpStatus.RequiresSignedExit)
	t.Logf("Minipool no longer requires signed exit as expected")
}

// Check if the signed exit upload task works as expected
func TestSignedExitUpload_Task(t *testing.T) {
	// Take a snapshot, revert at the end
	testMgr := harness.TestManager
	mainNode := harness.MainNode
	mainNodeAddress := harness.MainNodeAddress
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

	// Get some services
	sp := mainNode.GetServiceProvider()
	qMgr := sp.GetQueryManager()
	hd := mainNode.GetHyperdriveNode().GetApiClient()
	res := sp.GetResources()
	bn := testMgr.GetBeaconMockManager()

	// Make sure MP details are populated
	mpCommon := mp.Common()
	err = qMgr.Query(nil, nil,
		mpCommon.Pubkey,
		mpCommon.WithdrawalCredentials,
	)
	require.NoError(t, err)

	// Set up the NS Mock
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	nsDB := nsMgr.GetDatabase()
	deployment := nsDB.Constellation.GetDeployment(res.DeploymentName)
	pubkey := mpCommon.Pubkey.Get()
	deployment.SetValidatorInfoForMinipool(mpCommon.Address, pubkey)
	_, err = bn.AddValidator(pubkey, mpCommon.WithdrawalCredentials.Get())
	require.NoError(t, err)

	// Make a task
	logger := sp.GetTasksLogger()
	ctx := logger.CreateContextWithLogger(sp.GetBaseContext())
	snapshotTask := cstasks.NewNetworkSnapshotTask(ctx, sp, logger)
	exitTask := cstasks.NewSubmitSignedExitsTask(ctx, sp, logger)

	// Make sure a signed exit hasn't been uploaded yet
	nsNode, _ := nsDB.Core.GetNode(mainNodeAddress)
	nsValidator := deployment.GetValidator(nsNode, pubkey)
	require.Nil(t, nsValidator.GetExitMessage())
	t.Logf("Minipool requires signed exit as expected")

	// Run the task
	walletResponse, err := hd.Wallet.Status()
	require.NoError(t, err)
	walletStatus := walletResponse.Data.WalletStatus
	snapshot, err := snapshotTask.Run(&walletStatus)
	require.NoError(t, err)
	err = exitTask.Run(snapshot)
	require.NoError(t, err)

	// Check the status again
	require.NotNil(t, nsValidator.GetExitMessage())
	t.Logf("Minipool no longer requires signed exit as expected")
}

// Cleanup after a unit test
func nodeset_cleanup(snapshotName string) {
	testMgr := harness.TestManager
	mainNode := harness.MainNode

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
