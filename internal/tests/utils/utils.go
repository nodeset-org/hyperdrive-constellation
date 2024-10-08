package cstestutils

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/nodeset-org/hyperdrive-constellation/common/contracts"
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	cstesting "github.com/nodeset-org/hyperdrive-constellation/testing"
	"github.com/nodeset-org/nodeset-client-go/server-mock/db"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/node-manager-core/log"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
	"github.com/rocket-pool/rocketpool-go/v2/node"
	"github.com/rocket-pool/rocketpool-go/v2/tokens"
	"github.com/rocket-pool/rocketpool-go/v2/types"
	"github.com/stretchr/testify/require"
)

// =================
// === Without T ===
// =================

// Deposits RPL to the RPL vault and verifies the contract balances have been updated
func DepositToRplVaultBeforeTest(testHarness *StandardTestHarness, rplVault contracts.IErc4626Token, rpl *tokens.TokenRpl, amount *big.Int, opts *bind.TransactOpts) error {
	// Bindings
	testMgr := testHarness.TestManager
	logger := testHarness.Logger
	csNode := testMgr.GetNode()
	sp := csNode.GetServiceProvider()
	qMgr := sp.GetQueryManager()
	csMgr := sp.GetConstellationManager()

	// Get the xRPL balance before depositing
	var xRplBalance *big.Int
	err := qMgr.Query(func(mc *batch.MultiCaller) error {
		rplVault.BalanceOf(mc, &xRplBalance, opts.From)
		return nil
	}, nil)
	if err != nil {
		return err
	}

	// Deposit RPL to the RPL vault
	err = testMgr.Constellation_DepositToRplVault(rplVault, amount, opts, opts)
	if err != nil {
		return err
	}
	logger.Info(fmt.Sprintf("Deposited %.6f RPL into the RPL vault", eth.WeiToEth(amount)))

	// Verify balances have been updated
	var odRplBalance *big.Int
	var rvRplBalance *big.Int
	var newXrplBalance *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		rpl.BalanceOf(mc, &odRplBalance, csMgr.OperatorDistributor.Address)
		rpl.BalanceOf(mc, &rvRplBalance, rplVault.Address())
		rplVault.BalanceOf(mc, &newXrplBalance, opts.From)
		return nil
	}, nil)
	if err != nil {
		return err
	}
	if odRplBalance.Cmp(common.Big0) != 1 {
		logger.Error("OperatorDistributor's RPL balance is not greater than 0")
	}
	logger.Info(fmt.Sprintf("OperatorDistributor's RPL balance is now %.6f (%s wei)", eth.WeiToEth(odRplBalance), odRplBalance.String()))
	if rvRplBalance.Cmp(common.Big0) != 1 {
		logger.Error("RPL vault's RPL balance is not greater than 0")
	}
	logger.Info(fmt.Sprintf("RPL vault's RPL balance is now %.6f (%s wei)", eth.WeiToEth(rvRplBalance), rvRplBalance.String()))
	if newXrplBalance.Cmp(xRplBalance) != 1 {
		logger.Error("Sender's xRPL balance did not go up")
	}
	logger.Info(fmt.Sprintf("Sender's xRPL balance went up from %.6f (%s wei) to %.6f (%s wei)",
		eth.WeiToEth(xRplBalance), xRplBalance.String(),
		eth.WeiToEth(newXrplBalance), newXrplBalance.String(),
	))
	return nil
}

// Deposits WETH to the WETH vault and verifies the contract balances have been updated
func DepositToWethVaultBeforeTest(testHarness *StandardTestHarness, wethVault contracts.IErc4626Token, weth *contracts.Weth, amount *big.Int, opts *bind.TransactOpts) error {
	// Bindings
	testMgr := testHarness.TestManager
	logger := testHarness.Logger
	sp := testMgr.GetNode().GetServiceProvider()
	qMgr := sp.GetQueryManager()
	csMgr := sp.GetConstellationManager()
	ec := sp.GetEthClient()

	err := testMgr.Constellation_DepositToWethVault(weth, wethVault, amount, opts)
	if err != nil {
		return err
	}
	logger.Info(fmt.Sprintf("Deposited %.6f WETH into the WETH vault", eth.WeiToEth(amount)))

	// Verify OperatorDistributor WETH balance has been updated
	odEthBalance, err := ec.BalanceAt(context.Background(), csMgr.OperatorDistributor.Address, nil)
	if err != nil {
		return err
	}
	var evWethBalance *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		weth.BalanceOf(mc, &evWethBalance, wethVault.Address())
		return nil
	}, nil)
	if err != nil {
		return err
	}
	if odEthBalance.Cmp(common.Big0) != 1 {
		logger.Error("OperatorDistributor's ETH balance is not greater than 0")
	}
	logger.Info(fmt.Sprintf("OperatorDistributor's ETH balance is now %.6f (%s wei)", eth.WeiToEth(odEthBalance), odEthBalance.String()))
	if evWethBalance.Cmp(common.Big0) != 1 {
		logger.Error("WETH vault's WETH balance is not greater than 0")
	}
	logger.Info(fmt.Sprintf("WETH vault's WETH balance is now %.6f (%s wei)", eth.WeiToEth(evWethBalance), evWethBalance.String()))
	return nil
}

