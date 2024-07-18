package with_ns_registered

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/rocket-pool/node-manager-core/eth"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"

	hdtesting "github.com/nodeset-org/hyperdrive-daemon/testing"
	"github.com/nodeset-org/osha/keys"
	"github.com/rocket-pool/rocketpool-go/v2/deposit"
	"github.com/stretchr/testify/require"
)

const (
	expectedMinipoolCount int = 1
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

	// Get the private key for the Constellation deployer (the admin)
	keygen, err := keys.NewKeyGeneratorWithDefaults()
	require.NoError(t, err)
	adminPrivateKey, err := keygen.GetEthPrivateKey(0)
	subnodePrivateKey, err := keygen.GetEthPrivateKey(4)
	require.NoError(t, err)

	// Set up the NodeSet mock server
	hd := testMgr.HyperdriveTestManager.GetApiClient()
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	nsMgr.SetConstellationAdminPrivateKey(adminPrivateKey)
	nsMgr.SetConstellationWhitelistAddress(whitelistAddress)
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

	// Fund deposit pool
	csSp := testMgr.GetConstellationServiceProvider()
	txMgr := csSp.GetTransactionManager()
	rpMgr := csSp.GetRocketPoolManager()
	rpMgr.RefreshRocketPoolContracts()
	dpMgr, err := deposit.NewDepositPoolManager(rpMgr.RocketPool)
	if err != nil {
		fail("error creating deposit pool manager: %v", err)
	}

	opts, err := bind.NewKeyedTransactorWithChainID(subnodePrivateKey, big.NewInt(31337))
	if err != nil {
		fail("error creating transactor: %v", err)
	}

	fundOpts := &bind.TransactOpts{
		From:      opts.From,
		Nonce:     opts.Nonce,
		Signer:    opts.Signer,
		GasPrice:  opts.GasPrice,
		GasLimit:  opts.GasLimit,
		Value:     eth.EthToWei(64),
		Context:   opts.Context,
		GasFeeCap: opts.GasFeeCap,
		GasTipCap: opts.GasTipCap,
		NoSend:    opts.NoSend,
	}
	txInfo, err := dpMgr.Deposit(fundOpts)
	tx, err := txMgr.ExecuteTransaction(txInfo, opts)
	err = txMgr.WaitForTransaction(tx)
	if err != nil {
		fail("error waiting for transaction: %v", err)
	}
	err = rpMgr.RocketPool.Query(nil, nil,
		dpMgr.Balance,
	)
	if err != nil {
		fail("error querying deposit pool balance: %v", err)
	}
	fmt.Printf("Deposit pool balance: %.6f\n", eth.WeiToEth(dpMgr.Balance.Get()))

	// Mint RPL

	// Stake RPL

	// Whitelist subnode

	// Set available minipool count to 1
	nsMgr.SetAvailableConstellationMinipoolCount(nodeAddress, expectedMinipoolCount)

	// Deposit to the minipool
	depositResponse, err := cs.Minipool.Deposit(nodeAddress, big.NewInt(0))
	require.NoError(t, err)
	// require.False(t, depositResponse.Data.InsufficientLiquidity)
	require.False(t, depositResponse.Data.InsufficientMinipoolCount)
	require.False(t, depositResponse.Data.NotWhitelisted)

}
