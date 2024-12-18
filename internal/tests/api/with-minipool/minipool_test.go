package with_minipool

import (
	"context"
	"math/big"
	"runtime/debug"
	"strconv"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	cstestutils "github.com/nodeset-org/hyperdrive-constellation/internal/tests/utils"
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	cstasks "github.com/nodeset-org/hyperdrive-constellation/tasks"
	hdtesting "github.com/nodeset-org/hyperdrive-daemon/testing"
	batch "github.com/rocket-pool/batch-query"
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
	_, err = cs.Minipool.Create(standardSalt, false, false)
	require.Error(t, err)
	t.Logf("Failed to create minipool with duplicate salt as expected: %v", err)
}

func TestSkipLiquidityCheck(t *testing.T) {
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
	sp := mainNode.GetServiceProvider()
	csMgr := sp.GetConstellationManager()
	cs := mainNode.GetApiClient()

	// Set max validators to 2
	txInfo, err := csMgr.SuperNodeAccount.SetMaxValidators(common.Big2, deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, deployerOpts, "Set max validators to 2")

	// Create without liquidity check
	response, err := cs.Minipool.Create(big.NewInt(2), true, false)
	require.NoError(t, err)
	require.True(t, response.Data.CanCreate)
	require.False(t, response.Data.InsufficientLiquidity)
	t.Logf("MP create succeeded with skip-liquidity check on")

	// Create with liquidity check
	response, err = cs.Minipool.Create(big.NewInt(2), false, false)
	require.NoError(t, err)
	require.False(t, response.Data.CanCreate)
	require.True(t, response.Data.InsufficientLiquidity)
	t.Logf("MP create failed with skip-liquidity check off")
}