// Registers the node with Constellation, ensuring it wasn't previously registered beforehand
func RegisterWithConstellationBeforeTest(testHarness *StandardTestHarness, csNode *cstesting.ConstellationNode) {
	// Bindings
	testMgr := testHarness.TestManager
	logger := testHarness.Logger
	cs := csNode.GetApiClient()

	// Check if the node is registered
	statusResponse, err := cs.Node.GetRegistrationStatus()
	if err != nil {
		logger.Error("Error getting registration status", log.Err(err))
	}
	if statusResponse.Data.Registered {
		logger.Error("Node is already registered with Constellation")
	}
	logger.Info("Node is not registered with Constellation yet, as expected")

	// Register the node
	response, err := cs.Node.Register()
	if err != nil {
		logger.Error("Error registering node with Constellation", log.Err(err))
	}
	if response.Data.NotAuthorized {
		logger.Error("Node is not authorized to register with Constellation")
	}
	if response.Data.NotRegisteredWithNodeSet {
		logger.Error("Node is not registered with a NodeSet")
	}
	if !response.Data.TxInfo.SimulationResult.IsSimulated {
		logger.Error("TX is not simulated")
	}
	if response.Data.TxInfo.SimulationResult.SimulationError != "" {
		logger.Error("TX simulation failed", slog.String("error", response.Data.TxInfo.SimulationResult.SimulationError))
	}
	err = testMgr.MineTxViaHyperdrive(csNode.GetHyperdriveNode().GetApiClient(), response.Data.TxInfo)
	if err != nil {
		logger.Error("Error mining registration TX", log.Err(err))
	}
	logger.Info("Mined the registration TX")

	// Check if the node is registered
	statusResponse, err = cs.Node.GetRegistrationStatus()
	if err != nil {
		logger.Error("Error getting registration status", log.Err(err))
	}
	if !statusResponse.Data.Registered {
		logger.Error("Node is not registered with Constellation")
	}
	logger.Info("Node is now registered with Constellation")
}

// Deposits into Constellation, creating a new minipool
func CreateMinipoolBeforeTest(testHarness *StandardTestHarness, csNode *cstesting.ConstellationNode, nodeAddress common.Address, salt *big.Int, rpSuperNode *node.Node, mpMgr *minipool.MinipoolManager) (minipool.IMinipool, error) {
	// Bindings
	testMgr := testHarness.TestManager
	logger := testHarness.Logger
	sp := csNode.GetServiceProvider()
	qMgr := sp.GetQueryManager()

	// Check the Supernode minipool count
	err := qMgr.Query(nil, nil, rpSuperNode.MinipoolCount)
	if err != nil {
		return nil, err
	}
	previousMpCount := rpSuperNode.MinipoolCount.Formatted()
	logger.Info(fmt.Sprintf("Supernode has %d minipools", previousMpCount))

	// Make the minipool
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	nsDB := nsMgr.GetDatabase()
	res := sp.GetResources()
	deployment := nsDB.Constellation.GetDeployment(res.DeploymentName)
	data, err := BuildAndVerifyCreateMinipoolTxBeforeTest(logger, deployment, csNode, nodeAddress, salt, rpSuperNode, true)
	if err != nil {
		return nil, err
	}
	err = testMgr.MineTxViaHyperdrive(csNode.GetHyperdriveNode().GetApiClient(), data.TxInfo)
	if err != nil {
		return nil, err
	}

	// Save the key
	err = SaveValidatorKeyBeforeTest(logger, csNode, data)
	if err != nil {
		return nil, err
	}

	// Check the Supernode minipool count
	err = qMgr.Query(nil, nil, rpSuperNode.MinipoolCount)
	if err != nil {
		return nil, err
	}
	newMpCount := rpSuperNode.MinipoolCount.Formatted()
	if newMpCount-previousMpCount != 1 {
		return nil, fmt.Errorf("Supernode minipool count did not increase by 1")
	}
	logger.Info(fmt.Sprintf("Supernode now has %d minipools", newMpCount))

	// Verify the minipool
	mp, err := VerifyMinipoolAfterCreationBeforeTest(logger, qMgr, rpSuperNode, newMpCount-1, data.MinipoolAddress, mpMgr)
	if err != nil {
		return nil, err
	}
	return mp, nil
}

