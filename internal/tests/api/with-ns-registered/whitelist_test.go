package with_ns_registered

import (
	"runtime/debug"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/nodeset-org/hyperdrive-constellation/common/contracts/constellation"
	hdtesting "github.com/nodeset-org/hyperdrive-daemon/testing"
	"github.com/nodeset-org/osha/keys"
	batchquery "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/node-manager-core/utils"
	"github.com/stretchr/testify/require"
)

const (
	whitelistString string = "0x3fdc08D815cc4ED3B7F69Ee246716f2C8bCD6b07"
	directoryString string = "0x71C95911E9a5D330f4D621842EC243EE1343292e"
)

var (
	whitelistAddress common.Address = common.HexToAddress(whitelistString)
	directoryAddress common.Address = common.HexToAddress(directoryString)
)

// Test registration with Constellation using a good signature
func TestConstellationRegistration(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

	// Get the private key for the Constellation deployer (the admin)
	keygen, err := keys.NewKeyGeneratorWithDefaults()
	require.NoError(t, err)
	adminKey, err := keygen.GetEthPrivateKey(0)
	require.NoError(t, err)

	// Assert the admin has the right role
	adminAddress := crypto.PubkeyToAddress(adminKey.PublicKey)
	t.Logf("Admin address: %s", adminAddress.Hex())
	roleHash := crypto.Keccak256Hash([]byte("ADMIN_SERVER_ROLE"))
	sp := testMgr.GetServiceProvider()
	qMgr := sp.GetQueryManager()
	directory, err := constellation.NewDirectory(directoryAddress, sp.GetEthClient(), sp.GetTransactionManager())
	require.NoError(t, err)
	var isAdmin bool
	err = qMgr.Query(func(mc *batchquery.MultiCaller) error {
		directory.HasRole(mc, &isAdmin, roleHash, adminAddress)
		return nil
	}, nil)
	require.NoError(t, err)
	require.True(t, isAdmin)
	t.Log("Admin has the right role")

	// Commit a block just so the latest block is fresh - otherwise the sync progress check will
	// error out because the block is too old and it thinks the client just can't find any peers
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

	// Make a signature
	hd := testMgr.HyperdriveTestManager.GetApiClient()
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	nsMgr.SetConstellationAdminPrivateKey(adminKey)
	nsMgr.SetConstellationWhitelistAddress(whitelistAddress)
	response, err := hd.NodeSet_Constellation.GetRegistrationSignature()
	require.NoError(t, err)
	require.False(t, response.Data.NotAuthorized)
	require.False(t, response.Data.NotRegistered)
	signature := response.Data.Signature
	t.Logf("Generated signature: %s", utils.EncodeHexWithPrefix(signature))

	// Make the registration tx
	opts := &bind.TransactOpts{
		From: nodeAddress,
	}
	whitelist := testMgr.GetConstellationServiceProvider().GetConstellationManager().Whitelist
	txInfo, err := whitelist.AddOperator(nodeAddress, signature, opts) // TODO: make this a route
	require.NoError(t, err)
	require.Empty(t, txInfo.SimulationResult.SimulationError)
	t.Log("Generated registration tx")

	// Submit the tx
	submission, _ := eth.CreateTxSubmissionFromInfo(txInfo, nil)
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
}
