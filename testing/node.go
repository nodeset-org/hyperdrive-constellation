package cstesting

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	csclient "github.com/nodeset-org/hyperdrive-constellation/client"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	csserver "github.com/nodeset-org/hyperdrive-constellation/server"
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	hdclient "github.com/nodeset-org/hyperdrive-daemon/client"
	hdcommon "github.com/nodeset-org/hyperdrive-daemon/common"
	hdservices "github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
	hdserver "github.com/nodeset-org/hyperdrive-daemon/server"
	hdconfig "github.com/nodeset-org/hyperdrive-daemon/shared/config"
)

// A complete Hyperdrive node instance
type HyperdriveNode struct {
	// The daemon's service provider
	ServiceProvider hdcommon.IHyperdriveServiceProvider

	// The daemon's HTTP API server
	ApiServer *hdserver.ServerManager

	// An HTTP API client for the daemon
	ApiClient *hdclient.ApiClient

	// Wait group for graceful shutdown
	wg *sync.WaitGroup
}

// A complete Constellation node instance
type ConstellationNode struct {
	// The daemon's service provider
	ServiceProvider cscommon.IConstellationServiceProvider

	// The daemon's HTTP API server
	ApiServer *csserver.ServerManager

	// An HTTP API client for the daemon
	ApiClient *csclient.ApiClient

	// The Hyperdrive node parent
	HyperdriveNode *HyperdriveNode

	// Wait group for graceful shutdown
	wg *sync.WaitGroup
}

// Create a new complete Constellation node with a new Hyperdrive node parent running with the provided folder as the Hyperdrive user directory,
func NewConstellationNode(folder string, parentSp cscommon.IConstellationServiceProvider, hdPort uint16, csPort uint16, clientLogger *slog.Logger) (*ConstellationNode, error) {
	address := "localhost"

	// Make the directory structure
	moduleDir := filepath.Join(folder, "data", hdconfig.ModulesName, csconfig.ModuleName)
	err := os.MkdirAll(moduleDir, 0755)
	if err != nil {
		return nil, fmt.Errorf("error creating alt node module directory [%s]: %v", moduleDir, err)
	}

	// Make a Hyperdrive node
	hdNode, err := createHyperdriveNode(folder, parentSp, address, hdPort, clientLogger)
	if err != nil {
		return nil, fmt.Errorf("error creating Hyperdrive node: %v", err)
	}

	// Make a Constellation node
	csNode, err := createConstellationNode(folder, parentSp, address, csPort, clientLogger, hdNode)
	if err != nil {
		return nil, fmt.Errorf("error creating Constellation node: %v", err)
	}
	return csNode, nil
}

// Closes the Constellation node and its Hyperdrive parent.
func (n *ConstellationNode) Close() error {
	if n.ApiServer != nil {
		n.ApiServer.Stop()
		n.wg.Wait()
		n.ApiServer = nil
	}
	hd := n.HyperdriveNode
	if hd.ApiServer != nil {
		hd.ApiServer.Stop()
		hd.wg.Wait()
		hd.ApiServer = nil
	}
	return nil
}

// Create a new Hyperdrive node, including its folder structure, service provider, server manager, and API client.
func createHyperdriveNode(folder string, parentSp cscommon.IConstellationServiceProvider, address string, hdPort uint16, clientLogger *slog.Logger) (*HyperdriveNode, error) {
	parentHdCfg := parentSp.GetHyperdriveConfig()

	// Make a new configs
	hdNetSettings := parentHdCfg.GetNetworkSettings()
	hdCfg, err := hdconfig.NewHyperdriveConfigForNetwork(folder, hdNetSettings, parentHdCfg.Network.Value)
	if err != nil {
		return nil, fmt.Errorf("error creating Hyperdrive config: %v", err)
	}
	hdCfg.UserDataPath.Value = filepath.Join(folder, "data")
	hdCfg.ApiPort.Value = hdPort

	// Make a new HD service provider
	hdSp, err := hdcommon.NewHyperdriveServiceProviderFromCustomServices(
		hdCfg,
		parentSp.GetHyperdriveResources(),
		parentSp.GetEthClient(),
		parentSp.GetBeaconClient(),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("error creating HD service provider: %v", err)
	}

	// Create the HD server
	hdWg := &sync.WaitGroup{}
	hdServerMgr, err := hdserver.NewServerManager(hdSp, address, 0, hdWg)
	if err != nil {
		return nil, fmt.Errorf("error creating hyperdrive server: %v", err)
	}

	// Create the client
	urlString := fmt.Sprintf("http://%s:%d/%s", address, hdServerMgr.GetPort(), hdconfig.HyperdriveApiClientRoute)
	url, err := url.Parse(urlString)
	if err != nil {
		return nil, fmt.Errorf("error parsing client URL [%s]: %v", urlString, err)
	}
	hdClient := hdclient.NewApiClient(url, clientLogger, nil)

	return &HyperdriveNode{
		ServiceProvider: hdSp,
		ApiServer:       hdServerMgr,
		ApiClient:       hdClient,
		wg:              hdWg,
	}, nil
}

// Create a new Constellation node, including its folder structure, service provider, server manager, and API client.
func createConstellationNode(folder string, parentSp cscommon.IConstellationServiceProvider, address string, csPort uint16, clientLogger *slog.Logger, hyperdriveNode *HyperdriveNode) (*ConstellationNode, error) {
	// Get the HD artifacts
	hdSp := hyperdriveNode.ServiceProvider
	hdCfg := hdSp.GetConfig()
	hdClient := hyperdriveNode.ApiClient

	// Make Constellation resources
	csResources := getTestResources(hdSp.GetResources())
	csCfg, err := csconfig.NewConstellationConfig(hdCfg, []*csconfig.ConstellationSettings{})
	if err != nil {
		return nil, fmt.Errorf("error creating Constellation config: %v", err)
	}

	// Make a new service provider
	moduleDir := filepath.Join(hdCfg.UserDataPath.Value, hdconfig.ModulesName, csconfig.ModuleName)
	moduleSp, err := hdservices.NewModuleServiceProviderFromArtifacts(hdClient, hdCfg, csCfg, hdSp.GetResources(), moduleDir, csconfig.ModuleName, csconfig.ClientLogName, hdSp.GetEthClient(), hdSp.GetBeaconClient())
	if err != nil {
		return nil, fmt.Errorf("error creating service provider: %v", err)
	}
	constellationSp, err := cscommon.NewConstellationServiceProviderFromCustomServices(moduleSp, csCfg, csResources)
	if err != nil {
		return nil, fmt.Errorf("error creating constellation service provider: %v", err)
	}

	// Create the server
	wg := &sync.WaitGroup{}
	serverMgr, err := csserver.NewServerManager(constellationSp, address, 0, wg)
	if err != nil {
		return nil, fmt.Errorf("error creating constellation server: %v", err)
	}

	// Create the client
	urlString := fmt.Sprintf("http://%s:%d/%s", address, serverMgr.GetPort(), csconfig.ApiClientRoute)
	url, err := url.Parse(urlString)
	if err != nil {
		return nil, fmt.Errorf("error parsing client URL [%s]: %v", urlString, err)
	}
	apiClient := csclient.NewApiClient(url, clientLogger, nil)

	return &ConstellationNode{
		ServiceProvider: constellationSp,
		ApiServer:       serverMgr,
		ApiClient:       apiClient,
		HyperdriveNode:  hyperdriveNode,
		wg:              wg,
	}, nil
}
