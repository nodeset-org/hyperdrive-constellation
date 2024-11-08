package cstesting

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	hdservices "github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
	hdconfig "github.com/nodeset-org/hyperdrive-daemon/shared/config"
	hdtesting "github.com/nodeset-org/hyperdrive-daemon/testing"
	"github.com/nodeset-org/osha"
	vcdb "github.com/nodeset-org/osha/vc/db"
	vcmanager "github.com/nodeset-org/osha/vc/manager"
	vcserver "github.com/nodeset-org/osha/vc/server"
	"github.com/rocket-pool/node-manager-core/log"
	"github.com/rocket-pool/node-manager-core/node/validator/keymanager"
)

const (
	deploymentName string = "localtest"
)

type TestManagerOptions struct {
	// The hostname to run the VC Key Manager with
	KeyManagerHostname *string

	// The port to run the VC Key Manager with
	KeyManagerPort *uint16

	// Options for the VC Key Manager server
	KeyManagerOptions *vcdb.KeyManagerDatabaseOptions

	// Options for the key manager client
	KeyManagerClientOptions *keymanager.StandardKeyManagerClientOptions
}

// ConstellationTestManager for managing testing resources and services
type ConstellationTestManager struct {
	*hdtesting.HyperdriveTestManager

	// The complete Constellation node
	node *ConstellationNode

	// VC mock server for testing the VC key manager API
	vcMockServer *vcserver.VcMockServer

	// Key manager client
	keyManagerClient keymanager.IKeyManagerClient

	// VC Wait group for graceful shutdown
	vcWg *sync.WaitGroup

	// Map of which services were captured during a snapshot
	snapshotServiceMap map[string]hdtesting.Service

	// Snapshot ID from the baseline - the initial state of the VC prior to running any tests
	baselineSnapshotID string
}

// Creates a new TestManager instance
func NewConstellationTestManager(opts *TestManagerOptions) (*ConstellationTestManager, error) {
	tm, err := hdtesting.NewHyperdriveTestManagerWithDefaults(provisionNetworkSettings)
	if err != nil {
		return nil, fmt.Errorf("error creating test manager: %w", err)
	}

	// Set up the options
	if opts == nil {
		opts = &TestManagerOptions{}
	}
	if opts.KeyManagerHostname == nil {
		hostname := "localhost"
		opts.KeyManagerHostname = &hostname
	}
	if opts.KeyManagerPort == nil {
		port := uint16(5062)
		opts.KeyManagerPort = &port
	}
	if opts.KeyManagerOptions == nil {
		graffiti := vcdb.DefaultGraffiti
		root := common.BytesToHash(tm.GetBeaconMockManager().GetConfig().GenesisValidatorsRoot)
		jwt := vcdb.DefaultJwtSecret
		opts.KeyManagerOptions = &vcdb.KeyManagerDatabaseOptions{
			DefaultFeeRecipient:   &vcdb.DefaultFeeRecipient,
			DefaultGraffiti:       &graffiti,
			GenesisValidatorsRoot: &root,
			JwtSecret:             &jwt,
		}
	}

	// Get the HD artifacts
	hdNode := tm.GetNode()
	hdSp := hdNode.GetServiceProvider()
	hdCfg := hdSp.GetConfig()
	hdClient := hdNode.GetApiClient()

	// Make Constellation resources
	csResources := getTestResources(hdSp.GetResources(), deploymentName)
	csCfg, err := csconfig.NewConstellationConfig(hdCfg, []*csconfig.ConstellationSettings{})
	if err != nil {
		closeTestManager(tm)
		return nil, fmt.Errorf("error creating Constellation config: %v", err)
	}

	// Make the module directory
	moduleDir := filepath.Join(hdCfg.UserDataPath.Value, hdconfig.ModulesName, csconfig.ModuleName)
	err = os.MkdirAll(moduleDir, 0755)
	if err != nil {
		closeTestManager(tm)
		return nil, fmt.Errorf("error creating module directory [%s]: %v", moduleDir, err)
	}

	// Make the JWT file for the key manager client
	kmJwtSecretFile := filepath.Join(tm.GetTestDir(), "km_jwt_secret")
	err = os.WriteFile(kmJwtSecretFile, []byte(*opts.KeyManagerOptions.JwtSecret), 0644)
	if err != nil {
		closeTestManager(tm)
		return nil, fmt.Errorf("error writing Key Manager JWT secret file: %w", err)
	}

	// Make the VC mock server
	vcMockServer, err := vcserver.NewVcMockServer(tm.GetLogger(), *opts.KeyManagerHostname, *opts.KeyManagerPort, *opts.KeyManagerOptions)
	if err != nil {
		closeTestManager(tm)
		return nil, fmt.Errorf("error creating VC mock server: %w", err)
	}
	vcWg := &sync.WaitGroup{}
	vcMockServer.Start(vcWg)
	keyManagerClient, err := keymanager.NewStandardKeyManagerClient(
		fmt.Sprintf("http://%s:%d", *opts.KeyManagerHostname, *opts.KeyManagerPort),
		kmJwtSecretFile,
		opts.KeyManagerClientOptions,
	)
	if err != nil {
		closeTestManager(tm)
		return nil, fmt.Errorf("error creating Key Manager client: %w", err)
	}

	// Make a new service provider
	moduleSp, err := hdservices.NewModuleServiceProviderFromArtifacts(hdClient, hdCfg, csCfg, hdSp.GetResources(), moduleDir, csconfig.ModuleName, csconfig.ClientLogName, hdSp.GetEthClient(), hdSp.GetBeaconClient())
	if err != nil {
		closeTestManager(tm)
		return nil, fmt.Errorf("error creating service provider: %v", err)
	}
	csSp, err := cscommon.NewConstellationServiceProviderFromCustomServices(
		moduleSp,
		csCfg,
		csResources,
		&cscommon.ConstellationServiceProviderOptions{
			KeyManagerClient: keyManagerClient,
		},
	)
	if err != nil {
		closeTestManager(tm)
		return nil, fmt.Errorf("error creating constellation service provider: %v", err)
	}

	// Create the Constellation node
	node, err := newConstellationNode(csSp, "localhost", tm.GetLogger(), hdNode)
	if err != nil {
		closeTestManager(tm)
		return nil, fmt.Errorf("error creating Constellation node: %v", err)
	}

	// Disable automining
	err = tm.ToggleAutoMine(false)
	if err != nil {
		closeTestManager(tm)
		return nil, fmt.Errorf("error disabling automining: %v", err)
	}

	// Return
	m := &ConstellationTestManager{
		HyperdriveTestManager: tm,
		node:                  node,
		vcMockServer:          vcMockServer,
		keyManagerClient:      keyManagerClient,
		vcWg:                  vcWg,
		snapshotServiceMap:    map[string]hdtesting.Service{},
	}
	return m, nil
}

