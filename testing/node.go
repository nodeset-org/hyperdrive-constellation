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
	hdservices "github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
	hdconfig "github.com/nodeset-org/hyperdrive-daemon/shared/config"
	hdtesting "github.com/nodeset-org/hyperdrive-daemon/testing"
)

// A complete Constellation node instance
type ConstellationNode struct {
	// The daemon's service provider
	sp cscommon.IConstellationServiceProvider

	// The daemon's HTTP API server
	serverMgr *csserver.ServerManager

	// An HTTP API client for the daemon
	client *csclient.ApiClient

	// The client logger
	logger *slog.Logger

	// The Hyperdrive node parent
	hdNode *hdtesting.HyperdriveNode

	// Wait group for graceful shutdown
	wg *sync.WaitGroup
}

// Create a new Constellation node, including its folder structure, service provider, server manager, and API client.
func newConstellationNode(sp cscommon.IConstellationServiceProvider, address string, clientLogger *slog.Logger, hyperdriveNode *hdtesting.HyperdriveNode) (*ConstellationNode, error) {
	// Create the server
	wg := &sync.WaitGroup{}
	csCfg := sp.GetConfig()
	serverMgr, err := csserver.NewServerManager(sp, address, csCfg.ApiPort.Value, wg)
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
		sp:        sp,
		serverMgr: serverMgr,
		client:    apiClient,
		logger:    clientLogger,
		hdNode:    hyperdriveNode,
		wg:        wg,
	}, nil
}

// Closes the Constellation node. The caller is responsible for stopping the Hyperdrive daemon owning this module.
func (n *ConstellationNode) Close() error {
	if n.serverMgr != nil {
		n.serverMgr.Stop()
		n.wg.Wait()
		n.serverMgr = nil
		n.logger.Info("Stopped Constellation daemon API server")
	}
	return n.hdNode.Close()
}

// Get the daemon's service provider
func (n *ConstellationNode) GetServiceProvider() cscommon.IConstellationServiceProvider {
	return n.sp
}

// Get the HTTP API server for the node's daemon
func (n *ConstellationNode) GetServerManager() *csserver.ServerManager {
	return n.serverMgr
}

// Get the HTTP API client for interacting with the node's daemon server
func (n *ConstellationNode) GetApiClient() *csclient.ApiClient {
	return n.client
}

// Get the Hyperdrive node for this Constellation module
func (n *ConstellationNode) GetHyperdriveNode() *hdtesting.HyperdriveNode {
	return n.hdNode
}

// Create a new Constellation node based on this one's configuration, but with a custom folder, address, and port.
func (n *ConstellationNode) CreateSubNode(hdNode *hdtesting.HyperdriveNode, address string, port uint16) (*ConstellationNode, error) {
	// Get the HD artifacts
	hdSp := hdNode.GetServiceProvider()
	hdCfg := hdSp.GetConfig()
	hdClient := hdNode.GetApiClient()

	// Make Constellation resources
	csResources := getTestResources(hdSp.GetResources())
	csCfg, err := csconfig.NewConstellationConfig(hdCfg, []*csconfig.ConstellationSettings{})
	if err != nil {
		return nil, fmt.Errorf("error creating Constellation config: %v", err)
	}
	csCfg.ApiPort.Value = port

	// Make sure the module directory exists
	moduleDir := filepath.Join(hdCfg.UserDataPath.Value, hdconfig.ModulesName, csconfig.ModuleName)
	err = os.MkdirAll(moduleDir, 0755)
	if err != nil {
		return nil, fmt.Errorf("error creating data and modules directories [%s]: %v", moduleDir, err)
	}

	// Make a new service provider
	moduleSp, err := hdservices.NewModuleServiceProviderFromArtifacts(
		hdClient,
		hdCfg,
		csCfg,
		hdSp.GetResources(),
		moduleDir,
		csconfig.ModuleName,
		csconfig.ClientLogName,
		hdSp.GetEthClient(),
		hdSp.GetBeaconClient(),
	)
	if err != nil {
		return nil, fmt.Errorf("error creating service provider: %v", err)
	}
	csSp, err := cscommon.NewConstellationServiceProviderFromCustomServices(moduleSp, csCfg, csResources)
	if err != nil {
		return nil, fmt.Errorf("error creating constellation service provider: %v", err)
	}
	return newConstellationNode(csSp, address, n.logger, hdNode)
}
