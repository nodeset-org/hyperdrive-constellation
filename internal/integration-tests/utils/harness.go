package cstestutils

import (
	"fmt"
	"log/slog"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	cstesting "github.com/nodeset-org/hyperdrive-constellation/testing"
	hdtesting "github.com/nodeset-org/hyperdrive-daemon/testing"
	"github.com/nodeset-org/osha/keys"
	"github.com/rocket-pool/node-manager-core/wallet"
	"github.com/rocket-pool/rocketpool-go/v2/node"
)

const (
	DefaultNodeSetEmail string = "test@nodeset.io"
)

type StandardTestHarness struct {
	TestManager  *cstesting.ConstellationTestManager
	Logger       *slog.Logger
	NodeSetEmail string
	KeyGenerator *keys.KeyGenerator
	DeployerOpts *bind.TransactOpts
	AdminOpts    *bind.TransactOpts
	Bindings     *ContractBindings

	// Primary CS node
	MainNode        *cstesting.ConstellationNode
	MainNodeAddress common.Address

	// Oracle DAO
	OdaoOpts  []*bind.TransactOpts
	OdaoNodes []*node.Node
}

type StandardTestHarnessOptions struct {
	WalletIndex            uint64
	SetupOracleDao         bool
	OracleDaoWalletIndices []uint
}

var (
	DefaultStandardTestHarnessOptions = &StandardTestHarnessOptions{
		WalletIndex:            4,
		SetupOracleDao:         true,
		OracleDaoWalletIndices: []uint{10, 11, 12},
	}
)

// Creates a standard test harness and bootstrapped chain deployment using the default configuration
func CreateStandardTestHarness(options *StandardTestHarnessOptions) (*StandardTestHarness, error) {
	if options == nil {
		options = DefaultStandardTestHarnessOptions
	}

	// Test manager
	testMgr, err := cstesting.NewConstellationTestManager()
	if err != nil {
		return nil, fmt.Errorf("error creating test manager: %v", err)
	}

	// Default keygen and contract deployment options
	keygen, err := keys.NewKeyGeneratorWithDefaults()
	if err != nil {
		return nil, fmt.Errorf("error creating key generator: %v", err)
	}
	deployerKey, err := keygen.GetEthPrivateKey(0) // Default is wallet 0
	if err != nil {
		return nil, fmt.Errorf("error getting deployer key: %v", err)
	}
	chainID := testMgr.GetBeaconMockManager().GetConfig().ChainID
	chainIDBig := big.NewInt(int64(chainID))
	deployerOpts, err := bind.NewKeyedTransactorWithChainID(deployerKey, chainIDBig)
	if err != nil {
		return nil, fmt.Errorf("error creating deployer transactor: %v", err)
	}

	// Default Constellation admin
	adminKey, err := keygen.GetEthPrivateKey(1) // Default is wallet 1
	if err != nil {
		return nil, fmt.Errorf("error getting admin key: %v", err)
	}
	adminOpts, err := bind.NewKeyedTransactorWithChainID(adminKey, chainIDBig)
	if err != nil {
		return nil, fmt.Errorf("error creating admin transactor: %v", err)
	}

	// Create the main node and recover a wallet
	mainNode := testMgr.GetNode()
	derivationPath := string(wallet.DerivationPath_Default)
	password := "test_password123"
	hdNode := mainNode.GetHyperdriveNode()
	hd := hdNode.GetApiClient()
	recoverResponse, err := hd.Wallet.Recover(&derivationPath, keys.DefaultMnemonic, &options.WalletIndex, password, true)
	if err != nil {
		return nil, fmt.Errorf("error generating wallet: %v", err)
	}
	mainNodeAddress := recoverResponse.Data.AccountAddress

	// Make a NodeSet account
	email := DefaultNodeSetEmail
	nsMgr := testMgr.GetNodeSetMockServer().GetManager()
	nsDB := nsMgr.GetDatabase()
	user, err := nsDB.Core.AddUser(email)
	if err != nil {
		return nil, fmt.Errorf("error adding user to nodeset: %v", err)
	}
	_ = user.WhitelistNode(mainNodeAddress)
	if err != nil {
		return nil, fmt.Errorf("error adding node account to nodeset: %v", err)
	}

	// Register with NodeSet
	response, err := hd.NodeSet.RegisterNode(email)
	if err != nil {
		return nil, fmt.Errorf("error registering node with nodeset: %v", err)
	}
	if response.Data.AlreadyRegistered {
		return nil, fmt.Errorf("node is already registered with nodeset")
	}
	if response.Data.NotWhitelisted {
		return nil, fmt.Errorf("node is not whitelisted with a nodeset user account")
	}

	// Set up the services
	sp := mainNode.GetServiceProvider()
	rpMgr := sp.GetRocketPoolManager()
	err = rpMgr.RefreshRocketPoolContracts()
	if err != nil {
		return nil, fmt.Errorf("error refreshing Rocket Pool contracts: %v", err)
	}
	csMgr := sp.GetConstellationManager()
	err = csMgr.LoadContracts()
	if err != nil {
		return nil, fmt.Errorf("error loading Constellation contracts: %v", err)
	}
	res := sp.GetResources()
	deployment := nsDB.Constellation.AddDeployment(
		res.DeploymentName,
		new(big.Int).SetUint64(uint64(res.ChainID)),
		csMgr.Whitelist.Address,
		csMgr.SuperNodeAccount.Address,
	)
	deployment.SetAdminPrivateKey(deployerKey)
	nsDB.SetSecretEncryptionIdentity(hdtesting.EncryptionIdentity)

	// Make the contract bindings
	bindings, err := CreateBindings(sp)
	if err != nil {
		return nil, fmt.Errorf("error creating contract bindings: %v", err)
	}

	// Setup the Oracle DAO if requested
	var odaoNodes []*node.Node
	var odaoOpts []*bind.TransactOpts
	if options.SetupOracleDao {
		// Setup the Oracle DAO
		odaoNodes, odaoOpts, err = testMgr.RocketPool_CreateOracleDaoNodesWithDefaults(keygen, chainIDBig, options.OracleDaoWalletIndices, deployerOpts)
		if err != nil {
			return nil, fmt.Errorf("error creating oDAO nodes: %v", err)
		}
	}

	return &StandardTestHarness{
		TestManager:     testMgr,
		Logger:          testMgr.GetLogger(),
		NodeSetEmail:    DefaultNodeSetEmail,
		KeyGenerator:    keygen,
		DeployerOpts:    deployerOpts,
		AdminOpts:       adminOpts,
		Bindings:        bindings,
		MainNode:        mainNode,
		MainNodeAddress: mainNodeAddress,
		OdaoOpts:        odaoOpts,
		OdaoNodes:       odaoNodes,
	}, nil
}

func (h *StandardTestHarness) Close() error {
	if h.TestManager == nil {
		return nil
	}
	return h.TestManager.Close()
}
