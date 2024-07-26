package with_ns_registered

import (
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	cstesting "github.com/nodeset-org/hyperdrive-constellation/testing"
	"github.com/nodeset-org/osha/keys"
	"github.com/rocket-pool/node-manager-core/log"
	"github.com/rocket-pool/node-manager-core/wallet"
	"github.com/rocket-pool/rocketpool-go/v2/node"
)

// Various singleton variables used for testing
var (
	testMgr      *cstesting.ConstellationTestManager
	wg           *sync.WaitGroup
	logger       *slog.Logger
	nodeAddress  common.Address
	nsEmail      string = "test@nodeset.io"
	keygen       *keys.KeyGenerator
	deployerOpts *bind.TransactOpts

	// Oracle DAO
	odaoOpts  []*bind.TransactOpts
	odaoNodes []*node.Node
)

// Initialize a common server used by all tests
func TestMain(m *testing.M) {
	wg = &sync.WaitGroup{}
	var err error
	testMgr, err = cstesting.NewConstellationTestManager("localhost", "localhost", "localhost")
	if err != nil {
		fail("error creating test manager: %v", err)
	}
	logger = testMgr.GetLogger()

	// Generate a new wallet
	derivationPath := string(wallet.DerivationPath_Default)
	index := uint64(4)
	password := "test_password123"
	hd := testMgr.HyperdriveTestManager.GetApiClient()
	recoverResponse, err := hd.Wallet.Recover(&derivationPath, keys.DefaultMnemonic, &index, password, true)
	if err != nil {
		fail("error generating wallet: %v", err)
	}
	nodeAddress = recoverResponse.Data.AccountAddress

	// Make a NodeSet account
	nsServer := testMgr.GetNodeSetMockServer().GetManager()
	err = nsServer.AddUser(nsEmail)
	if err != nil {
		fail("error adding user to nodeset: %v", err)
	}
	err = nsServer.WhitelistNodeAccount(nsEmail, nodeAddress)
	if err != nil {
		fail("error adding node account to nodeset: %v", err)
	}

	// Register with NodeSet
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

	// Set up the services
	sp := testMgr.GetConstellationServiceProvider()
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
