package with_ns_registered

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/nodeset-org/hyperdrive-constellation/common/contracts"
	hdtesting "github.com/nodeset-org/hyperdrive-daemon/testing"
	"github.com/nodeset-org/osha/keys"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/rocketpool-go/v2/dao/oracle"
	"github.com/rocket-pool/rocketpool-go/v2/dao/protocol"
	"github.com/rocket-pool/rocketpool-go/v2/deposit"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
	"github.com/rocket-pool/rocketpool-go/v2/node"
	"github.com/rocket-pool/rocketpool-go/v2/rocketpool"
	"github.com/rocket-pool/rocketpool-go/v2/tokens"
	"github.com/rocket-pool/rocketpool-go/v2/types"
	"github.com/stretchr/testify/require"
)

const (
	expectedMinipoolCount int     = 1
	ethBondPerLeb8        float64 = 8
)

// Test getting the available minipool count when there are no minipools available
func TestMinipoolGetAvailableMinipoolCount_Zero(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

	// Check the available minipool count
	cs := testMgr.GetApiClient()
	countResponse, err := cs.Minipool.GetAvailableMinipoolCount()
	require.NoError(t, err)
	require.Equal(t, 0, countResponse.Data.Count)
}

// Test getting the available minipool count when there is one minipool available
func TestMinipoolGetAvailableMinipoolCount_One(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

	// Set up the NodeSet mock server
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	nsMgr.SetAvailableConstellationMinipoolCount(nodeAddress, expectedMinipoolCount)

	// Check the available minipool count
	cs := testMgr.GetApiClient()
	countResponse, err := cs.Minipool.GetAvailableMinipoolCount()
	require.NoError(t, err)
	require.Equal(t, expectedMinipoolCount, countResponse.Data.Count)
}