// Creates the TX for creating a new minipool, and verifies it simulated successfully
func BuildAndVerifyCreateMinipoolTxBeforeTest(logger *slog.Logger, deployment *db.ConstellationDeployment, csNode *cstesting.ConstellationNode, nodeAddress common.Address, salt *big.Int, rpSuperNode *node.Node, shouldSucceed bool) (*csapi.MinipoolCreateData, error) {
	// Bindings
	cs := csNode.GetApiClient()

	// Make the minipool
	depositResponse, err := cs.Minipool.Create(salt)
	if err != nil {
		return nil, err
	}
	if !depositResponse.Data.CanCreate {
		return nil, fmt.Errorf("Can't create minipool")
	}
	if shouldSucceed {
		if !depositResponse.Data.TxInfo.SimulationResult.IsSimulated {
			return nil, fmt.Errorf("TX is not simulated")
		}
		if depositResponse.Data.TxInfo.SimulationResult.SimulationError != "" {
			return nil, fmt.Errorf("TX simulation failed: %s", depositResponse.Data.TxInfo.SimulationResult.SimulationError)
		}
	}
	logger.Info(fmt.Sprintf("Using salt 0x%s, MP address = %s", salt.Text(16), depositResponse.Data.MinipoolAddress.Hex()))

	// Increment the nonce for the node
	deployment.IncrementSuperNodeNonce(nodeAddress)
	return depositResponse.Data, nil
}

// Saves the validator key created as part of a minipool creation TX to disk
func SaveValidatorKeyBeforeTest(logger *slog.Logger, csNode *cstesting.ConstellationNode, data *csapi.MinipoolCreateData) error {
	// Bindings
	cs := csNode.GetApiClient()

	// Save the key
	pubkey := data.ValidatorPubkey
	index := data.Index
	_, err := cs.Wallet.CreateValidatorKey(pubkey, index, 1)
	if err != nil {
		return fmt.Errorf("error creating validator key: %w", err)
	}
	logger.Info(fmt.Sprintf("Saved validator key for pubkey %s, index %d", pubkey.Hex(), index))
	return nil
}

// Verifies the supernode's minipool address at the provided index is expected and the minipool status is prelaunch
func VerifyMinipoolAfterCreationBeforeTest(logger *slog.Logger, qMgr *eth.QueryManager, rpSuperNode *node.Node, superNodeMpIndex uint64, expectedMinipoolAddress common.Address, mpMgr *minipool.MinipoolManager) (minipool.IMinipool, error) {
	// Make sure the address is correct
	var mpAddress common.Address
	err := qMgr.Query(func(mc *batch.MultiCaller) error {
		rpSuperNode.GetMinipoolAddress(mc, &mpAddress, superNodeMpIndex)
		return nil
	}, nil)
	if err != nil {
		return nil, err
	}
	if mpAddress != expectedMinipoolAddress {
		return nil, fmt.Errorf("Minipool address is not expected; expected %s, got %s", expectedMinipoolAddress.Hex(), mpAddress.Hex())
	}

	// Make sure it's in prelaunch
	mp, err := mpMgr.CreateMinipoolFromAddress(mpAddress, false, nil)
	if err != nil {
		return nil, err
	}
	err = qMgr.Query(nil, nil, mp.Common().Status)
	if err != nil {
		return nil, err
	}
	if mp.Common().Status.Formatted() != types.MinipoolStatus_Prelaunch {
		return nil, fmt.Errorf("Minipool is not in prelaunch; status is %s", mp.Common().Status.Formatted())
	}
	logger.Info(fmt.Sprintf("Minipool %s is in prelaunch", mpAddress.Hex()))
	return mp, nil
}

