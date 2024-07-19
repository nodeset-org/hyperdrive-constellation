package with_ns_registered

import (
	"fmt"
	"math/big"
	"testing"

	batch "github.com/rocket-pool/batch-query"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rocket-pool/node-manager-core/eth"

	hdtesting "github.com/nodeset-org/hyperdrive-daemon/testing"
	"github.com/nodeset-org/osha/keys"
	"github.com/rocket-pool/rocketpool-go/v2/deposit"
	"github.com/rocket-pool/rocketpool-go/v2/node"
	"github.com/rocket-pool/rocketpool-go/v2/rocketpool"
	"github.com/rocket-pool/rocketpool-go/v2/tokens"
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

// func TestMinipoolDeposit(t *testing.T) {
// 	// Take a snapshot, revert at the end
// 	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
// 	if err != nil {
// 		fail("Error creating custom snapshot: %v", err)
// 	}
// 	defer nodeset_cleanup(snapshotName)

// 	// Get the private key for the RP and Constellation deployer
// 	// keygen, err := keys.NewKeyGeneratorWithDefaults()
// 	// require.NoError(t, err)
// 	// deployerKey, err := keygen.GetEthPrivateKey(0)
// 	// require.NoError(t, err)
// 	// deployerPubkey := crypto.PubkeyToAddress(deployerKey.PublicKey)
// 	// t.Logf("Deployer key: %s\n", deployerPubkey.Hex())
// 	// chainID := testMgr.GetBeaconMockManager().GetConfig().ChainID
// 	// deployerOpts, err := bind.NewKeyedTransactorWithChainID(deployerKey, big.NewInt(int64(chainID)))
// 	// require.NoError(t, err)

// 	// // Get the admin key for Constellation
// 	// adminKey, err := keygen.GetEthPrivateKey(1)
// 	// require.NoError(t, err)
// 	// adminPubkey := crypto.PubkeyToAddress(adminKey.PublicKey)
// 	// t.Logf("Admin key: %s\n", adminPubkey.Hex())
// 	// adminOpts, err := bind.NewKeyedTransactorWithChainID(adminKey, big.NewInt(int64(chainID)))
// 	// require.NoError(t, err)

// 	// Set up the services
// 	// sp := testMgr.GetConstellationServiceProvider()
// 	// qMgr := sp.GetQueryManager()

// 	// // Load RP
// 	// rpMgr := sp.GetRocketPoolManager()
// 	// err = rpMgr.RefreshRocketPoolContracts()
// 	// require.NoError(t, err)
// 	// rp := rpMgr.RocketPool
// 	// t.Log("Loaded Rocket Pool")

// 	// Load Constellation
// 	// csMgr := sp.GetConstellationManager()
// 	// err = csMgr.LoadContracts()
// 	// require.NoError(t, err)
// 	// t.Log("Loaded Constellation")

// 	// Make some RP bindings
// 	// dpMgr, err := deposit.NewDepositPoolManager(rp)
// 	// require.NoError(t, err)
// 	// fsrpl, err := tokens.NewTokenRplFixedSupply(rp)
// 	// require.NoError(t, err)
// 	// rpl, err := tokens.NewTokenRpl(rp)
// 	// require.NoError(t, err)
// 	// //nodeMgr, err := node.NewNodeManager(rp)
// 	// t.Log("Created Rocket Pool bindings")

// 	// Make Constellation use fallback mode for now until oDAO RPL price is working
// 	// txInfo, err := csMgr.PriceFetcher.UseFallback(adminOpts)
// 	// require.NoError(t, err)
// 	// MineTx(t, txInfo, adminOpts, "Enabled fallback mode for RPL price fetching")

// 	// Run a query
// 	// supernodeAddress := csMgr.SuperNodeAccount.Address
// 	// rpSuperNode, err := node.NewNode(rp, supernodeAddress)
// 	// require.NoError(t, err)
// 	// var rplPrice *big.Int
// 	// var rplRequired *big.Int
// 	// leb8BondInWei := eth.EthToWei(ethBondPerLeb8)
// 	// err = qMgr.Query(func(mc *batch.MultiCaller) error {
// 	// 	csMgr.PriceFetcher.GetRplPrice(mc, &rplPrice)
// 	// 	csMgr.OperatorDistributor.CalculateRplStakeShortfall(mc, &rplRequired, common.Big0, leb8BondInWei)
// 	// 	return nil
// 	// }, nil,
// 	// 	rpSuperNode.Exists,
// 	// 	dpMgr.Balance,
// 	// )
// 	// require.NoError(t, err)

// 	// // Verify some details
// 	// require.True(t, rpSuperNode.Exists.Get())
// 	// t.Log("Supernode account is registered with RP")
// 	// require.Equal(t, 0, dpMgr.Balance.Get().Cmp(common.Big0))
// 	// t.Log("Deposit pool balance is zero")
// 	// require.Equal(t, 1, rplPrice.Cmp(common.Big0))
// 	// t.Logf("RPL price is %.6f ETH (%s wei)", eth.WeiToEth(rplPrice), rplPrice.String())
// 	// t.Logf("RPL required for 8 ETH bond is %.6f RPL (%s wei)", eth.WeiToEth(rplRequired), rplRequired.String())

// 	// Mint some old RPL
// 	rplAmount := 1000
// 	rplAmountWei := eth.EthToWei(1000)
// 	txInfo, err = MintLegacyRpl(rp, deployerOpts, deployerPubkey, rplAmountWei)
// 	require.NoError(t, err)
// 	MineTx(t, txInfo, deployerOpts, fmt.Sprintf("Minted %d old RPL", rplAmount))

// 	// Approve old RPL for swap
// 	rplContract, err := rp.GetContract(rocketpool.ContractName_RocketTokenRPL)
// 	require.NoError(t, err)
// 	txInfo, err = fsrpl.Approve(rplContract.Address, rplAmountWei, deployerOpts)
// 	require.NoError(t, err)
// 	MineTx(t, txInfo, deployerOpts, "Approved old RPL for swap")

// 	// Swap it to new RPL
// 	txInfo, err = rpl.SwapFixedSupplyRplForRpl(rplAmountWei, deployerOpts)
// 	require.NoError(t, err)
// 	MineTx(t, txInfo, deployerOpts, "Swapped old RPL for new RPL")

// 	// Send it to the Supernode
// 	txInfo, err = rpl.Transfer(supernodeAddress, rplAmountWei, deployerOpts)
// 	require.NoError(t, err)
// 	MineTx(t, txInfo, deployerOpts, "Sent new RPL to Supernode")
// 	var supernodeRplBalance *big.Int
// 	err = qMgr.Query(func(mc *batch.MultiCaller) error {
// 		rpl.BalanceOf(mc, &supernodeRplBalance, supernodeAddress)
// 		return nil
// 	}, nil)
// 	require.NoError(t, err)
// 	require.Equal(t, 0, rplAmountWei.Cmp(supernodeRplBalance))
// 	t.Logf("Supernode RPL balance is now %d", supernodeRplBalance)
// }

func TestMinipoolDeposit(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

	err = testMgr.CommitBlock()
	if err != nil {
		t.Fatalf("Error committing block: %v", err)
	}

	// Check if the node is registered
	cs := testMgr.GetApiClient()
	statusResponse, err := cs.Node.GetRegistrationStatus()
	require.NoError(t, err)
	require.False(t, statusResponse.Data.Registered)
	t.Log("Node is not registered with Constellation yet, as expected")

	chainID := testMgr.GetBeaconMockManager().GetConfig().ChainID

	// Get the private key for the Constellation deployer (the admin)
	keygen, err := keys.NewKeyGeneratorWithDefaults()
	require.NoError(t, err)

	deployerKey, err := keygen.GetEthPrivateKey(0)
	deployerPubkey := crypto.PubkeyToAddress(deployerKey.PublicKey)
	require.NoError(t, err)
	deployerOpts, err := bind.NewKeyedTransactorWithChainID(deployerKey, big.NewInt(int64(chainID)))
	require.NoError(t, err)

	adminKey, err := keygen.GetEthPrivateKey(1)
	require.NoError(t, err)
	adminOpts, err := bind.NewKeyedTransactorWithChainID(adminKey, big.NewInt(int64(chainID)))
	require.NoError(t, err)

	// Setup services
	hd := testMgr.HyperdriveTestManager.GetApiClient()
	sp := testMgr.GetConstellationServiceProvider()
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	// txMgr := sp.GetTransactionManager()
	qMgr := sp.GetQueryManager()
	csMgr := sp.GetConstellationManager()
	err = csMgr.LoadContracts()
	require.NoError(t, err)
	t.Log("Loaded Constellation contracts")

	rpMgr := sp.GetRocketPoolManager()
	rp := rpMgr.RocketPool
	rpMgr.RefreshRocketPoolContracts()
	t.Log("Loaded Rocketpool contracts")

	// Set up the NodeSet mock server
	nsMgr.SetConstellationAdminPrivateKey(deployerKey)
	nsMgr.SetConstellationWhitelistAddress(whitelistAddress)
	// Set available minipool count to 1
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

	// Make RP bindings
	dpMgr, err := deposit.NewDepositPoolManager(rp)
	require.NoError(t, err)
	fsrpl, err := tokens.NewTokenRplFixedSupply(rp)
	require.NoError(t, err)
	rpl, err := tokens.NewTokenRpl(rp)
	require.NoError(t, err)
	t.Log("Created Rocket Pool bindings")

	// Make Constellation use fallback mode for now until oDAO RPL price is working
	txInfo, err := csMgr.PriceFetcher.UseFallback(adminOpts)
	require.NoError(t, err)
	MineTx(t, txInfo, adminOpts, "Enabled fallback mode for RPL price fetching")

	// Get RPL required from contracts for 8 eth deposit
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
	)
	require.True(t, rpSuperNode.Exists.Get())
	t.Log("Supernode account is registered with RP")
	require.Equal(t, 0, dpMgr.Balance.Get().Cmp(common.Big0))
	t.Log("Deposit pool balance is zero")
	require.Equal(t, 1, rplPrice.Cmp(common.Big0))
	t.Logf("RPL price is %.6f ETH (%s wei)", eth.WeiToEth(rplPrice), rplPrice.String())
	t.Logf("RPL required for 8 ETH bond is %.6f RPL (%s wei)", eth.WeiToEth(rplRequired), rplRequired.String())

	// Mint old RPL and swap for new RPL
	rplAmount := 1000
	rplAmountWei := eth.EthToWei(1000)
	txInfo, err = MintLegacyRpl(rpMgr.RocketPool, deployerOpts, deployerPubkey, rplAmountWei)
	require.NoError(t, err)
	MineTx(t, txInfo, deployerOpts, fmt.Sprintf("Minted %d old RPL", rplAmount))

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

	// Send to Supernode
	txInfo, err = rpl.Transfer(supernodeAddress, rplAmountWei, deployerOpts)
	require.NoError(t, err)
	MineTx(t, txInfo, deployerOpts, "Sent new RPL to Supernode")
	var supernodeRplBalance *big.Int
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		rpl.BalanceOf(mc, &supernodeRplBalance, supernodeAddress)
		return nil
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 0, rplAmountWei.Cmp(supernodeRplBalance))
	t.Logf("Supernode RPL balance is now %d", supernodeRplBalance)

	// TODO: Get rest from Joe

	depositResponse, err := cs.Minipool.Deposit(nodeAddress, big.NewInt(0))
	require.NoError(t, err)
	require.False(t, depositResponse.Data.InsufficientLiquidity)
	require.False(t, depositResponse.Data.InsufficientMinipoolCount)
	require.False(t, depositResponse.Data.NotWhitelisted)

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