func TestSkipBalanceCheck(t *testing.T) {
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
	hd := mainNode.GetHyperdriveNode().GetApiClient()
	ec := sp.GetEthClient()

	// Deposit RPL to the RPL vault
	rplAmount := eth.EthToWei(3200)
	cstestutils.DepositToRplVault(t, testMgr, csMgr.RplVault, bindings.Rpl, rplAmount, deployerOpts)

	// Deposit WETH to the WETH vault
	wethAmount := eth.EthToWei(90)
	cstestutils.DepositToWethVault(t, testMgr, csMgr.WethVault, bindings.Weth, wethAmount, deployerOpts)

	// Set max validators to 2
	txInfo, err := csMgr.SuperNodeAccount.SetMaxValidators(common.Big2, deployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, deployerOpts, "Set max validators to 2")

	// Send all the node's ETH to the deployer
	nodeBalance, err := ec.BalanceAt(context.Background(), harness.MainNodeAddress, nil)
	require.NoError(t, err)
	amount := new(big.Int).Sub(nodeBalance, eth.EthToWei(0.5))
	sendResponse, err := hd.Wallet.Send(amount, "eth", deployerOpts.From)
	require.NoError(t, err)
	err = testMgr.MineTxViaHyperdrive(hd, sendResponse.Data.TxInfo)
	require.NoError(t, err)

	// Create without balance check
	response, err := cs.Minipool.Create(common.Big2, false, true)
	require.NoError(t, err)
	require.True(t, response.Data.CanCreate)
	require.False(t, response.Data.InsufficientBalance)
	t.Logf("MP create succeeded with skip-balance check on")

	// Create with balance check
	response, err = cs.Minipool.Create(common.Big2, false, false)
	require.NoError(t, err)
	require.False(t, response.Data.CanCreate)
	require.True(t, response.Data.InsufficientBalance)
	t.Logf("MP create failed with skip-balance check off")
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

// Check if the signed exit upload task works as expected after a manual upload
func TestSignedExitUpload_TaskAfterManual(t *testing.T) {
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
	csMgr := sp.GetConstellationManager()
	cs := mainNode.GetApiClient()

	// Make sure MP details are populated
	mpCommon := mp.Common()
	err = qMgr.Query(nil, nil,
		mpCommon.Pubkey,
		mpCommon.WithdrawalCredentials,
	)
	require.NoError(t, err)

	// Make a 2nd minipool
	txInfo, err := csMgr.SuperNodeAccount.SetMaxValidators(common.Big2, harness.DeployerOpts)
	require.NoError(t, err)
	testMgr.MineTx(t, txInfo, harness.DeployerOpts, "Set max validators to 2")

	// Get the deposit amounts
	wethAmount, rplAmount := getDepositAmounts(2)

	// Deposit to the vaults
	cstestutils.DepositToRplVault(t, testMgr, csMgr.RplVault, harness.Bindings.Rpl, rplAmount, harness.DeployerOpts)
	cstestutils.DepositToWethVault(t, testMgr, csMgr.WethVault, harness.Bindings.Weth, wethAmount, harness.DeployerOpts)

	// Make another minipool
	salt2 := big.NewInt(0x90de5e702)
	mp2 := cstestutils.CreateMinipool(t, testMgr, mainNode, mainNodeAddress, salt2, harness.Bindings.RpSuperNode, harness.Bindings.MinipoolManager)
	mp2Common := mp2.Common()
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		mp2Common.Pubkey.AddToQuery(mc)
		mp2Common.WithdrawalCredentials.AddToQuery(mc)
		return nil
	}, nil)
	require.NoError(t, err)

	// Set up the NS Mock
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	nsDB := nsMgr.GetDatabase()
	deployment := nsDB.Constellation.GetDeployment(res.DeploymentName)
	pubkey := mpCommon.Pubkey.Get()
	pubkey2 := mp2Common.Pubkey.Get()
	deployment.SetValidatorInfoForMinipool(mpCommon.Address, pubkey)
	deployment.SetValidatorInfoForMinipool(mp2Common.Address, pubkey2)

	// Make a task
	logger := sp.GetTasksLogger()
	ctx := logger.CreateContextWithLogger(sp.GetBaseContext())
	snapshotTask := cstasks.NewNetworkSnapshotTask(ctx, sp, logger)
	exitTask := cstasks.NewSubmitSignedExitsTask(ctx, sp, logger)

	// Run the task to populate the initial cache
	walletResponse, err := hd.Wallet.Status()
	require.NoError(t, err)
	walletStatus := walletResponse.Data.WalletStatus
	snapshot, err := snapshotTask.Run(&walletStatus)
	require.NoError(t, err)
	err = exitTask.Run(snapshot)
	require.NoError(t, err)

	// Make sure a signed exit hasn't been uploaded yet for either MP
	nsNode, _ := nsDB.Core.GetNode(mainNodeAddress)
	nsValidator := deployment.GetValidator(nsNode, pubkey)
	require.Nil(t, nsValidator.GetExitMessage())
	nsValidator2 := deployment.GetValidator(nsNode, pubkey2)
	require.Nil(t, nsValidator2.GetExitMessage())
	t.Logf("Minipool 1 and 2 require signed exits as expected")

	// Add them to Beacon
	val1, err := bn.AddValidator(pubkey, mpCommon.WithdrawalCredentials.Get())
	require.NoError(t, err)
	_, err = bn.AddValidator(pubkey2, mp2Common.WithdrawalCredentials.Get())
	require.NoError(t, err)

	// Do a manual upload for the 1st MP
	exitInfo := csapi.MinipoolValidatorInfo{
		Address: mpCommon.Address,
		Pubkey:  mpCommon.Pubkey.Get(),
		Index:   strconv.FormatUint(val1.Index, 10),
	}
	_, err = cs.Minipool.UploadSignedExits([]csapi.MinipoolValidatorInfo{exitInfo})
	require.NoError(t, err)

	// Check the status again
	require.NotNil(t, nsValidator.GetExitMessage())
	require.Nil(t, nsValidator2.GetExitMessage())
	t.Logf("Minipool 1 no longer requires signed exit but minipool 2 still does as expected")

	// Run the task again
	snapshot, err = snapshotTask.Run(&walletStatus)
	require.NoError(t, err)
	err = exitTask.Run(snapshot)
	require.NoError(t, err)

	// Both minipools should now have signed exits
	require.NotNil(t, nsValidator.GetExitMessage())
	require.NotNil(t, nsValidator2.GetExitMessage())
	t.Logf("Minipools 1 and 2 no longer require signed exits as expected")
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
