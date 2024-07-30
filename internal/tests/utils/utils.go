package cstestutils

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/nodeset-org/hyperdrive-constellation/common/contracts"
	cstesting "github.com/nodeset-org/hyperdrive-constellation/testing"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
	"github.com/rocket-pool/rocketpool-go/v2/node"
	"github.com/rocket-pool/rocketpool-go/v2/tokens"
	"github.com/rocket-pool/rocketpool-go/v2/types"
	"github.com/stretchr/testify/require"
)

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
	testMgr.MineTxViaHyperdrive(t, csNode.GetHyperdriveNode().GetApiClient(), response.Data.TxInfo, "Registered the node with Constellation")

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

	// Deposit RPL to the RPL vault
	err := testMgr.Constellation_DepositToRplVault(rplVault, amount, opts, opts)
	require.NoError(t, err)
	t.Logf("Deposited %.6f RPL into the RPL vault", eth.WeiToEth(amount))

	// Verify OperatorDistributor RPL balance has been updated
	var odRplBalance *big.Int
	var rvRplBalance *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		rpl.BalanceOf(mc, &odRplBalance, csMgr.OperatorDistributor.Address)
		rpl.BalanceOf(mc, &rvRplBalance, rplVault.Address())
		return nil
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, odRplBalance.Cmp(common.Big0))
	t.Logf("OperatorDistributor's RPL balance is now %.6f (%s wei)", eth.WeiToEth(odRplBalance), odRplBalance.String())
	require.Equal(t, 1, rvRplBalance.Cmp(common.Big0))
	t.Logf("RPL vault's RPL balance is now %.6f (%s wei)", eth.WeiToEth(rvRplBalance), rvRplBalance.String())
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

// Deposits into Constellation, creating a new minipool
func CreateMinipoolViaDeposit(t *testing.T, testMgr *cstesting.ConstellationTestManager, csNode *cstesting.ConstellationNode, salt *big.Int, rpSuperNode *node.Node, mpMgr *minipool.MinipoolManager) minipool.IMinipool {
	// Bindings
	cs := csNode.GetApiClient()
	sp := csNode.GetServiceProvider()
	qMgr := sp.GetQueryManager()

	// Check the Supernode minipool count
	err := qMgr.Query(nil, nil, rpSuperNode.MinipoolCount)
	require.NoError(t, err)
	previousMpCount := rpSuperNode.MinipoolCount.Formatted()
	t.Logf("Supernode has %d minipools", previousMpCount)

	depositResponse, err := cs.Minipool.Create(salt)
	require.NoError(t, err)
	require.True(t, depositResponse.Data.CanCreate)
	require.True(t, depositResponse.Data.TxInfo.SimulationResult.IsSimulated)
	require.Empty(t, depositResponse.Data.TxInfo.SimulationResult.SimulationError)
	testMgr.MineTxViaHyperdrive(t, csNode.GetHyperdriveNode().GetApiClient(), depositResponse.Data.TxInfo, "Deposited and made a minipool")
	t.Logf("Using salt 0x%s, MP address = %s", salt.Text(16), depositResponse.Data.MinipoolAddress.Hex())

	// Save the key
	pubkey := depositResponse.Data.ValidatorPubkey
	index := depositResponse.Data.Index
	_, err = cs.Wallet.CreateValidatorKey(pubkey, index, 1)
	require.NoError(t, err)
	t.Logf("Saved validator key for pubkey %s, index %d", pubkey.Hex(), index)

	// Check the Supernode minipool count
	err = qMgr.Query(nil, nil, rpSuperNode.MinipoolCount)
	require.NoError(t, err)
	newMpCount := rpSuperNode.MinipoolCount.Formatted()
	require.Equal(t, uint64(1), newMpCount-previousMpCount)
	t.Logf("Supernode now has %d minipools", newMpCount)

	// Make sure the address is correct
	var mpAddress common.Address
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		rpSuperNode.GetMinipoolAddress(mc, &mpAddress, newMpCount-1)
		return nil
	}, nil)
	require.NoError(t, err)
	require.Equal(t, depositResponse.Data.MinipoolAddress, mpAddress)

	// Make sure it's in prelaunch
	mp, err := mpMgr.CreateMinipoolFromAddress(mpAddress, false, nil)
	require.NoError(t, err)
	err = qMgr.Query(nil, nil, mp.Common().Status)
	require.NoError(t, err)
	require.Equal(t, types.MinipoolStatus_Prelaunch, mp.Common().Status.Formatted())
	t.Log("Minipool is in prelaunch")

	return mp
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
			testMgr.MineTxViaHyperdrive(t, csNode.GetHyperdriveNode().GetApiClient(), details.TxInfo, "Staked the minipool")
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

// Harvest rewards from the yield distributor and assert the node's WETH balance increased
// Note that a node must have a minipool staking for this to succeed
func HarvestRewards(t *testing.T, testMgr *cstesting.ConstellationTestManager, csNode *cstesting.ConstellationNode, weth *contracts.Weth, treasuryAddress common.Address, nodeAddress common.Address, opts *bind.TransactOpts) {
	// Bindings
	sp := csNode.GetServiceProvider()
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()

	// Get wrapped ETH balances before harvest
	var wethBalanceNodeBefore *big.Int
	var wethBalanceTreasuryBefore *big.Int
	err := qMgr.Query(func(mc *batch.MultiCaller) error {
		weth.BalanceOf(mc, &wethBalanceNodeBefore, nodeAddress)
		weth.BalanceOf(mc, &wethBalanceTreasuryBefore, treasuryAddress)
		return nil
	}, nil)
	require.NoError(t, err)

	// Make a harvest TX
	harvestTxInfo, err := csMgr.YieldDistributor.Harvest(nodeAddress, common.Big0, common.Big1, opts)
	require.NoError(t, err)
	require.NotNil(t, harvestTxInfo)
	testMgr.MineTx(t, harvestTxInfo, opts, "Harvested minipool")

	// Get wrapped ETH balances after harvest
	var wethBalanceNodeAfter *big.Int
	var wethBalanceTreasuryAfter *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		weth.BalanceOf(mc, &wethBalanceNodeAfter, nodeAddress)
		weth.BalanceOf(mc, &wethBalanceTreasuryAfter, treasuryAddress)
		return nil
	}, nil)
	require.NoError(t, err)

	// Verify the node's WETH balance increased
	require.Equal(t, 1, wethBalanceNodeAfter.Cmp(wethBalanceNodeBefore))
	t.Logf("Node's WETH balance increased after harvest from %.6f to %.6f", eth.WeiToEth(wethBalanceNodeBefore), eth.WeiToEth(wethBalanceNodeAfter))

	// TODO: Claim treasury WETH
	// t.Logf("Treasury's WETH balance increased after harvest from %.6f to %.6f", eth.WeiToEth(wethBalanceTreasuryBefore), eth.WeiToEth(wethBalanceTreasuryAfter))
	// require.Equal(t, 1, wethBalanceTreasuryAfter.Cmp(wethBalanceTreasuryBefore))
}