// Stakes a minipool
func StakeMinipoolBeforeTest(testHarness *StandardTestHarness, csNode *cstesting.ConstellationNode, nodeAddress common.Address, mp minipool.IMinipool) error {
	// Bindings
	testMgr := testHarness.TestManager
	logger := testHarness.Logger
	cs := csNode.GetApiClient()
	sp := csNode.GetServiceProvider()
	qMgr := sp.GetQueryManager()
	ec := sp.GetEthClient()

	// Get the node balance
	beforeBalance, err := ec.BalanceAt(context.Background(), nodeAddress, nil)
	if err != nil {
		return fmt.Errorf("error getting node balance: %w", err)
	}

	// Stake the minipool
	stakeResponse, err := cs.Minipool.Stake()
	if err != nil {
		return fmt.Errorf("error staking minipool: %w", err)
	}
	if stakeResponse.Data.NotWhitelistedWithConstellation {
		return fmt.Errorf("node is not whitelisted with Constellation")
	}

	// Find the details for the MP and stake it
	for _, details := range stakeResponse.Data.Details {
		if details.Address == mp.Common().Address {
			err = testMgr.MineTxViaHyperdrive(csNode.GetHyperdriveNode().GetApiClient(), details.TxInfo)
			if err != nil {
				return fmt.Errorf("error mining staking TX: %w", err)
			}

			logger.Info(fmt.Sprintf("Staked minipool %s", mp.Common().Address.Hex()))
			break
		}
	}

	// Verify the minipool is staking now
	err = qMgr.Query(nil, nil, mp.Common().Status)
	if err != nil {
		return err
	}
	if mp.Common().Status.Formatted() != types.MinipoolStatus_Staking {
		return fmt.Errorf("Minipool is not in staking; status is %s", mp.Common().Status.Formatted())
	}
	logger.Info(fmt.Sprintf("Minipool %s is in staking", mp.Common().Address.Hex()))

	// Get the balance after
	afterBalance, err := ec.BalanceAt(context.Background(), nodeAddress, nil)
	if err != nil {
		return fmt.Errorf("error getting node balance: %w", err)
	}
	if afterBalance.Cmp(beforeBalance) != 1 {
		return fmt.Errorf("Node balance did not increase")
	}
	logger.Info(fmt.Sprintf("Node balance increased from %.6f to %.6f", eth.WeiToEth(beforeBalance), eth.WeiToEth(afterBalance)))
	return nil
}

// ==============
// === With T ===
// ==============

// Registers the node with Constellation, ensuring it wasn't previously registered beforehand
func RegisterWithConstellation(t *testing.T, testMgr *cstesting.ConstellationTestManager, csNode *cstesting.ConstellationNode) {
	// Bindings
	cs := csNode.GetApiClient()

	// Check if the node is registered
	statusResponse, err := cs.Node.GetRegistrationStatus()
	require.NoError(t, err)
	require.False(t, statusResponse.Data.Registered)
	t.Log("Node is not registered with Constellation yet, as expected")

	// Register the node
	response, err := cs.Node.Register()
	require.NoError(t, err)
	require.False(t, response.Data.NotAuthorized)
	require.False(t, response.Data.NotRegisteredWithNodeSet)
	require.True(t, response.Data.TxInfo.SimulationResult.IsSimulated)
	require.Empty(t, response.Data.TxInfo.SimulationResult.SimulationError)
	err = testMgr.MineTxViaHyperdrive(csNode.GetHyperdriveNode().GetApiClient(), response.Data.TxInfo)
	require.NoError(t, err)
	t.Log("Mined the registration TX")

	// Check if the node is registered
	statusResponse, err = cs.Node.GetRegistrationStatus()
	require.NoError(t, err)
	require.True(t, statusResponse.Data.Registered)
	t.Log("Node is now registered with Constellation")
}

