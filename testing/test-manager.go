package cstesting

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	hdservices "github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
	hdconfig "github.com/nodeset-org/hyperdrive-daemon/shared/config"
	hdtesting "github.com/nodeset-org/hyperdrive-daemon/testing"
	"github.com/rocket-pool/node-manager-core/log"
)

const (
	deploymentName string = "localtest"
)

// ConstellationTestManager for managing testing resources and services
type ConstellationTestManager struct {
	*hdtesting.HyperdriveTestManager

	// The complete Constellation node
	node *ConstellationNode

	// The snapshot ID of the baseline snapshot
	baselineSnapshotID string
}

// Creates a new TestManager instance
func NewConstellationTestManager() (*ConstellationTestManager, error) {
	tm, err := hdtesting.NewHyperdriveTestManagerWithDefaults(provisionNetworkSettings)
	if err != nil {
		return nil, fmt.Errorf("error creating test manager: %w", err)
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

	// Make a new service provider
	moduleSp, err := hdservices.NewModuleServiceProviderFromArtifacts(hdClient, hdCfg, csCfg, hdSp.GetResources(), moduleDir, csconfig.ModuleName, csconfig.ClientLogName, hdSp.GetEthClient(), hdSp.GetBeaconClient())
	if err != nil {
		closeTestManager(tm)
		return nil, fmt.Errorf("error creating service provider: %v", err)
	}
	csSp, err := cscommon.NewConstellationServiceProviderFromCustomServices(moduleSp, csCfg, csResources)
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
	module := &ConstellationTestManager{
		HyperdriveTestManager: tm,
		node:                  node,
	}

	// err = module.SetupTest()
	// if err != nil {
	// 	return nil, fmt.Errorf("error setting up test: %w", err)
	// }

	tm.RegisterModule(module)
	baselineSnapshot, err := tm.CreateSnapshot()
	if err != nil {
		return nil, fmt.Errorf("error creating baseline snapshot: %w", err)
	}
	module.baselineSnapshotID = baselineSnapshot
	return module, nil
}

// ===============
// === Getters ===
// ===============

func (m *ConstellationTestManager) GetModuleName() string {
	return "hyperdrive-constellation"
}

// Get the Constellation node handle
func (m *ConstellationTestManager) GetNode() *ConstellationNode {
	return m.node
}

// ====================
// === Snapshotting ===
// ====================

// Reverts the service states to the baseline snapshot
func (m *ConstellationTestManager) DependsOnConstellationBaseline() error {
	err := m.RevertSnapshot(m.baselineSnapshotID)
	if err != nil {
		return fmt.Errorf("error reverting to baseline snapshot: %w", err)
	}
	return nil
}

func (m *ConstellationTestManager) TakeModuleSnapshot() (any, error) {
	snapshotName, err := m.HyperdriveTestManager.TakeModuleSnapshot()
	if err != nil {
		return nil, fmt.Errorf("error taking snapshot: %w", err)
	}
	return snapshotName, nil
}

func (m *ConstellationTestManager) RevertModuleToSnapshot(moduleState any) error {
	err := m.HyperdriveTestManager.RevertModuleToSnapshot(moduleState)
	if err != nil {
		return fmt.Errorf("error reverting to snapshot: %w", err)
	}
	return nil
}

// Closes the test manager, shutting down the nodeset mock server and all other resources
func (m *ConstellationTestManager) CloseModule() error {
	err := m.node.Close()
	if err != nil {
		return fmt.Errorf("error closing Constellation node: %w", err)
	}
	if m.HyperdriveTestManager != nil {
		err := m.HyperdriveTestManager.Close()
		if err != nil {
			return fmt.Errorf("error closing test manager: %w", err)
		}
		m.HyperdriveTestManager = nil
	}
	return nil
}

// ==========================
// === Internal Functions ===
// ==========================

// Closes the Hyperdrive test manager, logging any errors
func closeTestManager(tm *hdtesting.HyperdriveTestManager) {
	err := tm.Close()
	if err != nil {
		tm.GetLogger().Error("Error closing test manager", log.Err(err))
	}
}

// Register a node with nodeset
func (m *ConstellationTestManager) RegisterWithNodeset(node *ConstellationNode, address common.Address) error {
	// whitelist the node with the nodeset.io account
	nsServer := m.GetNodeSetMockServer().GetManager()
	nsDB := nsServer.GetDatabase()
	user, err := nsDB.Core.AddUser(address.Hex())
	if err != nil {
		return fmt.Errorf("error adding user to nodeset: %v", err)
	}
	_ = user.WhitelistNode(address)

	// Register with NodeSet
	hd := node.GetHyperdriveNode().GetApiClient()
	response, err := hd.NodeSet.RegisterNode(address.Hex())
	if err != nil {
		return fmt.Errorf("error registering node with nodeset: %v", err)
	}
	if response.Data.AlreadyRegistered {
		return fmt.Errorf("node is already registered with nodeset")
	}
	if response.Data.NotWhitelisted {
		return fmt.Errorf("node is not whitelisted with a nodeset user account")
	}
	return nil
}