// Closes the test manager, shutting down the nodeset mock server and all other resources
func (m *ConstellationTestManager) Close() error {
	// Close the node
	err := m.node.Close()
	if err != nil {
		return fmt.Errorf("error closing Constellation node: %w", err)
	}

	// Close the HD test manager
	if m.HyperdriveTestManager != nil {
		err := m.HyperdriveTestManager.Close()
		if err != nil {
			return fmt.Errorf("error closing test manager: %w", err)
		}
		m.HyperdriveTestManager = nil
	}

	// Shut down the VC
	if m.vcWg != nil {
		logger := m.GetLogger()
		err = m.vcMockServer.Stop()
		if err != nil {
			logger.Warn("WARNING: VC mock server didn't shutdown cleanly", log.Err(err))
		}
		m.vcWg.Wait()
		logger.Info("Stopped Validator Client mock server")
		m.vcWg = nil
	}
	return nil
}

// ===============
// === Getters ===
// ===============

// Get the Constellation node handle
func (m *ConstellationTestManager) GetNode() *ConstellationNode {
	return m.node
}

// Get the VC mock manager
func (m *ConstellationTestManager) GetVcMockManager() *vcmanager.VcMockManager {
	return m.vcMockServer.GetManager()
}

// Get the Key Manager client
func (m *ConstellationTestManager) GetKeyManagerClient() keymanager.IKeyManagerClient {
	return m.keyManagerClient
}

// ====================
// === Snapshotting ===
// ====================

// Reverts the services to the baseline snapshot
func (m *ConstellationTestManager) RevertToBaseline() error {
	err := m.HyperdriveTestManager.RevertToBaseline()
	if err != nil {
		return fmt.Errorf("error reverting to baseline snapshot: %w", err)
	}

	// Regenerate the baseline snapshot since Hardhat can't revert to it multiple times
	baselineSnapshotID, err := m.takeSnapshot(hdtesting.Service_All)
	if err != nil {
		return fmt.Errorf("error creating baseline snapshot: %w", err)
	}
	m.baselineSnapshotID = baselineSnapshotID
	return nil
}

// Takes a snapshot of the service states
func (m *ConstellationTestManager) CreateCustomSnapshot(services hdtesting.Service) (string, error) {
	return m.takeSnapshot(services)
}

// Revert the services to a snapshot state
func (m *ConstellationTestManager) RevertToCustomSnapshot(snapshotID string) error {
	return m.revertToSnapshot(snapshotID)
}

// ==========================
// === Internal Functions ===
// ==========================

// Takes a snapshot of the service states
func (m *ConstellationTestManager) takeSnapshot(services hdtesting.Service) (string, error) {
	// Run the parent snapshotter
	snapshotName, err := m.HyperdriveTestManager.CreateCustomSnapshot(services)
	if err != nil {
		return "", fmt.Errorf("error taking snapshot: %w", err)
	}

	// Snapshot the VC
	if services.Contains(hdtesting.Service(osha.Service_EthClients)) {
		m.vcMockServer.GetManager().TakeSnapshot(snapshotName)
	}

	// Store the services that were captured
	m.snapshotServiceMap[snapshotName] = services
	return snapshotName, nil
}

// Revert the services to a snapshot state
func (m *ConstellationTestManager) revertToSnapshot(snapshotID string) error {
	services, exists := m.snapshotServiceMap[snapshotID]
	if !exists {
		return fmt.Errorf("snapshot with ID [%s] does not exist", snapshotID)
	}

	// Revert the VC
	if services.Contains(hdtesting.Service(osha.Service_EthClients)) {
		err := m.vcMockServer.GetManager().RevertToSnapshot(snapshotID)
		if err != nil {
			return fmt.Errorf("error reverting the VC mock to snapshot %s: %w", snapshotID, err)
		}
	}

	return m.TestManager.RevertToCustomSnapshot(snapshotID)
}

// Closes the Hyperdrive test manager, logging any errors
func closeTestManager(tm *hdtesting.HyperdriveTestManager) {
	err := tm.Close()
	if err != nil {
		tm.GetLogger().Error("Error closing test manager", log.Err(err))
	}
}