// Deposits RPL to the RPL vault and verifies the contract balances have been updated
func DepositToRplVault(t *testing.T, testMgr *cstesting.ConstellationTestManager, rplVault contracts.IErc4626Token, rpl *tokens.TokenRpl, amount *big.Int, opts *bind.TransactOpts) {
	// Bindings
	csNode := testMgr.GetNode()
	sp := csNode.GetServiceProvider()
	qMgr := sp.GetQueryManager()
	csMgr := sp.GetConstellationManager()

	// Get the xRPL balance before depositing
	var xRplBalance *big.Int
	err := qMgr.Query(func(mc *batch.MultiCaller) error {
		rplVault.BalanceOf(mc, &xRplBalance, opts.From)
		return nil
	}, nil)
	require.NoError(t, err)

	// Deposit RPL to the RPL vault
	err = testMgr.Constellation_DepositToRplVault(rplVault, amount, opts, opts)
	require.NoError(t, err)
	t.Logf("Deposited %.6f RPL into the RPL vault", eth.WeiToEth(amount))

	// Verify balances have been updated
	var odRplBalance *big.Int
	var rvRplBalance *big.Int
	var newXrplBalance *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		rpl.BalanceOf(mc, &odRplBalance, csMgr.OperatorDistributor.Address)
		rpl.BalanceOf(mc, &rvRplBalance, rplVault.Address())
		rplVault.BalanceOf(mc, &newXrplBalance, opts.From)
		return nil
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, odRplBalance.Cmp(common.Big0))
	t.Logf("OperatorDistributor's RPL balance is now %.6f (%s wei)", eth.WeiToEth(odRplBalance), odRplBalance.String())
	require.Equal(t, 1, rvRplBalance.Cmp(common.Big0))
	t.Logf("RPL vault's RPL balance is now %.6f (%s wei)", eth.WeiToEth(rvRplBalance), rvRplBalance.String())
	require.Equal(t, 1, newXrplBalance.Cmp(xRplBalance))
	t.Logf("Sender's xRPL balance went up from %.6f (%s wei) to %.6f (%s wei)",
		eth.WeiToEth(xRplBalance), xRplBalance.String(),
		eth.WeiToEth(newXrplBalance), newXrplBalance.String(),
	)
}

// Deposits WETH to the WETH vault and verifies the contract balances have been updated
func DepositToWethVault(t *testing.T, testMgr *cstesting.ConstellationTestManager, wethVault contracts.IErc4626Token, weth *contracts.Weth, amount *big.Int, opts *bind.TransactOpts) {
	// Bindings
	sp := testMgr.GetNode().GetServiceProvider()
	qMgr := sp.GetQueryManager()
	csMgr := sp.GetConstellationManager()
	ec := sp.GetEthClient()

	err := testMgr.Constellation_DepositToWethVault(weth, wethVault, amount, opts)
	require.NoError(t, err)
	t.Logf("Deposited %.6f WETH into the WETH vault", eth.WeiToEth(amount))

	// Verify OperatorDistributor WETH balance has been updated
	odEthBalance, err := ec.BalanceAt(context.Background(), csMgr.OperatorDistributor.Address, nil)
	require.NoError(t, err)
	var evWethBalance *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		weth.BalanceOf(mc, &evWethBalance, wethVault.Address())
		return nil
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, odEthBalance.Cmp(common.Big0))
	t.Logf("OperatorDistributor's ETH balance is now %.6f (%s wei)", eth.WeiToEth(odEthBalance), odEthBalance.String())
	require.Equal(t, 1, evWethBalance.Cmp(common.Big0))
	t.Logf("WETH vault's WETH balance is now %.6f (%s wei)", eth.WeiToEth(evWethBalance), evWethBalance.String())
}

// Creates the TX for creating a new minipool, and verifies it simulated successfully
func BuildAndVerifyCreateMinipoolTx(t *testing.T, deployment *db.ConstellationDeployment, csNode *cstesting.ConstellationNode, nodeAddress common.Address, salt *big.Int, rpSuperNode *node.Node, shouldSucceed bool) *csapi.MinipoolCreateData {
	// Bindings
	cs := csNode.GetApiClient()

	// Make the minipool
	depositResponse, err := cs.Minipool.Create(salt)
	require.NoError(t, err)
	require.True(t, depositResponse.Data.CanCreate)
	if shouldSucceed {
		require.True(t, depositResponse.Data.TxInfo.SimulationResult.IsSimulated)
		require.Empty(t, depositResponse.Data.TxInfo.SimulationResult.SimulationError)
	}
	t.Logf("Using salt 0x%s, MP address = %s", salt.Text(16), depositResponse.Data.MinipoolAddress.Hex())

	// Increment the nonce for the node
	deployment.IncrementSuperNodeNonce(nodeAddress)
	return depositResponse.Data
}

