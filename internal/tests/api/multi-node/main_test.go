package multi_node

import (
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	cstesting "github.com/nodeset-org/hyperdrive-constellation/testing"
	"github.com/nodeset-org/osha/keys"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/node-manager-core/log"
	"github.com/rocket-pool/node-manager-core/wallet"
	"github.com/rocket-pool/rocketpool-go/v2/node"
)

// Various singleton variables used for testing
var (
	testMgr      *cstesting.ConstellationTestManager
	logger       *slog.Logger
	nsEmail      string = "test@nodeset.io"
	keygen       *keys.KeyGenerator
	deployerOpts *bind.TransactOpts
	adminOpts    *bind.TransactOpts

	// CS nodes
	mainNode        *cstesting.ConstellationNode
	mainNodeAddress common.Address

	node2        *cstesting.ConstellationNode
	node2Address common.Address

	node3        *cstesting.ConstellationNode
	node3Address common.Address

	node4        *cstesting.ConstellationNode
	node4Address common.Address

	node5        *cstesting.ConstellationNode
	node5Address common.Address

	// Oracle DAO
	odaoOpts  []*bind.TransactOpts
	odaoNodes []*node.Node
)

// Initialize a common server used by all tests
func TestMain(m *testing.M) {
	var err error
	testMgr, err = cstesting.NewConstellationTestManager()
	if err != nil {
		fail("error creating test manager: %v", err)
	}
	logger = testMgr.GetLogger()
	mainNode = testMgr.GetNode()

	// Generate a new wallet
	derivationPath := string(wallet.DerivationPath_Default)
	index := uint64(4)
	password := "test_password123"
	hdNode := mainNode.GetHyperdriveNode()
	hd := hdNode.GetApiClient()
	recoverResponse, err := hd.Wallet.Recover(&derivationPath, keys.DefaultMnemonic, &index, password, true)
	if err != nil {
		fail("error generating wallet: %v", err)
	}
	mainNodeAddress = recoverResponse.Data.AccountAddress

	// Make a NodeSet account
	nsServer := testMgr.GetNodeSetMockServer().GetManager()
	err = nsServer.AddUser(nsEmail)
	if err != nil {
		fail("error adding user to nodeset: %v", err)
	}

	// Register the primary
	err = registerWithNodeset(mainNode, mainNodeAddress)
	if err != nil {
		fail("error registering with nodeset: %v", err)
	}

	// Make the subnodes
	basePath := testMgr.GetTestDir()
	node2, node2Address, err = createNewNode(nil, mainNode, filepath.Join(basePath, "node2"))
	node3, node3Address, err = createNewNode(err, mainNode, filepath.Join(basePath, "node3"))
	node4, node4Address, err = createNewNode(err, mainNode, filepath.Join(basePath, "node4"))
	node5, node5Address, err = createNewNode(err, mainNode, filepath.Join(basePath, "node5"))
	if err != nil {
		fail("error creating subnodes: %v", err)
	}
	subnodeAddrs := []common.Address{node2Address, node3Address, node4Address, node5Address}
	for i, addr := range subnodeAddrs {
		logger.Info(
			"Created subnode",
			slog.Int("index", i+2),
			slog.String("address", addr.Hex()),
		)
	}

	// Mine a block to make the sync check pass
	err = testMgr.CommitBlock()
	if err != nil {
		fail("error committing block: %v", err)
	}

	// Send the subnodes some ETH
	amount := eth.EthToWei(100)
	submissions := make([]*eth.TransactionSubmission, len(subnodeAddrs))
	for i, addr := range subnodeAddrs {
		resp, err := hd.Wallet.Send(amount, "eth", addr)
		if err != nil {
			fail("error sending ETH to subnode %d: %v", i, err)
		}
		if !resp.Data.CanSend {
			fail("can't send ETH to subnode %d: insufficient balance", i)
		}
		submission, _ := eth.CreateTxSubmissionFromInfo(resp.Data.TxInfo, nil)
		submissions[i] = submission
	}
	submitResp, err := hd.Tx.SubmitTxBatch(submissions, nil, eth.GweiToWei(10), eth.GweiToWei(0.5))
	if err != nil {
		fail("error submitting ETH send tx batch: %v", err)
	}
	err = testMgr.CommitBlock()
	if err != nil {
		fail("error committing block: %v", err)
	}
	for i, hash := range submitResp.Data.TxHashes {
		_, err = hd.Tx.WaitForTransaction(hash)
		if err != nil {
			fail("error waiting for ETH send tx %d: %v", i, err)
		}
	}

	// Get the private key for the RP and Constellation deployer
	keygen, err = keys.NewKeyGeneratorWithDefaults()
	if err != nil {
		fail("error creating key generator: %v", err)
	}
	deployerKey, err := keygen.GetEthPrivateKey(0)
	if err != nil {
		fail("error getting deployer key: %v", err)
	}
	chainID := testMgr.GetBeaconMockManager().GetConfig().ChainID
	deployerOpts, err = bind.NewKeyedTransactorWithChainID(deployerKey, big.NewInt(int64(chainID)))
	if err != nil {
		fail("error creating deployer transactor: %v", err)
	}

	// Get the private key for the Constellation admin
	adminKey, err := keygen.GetEthPrivateKey(1)
	if err != nil {
		fail("error getting admin key: %v", err)
	}
	adminOpts, err = bind.NewKeyedTransactorWithChainID(adminKey, big.NewInt(int64(chainID)))
	if err != nil {
		fail("error creating admin transactor: %v", err)
	}

	// Set up the services
	sp := mainNode.GetServiceProvider()
	rpMgr := sp.GetRocketPoolManager()
	err = rpMgr.RefreshRocketPoolContracts()
	if err != nil {
		fail("error refreshing Rocket Pool contracts: %v", err)
	}
	csMgr := sp.GetConstellationManager()
	err = csMgr.LoadContracts()
	if err != nil {
		fail("error loading Constellation contracts: %v", err)
	}

	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	nsMgr.SetConstellationAdminPrivateKey(deployerKey)

	// Bootstrap the oDAO - indices are addresses 10-12
	odaoNodes, odaoOpts, err = testMgr.RocketPool_CreateOracleDaoNodesWithDefaults(keygen, big.NewInt(int64(chainID)), []uint{10, 11, 12}, deployerOpts)
	if err != nil {
		fail("error creating oDAO nodes: %v", err)
	}

	// Run tests
	code := m.Run()

	// Clean up and exit
	cleanup()
	os.Exit(code)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
	cleanup()
	os.Exit(1)
}

