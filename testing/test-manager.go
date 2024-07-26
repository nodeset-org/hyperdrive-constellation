package cstesting

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	csclient "github.com/nodeset-org/hyperdrive-constellation/client"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	csserver "github.com/nodeset-org/hyperdrive-constellation/server"
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	hdservices "github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
	hdconfig "github.com/nodeset-org/hyperdrive-daemon/shared/config"
	hdtesting "github.com/nodeset-org/hyperdrive-daemon/testing"
	"github.com/rocket-pool/node-manager-core/log"
)

// ConstellationTestManager for managing testing resources and services
type ConstellationTestManager struct {
	*hdtesting.HyperdriveTestManager

	// The service provider for the test environment
	sp cscommon.IConstellationServiceProvider

	// The Constellation Daemon server
	serverMgr *csserver.ServerManager

	// The Constellation Daemon client
	apiClient *csclient.ApiClient

	// Wait group for graceful shutdown
	swWg *sync.WaitGroup
}

// Creates a new TestManager instance
// `hdAddress` is the address to bind the Hyperdrive daemon to.
// `csAddress` is the address to bind the Constellation daemon to.
// `nsAddress` is the address to bind the nodeset.io mock server to.
func NewConstellationTestManager(hdAddress string, csAddress string, nsAddress string) (*ConstellationTestManager, error) {
	tm, err := hdtesting.NewHyperdriveTestManagerWithDefaults(hdAddress, nsAddress, provisionNetworkSettings)
	if err != nil {
		return nil, fmt.Errorf("error creating test manager: %w", err)
	}

	// Get the HD artifacts
	hdSp := tm.GetServiceProvider()
	hdCfg := hdSp.GetConfig()
	hdClient := tm.GetApiClient()

	// Make Constellation resources
	csResources, snResources := getTestResources(hdSp.GetResources())
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
	constellationSp, err := cscommon.NewConstellationServiceProviderFromCustomServices(moduleSp, csCfg, csResources, snResources)
	if err != nil {
		closeTestManager(tm)
		return nil, fmt.Errorf("error creating constellation service provider: %v", err)
	}

	// Create the server
	swWg := &sync.WaitGroup{}
	serverMgr, err := csserver.NewServerManager(constellationSp, csAddress, 0, swWg)
	if err != nil {
		closeTestManager(tm)
		return nil, fmt.Errorf("error creating constellation server: %v", err)
	}

	// Create the client
	urlString := fmt.Sprintf("http://%s:%d/%s", csAddress, serverMgr.GetPort(), csconfig.ApiClientRoute)
	url, err := url.Parse(urlString)
	if err != nil {
		closeTestManager(tm)
		return nil, fmt.Errorf("error parsing client URL [%s]: %v", urlString, err)
	}
	apiClient := csclient.NewApiClient(url, tm.GetLogger(), nil)

	// Return
	m := &ConstellationTestManager{
		HyperdriveTestManager: tm,
		sp:                    constellationSp,
		serverMgr:             serverMgr,
		apiClient:             apiClient,
		swWg:                  swWg,
	}
	return m, nil
}

// Get the Constellation service provider
func (m *ConstellationTestManager) GetConstellationServiceProvider() cscommon.IConstellationServiceProvider {
	return m.sp
}

// Get the Constellation Daemon server manager
func (m *ConstellationTestManager) GetServerManager() *csserver.ServerManager {
	return m.serverMgr
}

// Get the Constellation Daemon client
func (m *ConstellationTestManager) GetApiClient() *csclient.ApiClient {
	return m.apiClient
}

// Closes the test manager, shutting down the nodeset mock server and all other resources
func (m *ConstellationTestManager) Close() error {
	if m.serverMgr != nil {
		m.serverMgr.Stop()
		m.swWg.Wait()
		m.TestManager.GetLogger().Info("Stopped daemon API server")
		m.serverMgr = nil
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