func TestMinipoolDeposit(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

	// Get the private key for the RP and Constellation deployer
	keygen, err := keys.NewKeyGeneratorWithDefaults()
	require.NoError(t, err)
	deployerKey, err := keygen.GetEthPrivateKey(0)
	require.NoError(t, err)
	deployerPubkey := crypto.PubkeyToAddress(deployerKey.PublicKey)
	t.Logf("Deployer key: %s\n", deployerPubkey.Hex())
	chainID := testMgr.GetBeaconMockManager().GetConfig().ChainID
	deployerOpts, err := bind.NewKeyedTransactorWithChainID(deployerKey, big.NewInt(int64(chainID)))
	require.NoError(t, err)

	// Set up the services
	sp := testMgr.GetConstellationServiceProvider()
	ec := sp.GetEthClient()
	qMgr := sp.GetQueryManager()
	txMgr := sp.GetTransactionManager()

	// Load RP
	rpMgr := sp.GetRocketPoolManager()
	err = rpMgr.RefreshRocketPoolContracts()
	require.NoError(t, err)
	rp := rpMgr.RocketPool
	t.Log("Loaded Rocket Pool")

	// Make some RP bindings
	dpMgr, err := deposit.NewDepositPoolManager(rp)
	require.NoError(t, err)
	fsrpl, err := tokens.NewTokenRplFixedSupply(rp)
	require.NoError(t, err)
	rpl, err := tokens.NewTokenRpl(rp)
	require.NoError(t, err)
	pdaoMgr, err := protocol.NewProtocolDaoManager(rp)
	require.NoError(t, err)
	mpMgr, err := minipool.NewMinipoolManager(rp)
	require.NoError(t, err)
	//netMgr, err := network.NewNetworkManager(rp)
	//require.NoError(t, err)
	//nodeMgr, err := node.NewNodeManager(rp)
	t.Log("Created Rocket Pool bindings")

	// Load Constellation
	csMgr := sp.GetConstellationManager()
	err = csMgr.LoadContracts()
	require.NoError(t, err)
	t.Log("Loaded Constellation")

	// Create some Constellation bindings
	var rplVaultAddress common.Address
	var wethVaultAddress common.Address
	var wethAddress common.Address
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.Directory.GetRplVaultAddress(mc, &rplVaultAddress)
		csMgr.Directory.GetWethVaultAddress(mc, &wethVaultAddress)
		csMgr.Directory.GetWethAddress(mc, &wethAddress)
		return nil
	}, nil)
	require.NoError(t, err)
	rplVault, err := contracts.NewErc4626Token(rplVaultAddress, ec, qMgr, txMgr, nil)
	require.NoError(t, err)
	wethVault, err := contracts.NewErc4626Token(wethVaultAddress, ec, qMgr, txMgr, nil)
	require.NoError(t, err)
	weth, err := contracts.NewWeth(wethAddress, ec, qMgr, txMgr, nil)
	require.NoError(t, err)
	t.Log("Created Constellation bindings")

	// Bootstrap the oDAO
	odao1Key, err := keygen.GetEthPrivateKey(10)
	require.NoError(t, err)
	odao2Key, err := keygen.GetEthPrivateKey(11)
	require.NoError(t, err)
	odao3Key, err := keygen.GetEthPrivateKey(12)
	require.NoError(t, err)
	odao1Opts, err := bind.NewKeyedTransactorWithChainID(odao1Key, big.NewInt(int64(chainID)))
	require.NoError(t, err)
	odao2Opts, err := bind.NewKeyedTransactorWithChainID(odao2Key, big.NewInt(int64(chainID)))
	require.NoError(t, err)
	odao3Opts, err := bind.NewKeyedTransactorWithChainID(odao3Key, big.NewInt(int64(chainID)))
	require.NoError(t, err)
	_, err = BootstrapNodeToOdao(rp, deployerOpts, odao1Opts.From, odao1Opts, "Etc/UTC", "odao1", "https://odao1.com")
	require.NoError(t, err)
	_, err = BootstrapNodeToOdao(rp, deployerOpts, odao2Opts.From, odao2Opts, "Etc/UTC", "odao2", "https://odao2.com")
	require.NoError(t, err)
	_, err = BootstrapNodeToOdao(rp, deployerOpts, odao3Opts.From, odao3Opts, "Etc/UTC", "odao3", "https://odao3.com")
	require.NoError(t, err)
	t.Log("Bootstrapped oDAO nodes")

	/*
		// Update the RPL price to 0.02
		newPrice := big.NewInt(2e16)
		currentBlockHeader, err := ec.HeaderByNumber(context.Background(), nil)
		require.NoError(t, err)
		txInfo, err := netMgr.SubmitPrices(currentBlockHeader.Number.Uint64(), currentBlockHeader.Time, newPrice, odao1Opts)
		require.NoError(t, err)
		MineTx(t, txInfo, odao1Opts, "Updated RPL price from oDAO 1")
		txInfo, err = netMgr.SubmitPrices(currentBlockHeader.Number.Uint64(), currentBlockHeader.Time, newPrice, odao2Opts)
		require.NoError(t, err)
		MineTx(t, txInfo, odao2Opts, "Updated RPL price from oDAO 2")

		// Change the target stake ratio to 80%
		adminKey, err := keygen.GetEthPrivateKey(1)
		require.NoError(t, err)
		adminOpts, err := bind.NewKeyedTransactorWithChainID(adminKey, big.NewInt(int64(chainID)))
		targetStakeRatio := eth.EthToWei(1.2) // Sub 100% will fail (contract bug)
		txInfo, err = csMgr.OperatorDistributor.SetTargetStakeRatio(targetStakeRatio, adminOpts)
		require.NoError(t, err)
		MineTx(t, txInfo, adminOpts, "Changed target stake ratio to 80%")
	*/

	// Run a query
	supernodeAddress := csMgr.SuperNodeAccount.Address
	rpSuperNode, err := node.NewNode(rp, supernodeAddress)
	require.NoError(t, err)
	var rplPrice *big.Int
	var rplRequired *big.Int
	leb8BondInWei := eth.EthToWei(ethBondPerLeb8)
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.PriceFetcher.GetRplPrice(mc, &rplPrice)
		csMgr.OperatorDistributor.CalculateRplStakeShortfall(mc, &rplRequired, common.Big0, leb8BondInWei)
		return nil
	}, nil,
		rpSuperNode.Exists,
		dpMgr.Balance,
		pdaoMgr.Settings.Deposit.MaximumDepositPoolSize,
	)
	require.NoError(t, err)

	// Verify some details
	require.True(t, rpSuperNode.Exists.Get())
	t.Log("Supernode account is registered with RP")
	require.Equal(t, 0, dpMgr.Balance.Get().Cmp(common.Big0))
	t.Log("Deposit pool balance is zero")
	require.Equal(t, 1, rplPrice.Cmp(common.Big0))
	t.Logf("RPL price is %.6f RPL/ETH (%s wei)", eth.WeiToEth(rplPrice), rplPrice.String())
	t.Logf("RPL required for 8 ETH bond is %.6f RPL (%s wei)", eth.WeiToEth(rplRequired), rplRequired.String())

	// Send ETH to the RP deposit pool
	fundOpts := &bind.TransactOpts{
		From:  deployerOpts.From,
		Value: pdaoMgr.Settings.Deposit.MaximumDepositPoolSize.Get(), // Deposit the maximum amount
	}
	txInfo, err := dpMgr.Deposit(fundOpts)
	require.NoError(t, err)
	MineTx(t, txInfo, deployerOpts, "Funded the deposit pool")

	// Mint some old RPL
	rplAmountWei := eth.EthToWei(3200) // rplRequired // 44 will fail (contract bug)
	rplAmount := eth.WeiToEth(rplAmountWei)
	txInfo, err = MintLegacyRpl(rp, deployerOpts, deployerPubkey, rplAmountWei)
	require.NoError(t, err)
	MineTx(t, txInfo, deployerOpts, fmt.Sprintf("Minted %.6f old RPL", rplAmount))

	// Approve old RPL for swap
	rplContract, err := rp.GetContract(rocketpool.ContractName_RocketTokenRPL)
	require.NoError(t, err)
	txInfo, err = fsrpl.Approve(rplContract.Address, rplAmountWei, deployerOpts)
	require.NoError(t, err)
	MineTx(t, txInfo, deployerOpts, "Approved old RPL for swap")

	// Swap it to new RPL
	txInfo, err = rpl.SwapFixedSupplyRplForRpl(rplAmountWei, deployerOpts)
	require.NoError(t, err)
	MineTx(t, txInfo, deployerOpts, "Swapped old RPL for new RPL")

	// Deposit RPL into the RPL vault
	txInfo, err = rpl.Approve(rplVaultAddress, rplAmountWei, deployerOpts)
	require.NoError(t, err)
	MineTx(t, txInfo, deployerOpts, "Approved RPL for deposit")
	txInfo, err = rplVault.Deposit(rplAmountWei, deployerPubkey, deployerOpts)
	require.NoError(t, err)
	MineTx(t, txInfo, deployerOpts, "Deposited RPL into the RPL vault")

	// Verify OperatorDistributor RPL balance has been updated
	var odRplBalance *big.Int
	var rvRplBalance *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		rpl.BalanceOf(mc, &odRplBalance, csMgr.OperatorDistributor.Address)
		rpl.BalanceOf(mc, &rvRplBalance, rplVaultAddress)
		return nil
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, odRplBalance.Cmp(common.Big0))
	t.Logf("OperatorDistributor's RPL balance is now %.6f (%s wei)", eth.WeiToEth(odRplBalance), odRplBalance.String())
	require.Equal(t, 1, rvRplBalance.Cmp(common.Big0))
	t.Logf("RPL vault's RPL balance is now %.6f (%s wei)", eth.WeiToEth(rvRplBalance), rvRplBalance.String())

	// Mint some WETH
	ethAmountWei := eth.EthToWei(90)
	//ethAmount := eth.WeiToEth(ethAmountWei)
	wethOpts := &bind.TransactOpts{
		From:  deployerOpts.From,
		Value: big.NewInt(0).Set(ethAmountWei),
	}
	txInfo, err = weth.Deposit(wethOpts)
	require.NoError(t, err)
	MineTx(t, txInfo, deployerOpts, "Minted WETH")
	var wethBalance *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		weth.BalanceOf(mc, &wethBalance, deployerPubkey)
		return nil
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 0, ethAmountWei.Cmp(wethBalance))
	t.Logf("Deployer's WETH balance is now %.6f (%s wei)", eth.WeiToEth(wethBalance), wethBalance.String())

	// Deposit WETH into the WETH vault
	txInfo, err = weth.Approve(wethVaultAddress, ethAmountWei, deployerOpts)
	require.NoError(t, err)
	MineTx(t, txInfo, deployerOpts, "Approved WETH for deposit")
	txInfo, err = wethVault.Deposit(ethAmountWei, deployerPubkey, deployerOpts)
	require.NoError(t, err)
	MineTx(t, txInfo, deployerOpts, "Deposited WETH into the WETH vault")

	// Verify OperatorDistributor WETH balance has been updated
	odEthBalance, err := ec.BalanceAt(context.Background(), csMgr.OperatorDistributor.Address, nil)
	require.NoError(t, err)
	var evWethBalance *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		weth.BalanceOf(mc, &evWethBalance, wethVaultAddress)
		return nil
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, odEthBalance.Cmp(common.Big0))
	t.Logf("OperatorDistributor's ETH balance is now %.6f (%s wei)", eth.WeiToEth(odEthBalance), odEthBalance.String())
	require.Equal(t, 1, evWethBalance.Cmp(common.Big0))
	t.Logf("WETH vault's WETH balance is now %.6f (%s wei)", eth.WeiToEth(evWethBalance), evWethBalance.String())

	// Check if the node is registered
	cs := testMgr.GetApiClient()
	statusResponse, err := cs.Node.GetRegistrationStatus()
	require.NoError(t, err)
	require.False(t, statusResponse.Data.Registered)
	t.Log("Node is not registered with Constellation yet, as expected")

	// Set up the NodeSet mock server
	hd := testMgr.HyperdriveTestManager.GetApiClient()
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	nsMgr.SetConstellationAdminPrivateKey(deployerKey)
	nsMgr.SetAvailableConstellationMinipoolCount(nodeAddress, expectedMinipoolCount)
	t.Log("Set up the NodeSet mock server")

	// Make the registration tx
	response, err := cs.Node.Register()
	require.NoError(t, err)
	require.False(t, response.Data.NotAuthorized)
	require.False(t, response.Data.NotRegisteredWithNodeSet)
	t.Log("Generated registration tx")

	// Submit the tx
	submission, _ := eth.CreateTxSubmissionFromInfo(response.Data.TxInfo, nil)
	txResponse, err := hd.Tx.SubmitTx(submission, nil, eth.GweiToWei(10), eth.GweiToWei(0.5))
	require.NoError(t, err)
	t.Logf("Submitted registration tx: %s", txResponse.Data.TxHash)

	// Mine the tx
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined registration tx")

	// Wait for the tx
	_, err = hd.Tx.WaitForTransaction(txResponse.Data.TxHash)
	require.NoError(t, err)
	t.Log("Waiting for registration tx complete")

	// Check if the node is registered
	statusResponse, err = cs.Node.GetRegistrationStatus()
	require.NoError(t, err)
	require.True(t, statusResponse.Data.Registered)
	t.Log("Node is now registered with Constellation")

	// Make a Deposit TX
	salt := big.NewInt(0x90de5e7)
	depositResponse, err := cs.Minipool.Deposit(salt)
	require.NoError(t, err)
	require.False(t, depositResponse.Data.InsufficientLiquidity)
	require.False(t, depositResponse.Data.InsufficientMinipoolCount)
	require.False(t, depositResponse.Data.NotWhitelisted)
	require.True(t, depositResponse.Data.TxInfo.SimulationResult.IsSimulated)
	require.Empty(t, depositResponse.Data.TxInfo.SimulationResult.SimulationError)

	// Submit the tx
	submission, _ = eth.CreateTxSubmissionFromInfo(depositResponse.Data.TxInfo, nil)
	txResponse, err = hd.Tx.SubmitTx(submission, nil, eth.GweiToWei(10), eth.GweiToWei(0.5))
	require.NoError(t, err)
	t.Logf("Using salt %s, MP address = %s", salt.Text(16), depositResponse.Data.MinipoolAddress.Hex())
	t.Logf("Submitted deposit tx: %s", txResponse.Data.TxHash)

	// Mine the tx
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined deposit tx")

	// Wait for the tx
	_, err = hd.Tx.WaitForTransaction(txResponse.Data.TxHash)
	require.NoError(t, err)
	t.Log("Waiting for deposit tx complete")

	err = qMgr.Query(nil, nil, rpSuperNode.MinipoolCount)
	require.NoError(t, err)
	require.Equal(t, uint64(1), rpSuperNode.MinipoolCount.Formatted())
	t.Log("Supernode has one minipool")

	// Make sure it's in prelaunch
	var mpAddress common.Address
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		rpSuperNode.GetMinipoolAddress(mc, &mpAddress, 0)
		return nil
	}, nil)
	require.NoError(t, err)
	mp, err := mpMgr.CreateMinipoolFromAddress(mpAddress, false, nil)
	require.NoError(t, err)
	err = qMgr.Query(nil, nil, mp.Common().Status)
	require.NoError(t, err)
	require.Equal(t, types.MinipoolStatus_Prelaunch, mp.Common().Status.Formatted())
	t.Log("Minipool is in prelaunch")

	// Fast forward time
	slotsToAdvance := 12 * 60 * 60 / 12 // 1 hour
	err = testMgr.AdvanceSlots(uint(slotsToAdvance), false)
	require.NoError(t, err)
	t.Logf("Advanced %d slots", slotsToAdvance)

	// Make a Stake TX
	stakeResponse, err := cs.Minipool.Stake(mpAddress)
	require.NoError(t, err)
	require.NotNil(t, stakeResponse.Data.TxInfo)

	// Submit the tx
	submission, _ = eth.CreateTxSubmissionFromInfo(stakeResponse.Data.TxInfo, nil)
	txResponse, err = hd.Tx.SubmitTx(submission, nil, eth.GweiToWei(10), eth.GweiToWei(0.5))
	require.NoError(t, err)
	t.Logf("Submitted stake tx: %s", txResponse.Data.TxHash)

	// Mine the tx
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Log("Mined stake tx")

	// Wait for the tx
	_, err = hd.Tx.WaitForTransaction(txResponse.Data.TxHash)
	require.NoError(t, err)
	t.Log("Waiting for stake tx complete")

	err = qMgr.Query(nil, nil, mp.Common().Status)
	require.NoError(t, err)
	require.Equal(t, types.MinipoolStatus_Staking, mp.Common().Status.Formatted())
	t.Log("Minipool is in staking")

	// Mint some WETH
	//ethAmount := eth.WeiToEth(ethAmountWei)
	wethOpts = &bind.TransactOpts{
		From:  deployerOpts.From,
		Value: big.NewInt(0).Set(eth.EthToWei(10)),
	}
	txInfo, err = weth.Deposit(wethOpts)
	require.NoError(t, err)
	MineTx(t, txInfo, deployerOpts, "Minted WETH")

	txInfo, err = weth.Transfer(csMgr.YieldDistributor.Address, big.NewInt(0).Set(eth.EthToWei(10)), deployerOpts)
	require.NoError(t, err)
	MineTx(t, txInfo, deployerOpts, "Sent WETH to the YieldDistributor")

	// Fast forward time
	slotsToAdvance = 1200 * 60 * 60 / 12 // 1 hour
	err = testMgr.AdvanceSlots(uint(slotsToAdvance), false)
	require.NoError(t, err)
	t.Logf("Advanced %d slots", slotsToAdvance)

	var wethBalanceYieldDistributorBefore *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		weth.BalanceOf(mc, &wethBalanceYieldDistributorBefore, csMgr.YieldDistributor.Address)
		return nil
	}, nil)
	require.NoError(t, err)

	sendEthTx := eth.TransactionInfo{To: csMgr.YieldDistributor.Address, Value: eth.EthToWei(1)}
	sendEthTx.SimulationResult.IsSimulated = true
	sendEthOpts, _ := bind.NewKeyedTransactorWithChainID(deployerKey, big.NewInt(int64(chainID)))
	sendEthOpts.Value = eth.EthToWei(1)
	MineTx(t, &sendEthTx, sendEthOpts, "Sent ETH to the YieldDistributor")

	// Mine the tx
	err = testMgr.CommitBlock()
	require.NoError(t, err)

	var wethBalanceYieldDistributorAfter *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		weth.BalanceOf(mc, &wethBalanceYieldDistributorAfter, csMgr.YieldDistributor.Address)
		return nil
	}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, wethBalanceYieldDistributorAfter.Cmp(wethBalanceYieldDistributorBefore))

	// Get the node address
	nodeKey, err := keygen.GetEthPrivateKey(4)
	require.NoError(t, err)
	nodePubkey := crypto.PubkeyToAddress(nodeKey.PublicKey)

	// Get wrapped ETH balance of node pub key before harvest
	var wethBalanceNodeBefore *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		weth.BalanceOf(mc, &wethBalanceNodeBefore, nodePubkey)
		return nil
	}, nil)
	require.NoError(t, err)

	// Make a harvest TX for the minipool
	harvestTxInfo, err := csMgr.YieldDistributor.Harvest(nodePubkey, common.Big0, common.Big1, deployerOpts)
	require.NoError(t, err)
	require.NotNil(t, harvestTxInfo)
	MineTx(t, harvestTxInfo, deployerOpts, "Harvested minipool")

	// Get wrapped ETH balance of node pub key after harvest
	var wethBalanceNodeAfter *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		weth.BalanceOf(mc, &wethBalanceNodeAfter, nodePubkey)
		return nil
	}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, wethBalanceNodeAfter.Cmp(wethBalanceNodeBefore))
}