// Builds and submits multiple minipool creation TXs, returning the minipool data and transaction hashes
func BuildAndSubmitCreateMinipoolTxs(t *testing.T, deployment *db.ConstellationDeployment, nodes []*cstesting.ConstellationNode, addresses []common.Address, mpsPerNode int, salts [][]*big.Int, rpSuperNode *node.Node) ([][]*csapi.MinipoolCreateData, [][]common.Hash) {
	// Build the minipool creation TXs
	datas := make([][]*csapi.MinipoolCreateData, len(nodes))
	for i, node := range nodes {
		datasForNode := make([]*csapi.MinipoolCreateData, mpsPerNode)
		for j := 0; j < mpsPerNode; j++ {
			var salt *big.Int
			if salts != nil {
				salt = salts[i][j]
			} else {
				salt = big.NewInt(int64(mpsPerNode*i + j)) // Sequential salts; only works if this function is called once
			}
			data := BuildAndVerifyCreateMinipoolTx(t, deployment, node, addresses[i], salt, rpSuperNode, j == 0)
			datasForNode[j] = data
			SaveValidatorKey(t, node, data)
		}
		datas[i] = datasForNode
	}
	t.Log("Built minipool creation TXs")

	// Submit each TX
	hashes := make([][]common.Hash, len(nodes))
	for i, node := range nodes {
		hashesForNode := make([]common.Hash, mpsPerNode)
		hd := node.GetHyperdriveNode().GetApiClient()
		for j, data := range datas[i] {
			submission, _ := eth.CreateTxSubmissionFromInfo(data.TxInfo, nil)
			if submission.GasLimit == 0 {
				submission.GasLimit = 5000000 // Explicit limit for creations that failed simulation on purpose
			}
			response, err := hd.Tx.SubmitTx(submission, nil, eth.GweiToWei(10), eth.GweiToWei(0.5))
			require.NoError(t, err)
			hashesForNode[j] = response.Data.TxHash
		}
		hashes[i] = hashesForNode
	}
	t.Log("Submitted minipool creation TXs")
	return datas, hashes
}

// Saves the validator key created as part of a minipool creation TX to disk
func SaveValidatorKey(t *testing.T, csNode *cstesting.ConstellationNode, data *csapi.MinipoolCreateData) {
	// Bindings
	cs := csNode.GetApiClient()

	// Save the key
	pubkey := data.ValidatorPubkey
	index := data.Index
	_, err := cs.Wallet.CreateValidatorKey(pubkey, index, 1)
	require.NoError(t, err)
	t.Logf("Saved validator key for pubkey %s, index %d", pubkey.Hex(), index)
}

// Verifies the supernode's minipool address at the provided index is expected and the minipool status is prelaunch
func VerifyMinipoolAfterCreation(t *testing.T, qMgr *eth.QueryManager, rpSuperNode *node.Node, superNodeMpIndex uint64, expectedMinipoolAddress common.Address, mpMgr *minipool.MinipoolManager) minipool.IMinipool {
	// Make sure the address is correct
	var mpAddress common.Address
	err := qMgr.Query(func(mc *batch.MultiCaller) error {
		rpSuperNode.GetMinipoolAddress(mc, &mpAddress, superNodeMpIndex)
		return nil
	}, nil)
	require.NoError(t, err)
	require.Equal(t, expectedMinipoolAddress, mpAddress)

	// Make sure it's in prelaunch
	mp, err := mpMgr.CreateMinipoolFromAddress(mpAddress, false, nil)
	require.NoError(t, err)
	err = qMgr.Query(nil, nil, mp.Common().Status)
	require.NoError(t, err)
	require.Equal(t, types.MinipoolStatus_Prelaunch, mp.Common().Status.Formatted())
	t.Logf("Minipool %s is in prelaunch", mpAddress.Hex())
	return mp
}