func cleanup() {
	if testMgr == nil {
		return
	}
	err := testMgr.Close()
	if err != nil {
		logger.Error("Error closing test manager", log.Err(err))
	}
	testMgr = nil
}

// Create a new node pair with a given user directory, initialize its wallet, and register it with nodeset
func createNewNode(err error, primary *cstesting.ConstellationNode, newUserDir string) (*cstesting.ConstellationNode, common.Address, error) {
	if err != nil {
		return nil, common.Address{}, err
	}

	// Make the HD node
	hdNode, err := primary.GetHyperdriveNode().CreateSubNode(newUserDir, "localhost", 0)
	if err != nil {
		return nil, common.Address{}, fmt.Errorf("error creating HD subnode: %v", err)
	}

	// Make the CS node
	csNode, err := primary.CreateSubNode(hdNode, "localhost", 0)
	if err != nil {
		return nil, common.Address{}, fmt.Errorf("error creating CS subnode: %v", err)
	}

	// Generate a new wallet
	password := "test_password123"
	hd := hdNode.GetApiClient()
	initResponse, err := hd.Wallet.Initialize(nil, nil, true, password, true)
	if err != nil {
		fail("error generating wallet: %v", err)
	}
	nodeAddress := initResponse.Data.AccountAddress

	// Register with nodeset
	err = registerWithNodeset(csNode, nodeAddress)
	if err != nil {
		return nil, common.Address{}, fmt.Errorf("error registering with nodeset: %v", err)
	}

	return csNode, nodeAddress, nil
}

// Register a node with nodeset
func registerWithNodeset(node *cstesting.ConstellationNode, address common.Address) error {
	// whitelist the node with the nodeset.io account
	nsServer := testMgr.GetNodeSetMockServer().GetManager()
	err := nsServer.WhitelistNodeAccount(nsEmail, address)
	if err != nil {
		fail("error adding node account to nodeset: %v", err)
	}

	// Register with NodeSet
	hd := node.GetHyperdriveNode().GetApiClient()
	response, err := hd.NodeSet.RegisterNode(nsEmail)
	if err != nil {
		fail("error registering node with nodeset: %v", err)
	}
	if response.Data.AlreadyRegistered {
		fail("node is already registered with nodeset")
	}
	if response.Data.NotWhitelisted {
		fail("node is not whitelisted with a nodeset user account")
	}
	return nil
}
