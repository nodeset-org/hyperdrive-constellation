package qa

import (
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	cstesting "github.com/nodeset-org/hyperdrive-constellation/testing"
	"github.com/rocket-pool/node-manager-core/log"
)

// Various singleton variables used for testing
var (
	testMgr *cstesting.ConstellationTestManager
	logger  *slog.Logger
)

// Initialize a common server used by all tests
func TestMain(m *testing.M) {
	// Ignore the CS QA test suite during CI
	if os.Getenv("CI") != "" {
		fmt.Println("Skipping QA tests in CI")
		os.Exit(0)
	}

	var err error
	testMgr, err = cstesting.NewConstellationTestManager()
	if err != nil {
		fail("error creating test manager: %v", err)
	}
	logger = testMgr.GetLogger()

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
func createNewNode(primary *cstesting.ConstellationNode, newUserDir string) (*cstesting.ConstellationNode, common.Address, error) {
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
	err = testMgr.RegisterWithNodeset(csNode, nodeAddress)
	if err != nil {
		return nil, common.Address{}, fmt.Errorf("error registering with nodeset: %v", err)
	}

	return csNode, nodeAddress, nil
}