// Deposits into Constellation, creating a new minipool
func CreateMinipool(t *testing.T, testMgr *cstesting.ConstellationTestManager, csNode *cstesting.ConstellationNode, nodeAddress common.Address, salt *big.Int, rpSuperNode *node.Node, mpMgr *minipool.MinipoolManager) minipool.IMinipool {
	// Bindings
	sp := csNode.GetServiceProvider()
	qMgr := sp.GetQueryManager()

	// Check the Supernode minipool count
	err := qMgr.Query(nil, nil, rpSuperNode.MinipoolCount)
	require.NoError(t, err)
	previousMpCount := rpSuperNode.MinipoolCount.Formatted()
	t.Logf("Supernode has %d minipools", previousMpCount)

	// Make the minipool
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	nsDB := nsMgr.GetDatabase()
	res := sp.GetResources()
	deployment := nsDB.Constellation.GetDeployment(res.DeploymentName)
	data := BuildAndVerifyCreateMinipoolTx(t, deployment, csNode, nodeAddress, salt, rpSuperNode, true)
	err = testMgr.MineTxViaHyperdrive(csNode.GetHyperdriveNode().GetApiClient(), data.TxInfo)
	require.NoError(t, err)

	// Save the key
	SaveValidatorKey(t, csNode, data)

	// Check the Supernode minipool count
	err = qMgr.Query(nil, nil, rpSuperNode.MinipoolCount)
	require.NoError(t, err)
	newMpCount := rpSuperNode.MinipoolCount.Formatted()
	require.Equal(t, uint64(1), newMpCount-previousMpCount)
	t.Logf("Supernode now has %d minipools", newMpCount)

	// Verify the minipool
	mp := VerifyMinipoolAfterCreation(t, qMgr, rpSuperNode, newMpCount-1, data.MinipoolAddress, mpMgr)
	return mp
}

// Builds and submits multiple minipool stake TXs, returning the transaction hashes
func BuildAndSubmitStakeMinipoolTxs(t *testing.T, nodes []*cstesting.ConstellationNode, creationData [][]*csapi.MinipoolCreateData) [][]common.Hash {
	hashes := make([][]common.Hash, len(creationData))
	for i, node := range nodes {
		// Services
		cs := node.GetApiClient()
		hd := node.GetHyperdriveNode().GetApiClient()
		creationDataForNode := creationData[i]
		hashesForNode := make([]common.Hash, len(creationDataForNode))

		// Run a stake request
		stakeResp, err := cs.Minipool.Stake()
		require.NoError(t, err)
		require.Len(t, stakeResp.Data.Details, len(creationDataForNode))

		// Require the minipools can stake
		for j, data := range creationDataForNode {
			address := data.MinipoolAddress
			found := false
			for _, details := range stakeResp.Data.Details {
				if details.Address == address {
					found = true
					require.True(t, details.CanStake)
					submission, _ := eth.CreateTxSubmissionFromInfo(details.TxInfo, nil)
					submitResp, err := hd.Tx.SubmitTx(submission, nil, eth.GweiToWei(10), eth.GweiToWei(0.5))
					require.NoError(t, err)
					hashesForNode[j] = submitResp.Data.TxHash
					break
				}
			}
			require.True(t, found)
		}

		hashes[i] = hashesForNode
	}
	return hashes
}

// Stakes a minipool
func StakeMinipool(t *testing.T, testMgr *cstesting.ConstellationTestManager, csNode *cstesting.ConstellationNode, nodeAddress common.Address, mp minipool.IMinipool) {
	// Bindings
	cs := csNode.GetApiClient()
	sp := csNode.GetServiceProvider()
	qMgr := sp.GetQueryManager()
	ec := sp.GetEthClient()

	// Get the node balance
	beforeBalance, err := ec.BalanceAt(context.Background(), nodeAddress, nil)
	require.NoError(t, err)

	// Stake the minipool
	stakeResponse, err := cs.Minipool.Stake()
	require.NoError(t, err)
	require.False(t, stakeResponse.Data.NotWhitelistedWithConstellation)

	// Find the details for the MP and stake it
	for _, details := range stakeResponse.Data.Details {
		if details.Address == mp.Common().Address {
			err = testMgr.MineTxViaHyperdrive(csNode.GetHyperdriveNode().GetApiClient(), details.TxInfo)
			require.NoError(t, err)
			t.Logf("Staked minipool %s", mp.Common().Address.Hex())
			break
		}
	}

	// Verify the minipool is staking now
	err = qMgr.Query(nil, nil, mp.Common().Status)
	require.NoError(t, err)
	require.Equal(t, types.MinipoolStatus_Staking, mp.Common().Status.Formatted())
	t.Logf("Minipool %s is in staking", mp.Common().Address.Hex())

	// Get the balance after
	afterBalance, err := ec.BalanceAt(context.Background(), nodeAddress, nil)
	require.NoError(t, err)
	require.Equal(t, 1, afterBalance.Cmp(beforeBalance))
	t.Logf("Node balance increased from %.6f to %.6f", eth.WeiToEth(beforeBalance), eth.WeiToEth(afterBalance))
}