// Mint old RPL for unit testing
func MintLegacyRpl(rp *rocketpool.RocketPool, deployerOpts *bind.TransactOpts, receiver common.Address, amount *big.Int) (*eth.TransactionInfo, error) {
	fsrpl, err := rp.GetContract(rocketpool.ContractName_RocketTokenRPLFixedSupply)
	if err != nil {
		return nil, fmt.Errorf("error creating legacy RPL contract: %w", err)
	}
	return rp.GetTransactionManager().CreateTransactionInfo(fsrpl.Contract, "mint", deployerOpts, receiver, amount)
}

func MineTx(t *testing.T, txInfo *eth.TransactionInfo, opts *bind.TransactOpts, logMessage string) {
	// Check the simulation
	require.True(t, txInfo.SimulationResult.IsSimulated)
	require.Empty(t, txInfo.SimulationResult.SimulationError)

	txMgr := testMgr.GetServiceProvider().GetTransactionManager()

	// Submit the tx
	submission, _ := eth.CreateTxSubmissionFromInfo(txInfo, nil)
	tx, err := txMgr.ExecuteTransaction(txInfo,
		&bind.TransactOpts{
			From:      opts.From,
			Signer:    opts.Signer,
			GasLimit:  submission.GasLimit,
			Value:     submission.TxInfo.Value,
			Nonce:     nil,
			GasPrice:  nil,
			GasFeeCap: nil,
			GasTipCap: nil,
			Context:   opts.Context,
			NoSend:    opts.NoSend,
		},
	)
	require.NoError(t, err)

	// Mine the tx
	err = testMgr.CommitBlock()
	require.NoError(t, err)

	// Wait for the tx
	err = txMgr.WaitForTransaction(tx)
	require.NoError(t, err)
	t.Log(logMessage)
}

// Bootstraps a node into the Oracle DAO, taking care of all of the details involved
func BootstrapNodeToOdao(rp *rocketpool.RocketPool, deployerOpts *bind.TransactOpts, nodeAccount common.Address, nodeOpts *bind.TransactOpts, timezone string, id string, url string) (*node.Node, error) {
	// Get some contract bindings
	odaoMgr, err := oracle.NewOracleDaoManager(rp)
	if err != nil {
		return nil, fmt.Errorf("error getting oDAO manager binding: %w", err)
	}
	oma, err := rp.GetContract(rocketpool.ContractName_RocketDAONodeTrustedActions)
	if err != nil {
		return nil, fmt.Errorf("error getting OMA contract: %w", err)
	}
	fsrpl, err := tokens.NewTokenRplFixedSupply(rp)
	if err != nil {
		return nil, fmt.Errorf("error getting FSRPL binding: %w", err)
	}
	rpl, err := tokens.NewTokenRpl(rp)
	if err != nil {
		return nil, fmt.Errorf("error getting RPL binding: %w", err)
	}
	rplContract, err := rp.GetContract(rocketpool.ContractName_RocketTokenRPL)
	if err != nil {
		return nil, fmt.Errorf("error getting RPL contract: %w", err)
	}

	// Register the node
	node, err := RegisterNode(rp, nodeAccount, nodeOpts, timezone)
	if err != nil {
		return nil, fmt.Errorf("error registering node: %w", err)
	}

	// Get the amount of RPL to mint
	oSettings := odaoMgr.Settings
	err = rp.Query(nil, nil, odaoMgr.MemberCount, oSettings.Member.RplBond)
	if err != nil {
		return nil, fmt.Errorf("error getting network info: %w", err)
	}
	rplBond := oSettings.Member.RplBond.Get()

	// Bootstrap it and mint RPL for it
	err = rp.BatchCreateAndWaitForTransactions([]func() (*eth.TransactionSubmission, error){
		func() (*eth.TransactionSubmission, error) {
			return eth.CreateTxSubmissionFromInfo(odaoMgr.BootstrapMember(id, url, nodeAccount, deployerOpts))
		},
		func() (*eth.TransactionSubmission, error) {
			return eth.CreateTxSubmissionFromInfo(MintLegacyRpl(rp, deployerOpts, nodeAccount, rplBond))
		},
	}, true, deployerOpts)
	if err != nil {
		return nil, fmt.Errorf("error bootstrapping node and minting RPL: %w", err)
	}

	// Swap RPL and Join the oDAO
	err = rp.BatchCreateAndWaitForTransactions([]func() (*eth.TransactionSubmission, error){
		func() (*eth.TransactionSubmission, error) {
			return eth.CreateTxSubmissionFromInfo(fsrpl.Approve(rplContract.Address, rplBond, nodeOpts))
		},
		func() (*eth.TransactionSubmission, error) {
			return eth.CreateTxSubmissionFromInfo(rpl.SwapFixedSupplyRplForRpl(rplBond, nodeOpts))
		},
		func() (*eth.TransactionSubmission, error) {
			return eth.CreateTxSubmissionFromInfo(rpl.Approve(oma.Address, rplBond, nodeOpts))
		},
		func() (*eth.TransactionSubmission, error) {
			return eth.CreateTxSubmissionFromInfo(odaoMgr.Join(nodeOpts))
		},
	}, false, nodeOpts)
	if err != nil {
		return nil, fmt.Errorf("error joining oDAO: %w", err)
	}

	return node, nil
}

// Registers a new Rocket Pool node
func RegisterNode(rp *rocketpool.RocketPool, accountAddress common.Address, accountOpts *bind.TransactOpts, timezone string) (*node.Node, error) {
	// Create the node
	node, err := node.NewNode(rp, accountAddress)
	if err != nil {
		return nil, fmt.Errorf("error creating node %s: %w", accountAddress.Hex(), err)
	}

	// Register the node
	err = rp.CreateAndWaitForTransaction(func() (*eth.TransactionInfo, error) {
		return node.Register(timezone, accountOpts)
	}, true, accountOpts)
	if err != nil {
		return nil, fmt.Errorf("error registering node %s: %w", accountAddress.Hex(), err)
	}

	return node, nil
}
