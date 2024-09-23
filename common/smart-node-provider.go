package cscommon

import (
	"context"
	"errors"
	"fmt"

	dclient "github.com/docker/docker/client"
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	hdconfig "github.com/nodeset-org/hyperdrive-daemon/shared/config"
	"github.com/rocket-pool/node-manager-core/api/types"
	config "github.com/rocket-pool/node-manager-core/config"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/node-manager-core/log"
	"github.com/rocket-pool/node-manager-core/node/services"
	nwallet "github.com/rocket-pool/node-manager-core/node/wallet"
	"github.com/rocket-pool/node-manager-core/wallet"
	"github.com/rocket-pool/rocketpool-go/v2/rocketpool"
	sncontracts "github.com/rocket-pool/smartnode/v2/rocketpool-daemon/common/contracts"
	snvalidator "github.com/rocket-pool/smartnode/v2/rocketpool-daemon/common/validator"
	snconfig "github.com/rocket-pool/smartnode/v2/shared/config"
)

type smartNodeServiceProvider struct {
	csSp *constellationServiceProvider
	cfg  *snconfig.SmartNodeConfig
	res  *snconfig.MergedResources
}

func newSmartNodeServiceProvider(csSp *constellationServiceProvider, hdCfg *hdconfig.HyperdriveConfig, csCfg *csconfig.ConstellationConfig, snRes *snconfig.MergedResources) (*smartNodeServiceProvider, error) {
	snCfg, err := createSmartNodeConfig(hdCfg, csCfg, snRes)
	if err != nil {
		return nil, fmt.Errorf("error creating Smart Node config: %w", err)
	}
	return &smartNodeServiceProvider{
		csSp: csSp,
		cfg:  snCfg,
		res:  snRes,
	}, nil
}

// ============================
// === Smart Node Functions ===
// ============================

func (p *smartNodeServiceProvider) GetRocketPool() *rocketpool.RocketPool {
	rpMgr := p.csSp.GetRocketPoolManager()
	return rpMgr.RocketPool
}

func (p *smartNodeServiceProvider) RefreshRocketPoolContracts() error {
	rpMgr := p.csSp.GetRocketPoolManager()
	return rpMgr.RefreshRocketPoolContracts()
}

func (p *smartNodeServiceProvider) GetConfig() *snconfig.SmartNodeConfig {
	return p.cfg
}

func (p *smartNodeServiceProvider) GetResources() *snconfig.MergedResources {
	return p.res
}

func (p *smartNodeServiceProvider) GetValidatorManager() *snvalidator.ValidatorManager {
	// Not used
	return nil
}

func (p *smartNodeServiceProvider) GetSnapshotDelegation() *sncontracts.SnapshotDelegation {
	// Not used
	return nil
}

func (p *smartNodeServiceProvider) GetWatchtowerLogger() *log.Logger {
	// Not used
	return nil
}

// ====================
// === Requirements ===
// ====================

func (p *smartNodeServiceProvider) RequireRocketPoolContracts(ctx context.Context) (types.ResponseStatus, error) {
	err := p.RequireEthClientSynced(ctx)
	if err != nil {
		return types.ResponseStatus_ClientsNotSynced, err
	}
	err = p.RefreshRocketPoolContracts()
	if err != nil {
		return types.ResponseStatus_Error, err
	}
	return types.ResponseStatus_Success, nil
}

func (p *smartNodeServiceProvider) RequireEthClientSynced(ctx context.Context) error {
	return p.csSp.RequireEthClientSynced(ctx)
}

func (p *smartNodeServiceProvider) RequireBeaconClientSynced(ctx context.Context) error {
	return p.csSp.RequireEthClientSynced(ctx)
}

func (p *smartNodeServiceProvider) RequireNodeAddress() error {
	status, err := p.getWalletStatus()
	if err != nil {
		return err
	}
	return p.csSp.RequireNodeAddress(status)
}

func (p *smartNodeServiceProvider) RequireWalletReady() error {
	status, err := p.getWalletStatus()
	if err != nil {
		return err
	}
	return p.csSp.RequireWalletReady(status)
}

func (p *smartNodeServiceProvider) RequireNodeRegistered(ctx context.Context) (types.ResponseStatus, error) {
	// The Constellation supernode is always registered
	return types.ResponseStatus_Success, nil
}

func (p *smartNodeServiceProvider) RequireSnapshot() error {
	// Not used
	return fmt.Errorf("Snapshot voting is not available for Constellation nodes.")
}

func (p *smartNodeServiceProvider) RequireOnOracleDao(ctx context.Context) (types.ResponseStatus, error) {
	// The Constellation supernode is never a security council member
	return types.ResponseStatus_InvalidChainState, errors.New("The Constellation supernode is not an Oracle DAO member.")
}

func (p *smartNodeServiceProvider) RequireOnSecurityCouncil(ctx context.Context) (types.ResponseStatus, error) {
	// The Constellation supernode is never a security council member
	return types.ResponseStatus_InvalidChainState, errors.New("The Constellation supernode is not a security council member.")
}

func (p *smartNodeServiceProvider) WaitEthClientSynced(ctx context.Context, verbose bool) error {
	return p.csSp.WaitEthClientSynced(ctx, verbose)
}

func (p *smartNodeServiceProvider) WaitBeaconClientSynced(ctx context.Context, verbose bool) error {
	return p.csSp.WaitBeaconClientSynced(ctx, verbose)
}

func (p *smartNodeServiceProvider) WaitNodeAddress(ctx context.Context, verbose bool) error {
	_, err := p.csSp.WaitForNodeAddress(ctx)
	return err
}

func (p *smartNodeServiceProvider) WaitWalletReady(ctx context.Context, verbose bool) error {
	_, err := p.csSp.WaitForWallet(ctx)
	return err
}

func (p *smartNodeServiceProvider) WaitNodeRegistered(ctx context.Context, verbose bool) error {
	// Constellation is always registered
	return nil
}

// =====================
// === NMC Functions ===
// =====================

func (p *smartNodeServiceProvider) GetApiLogger() *log.Logger {
	return p.csSp.GetApiLogger()
}

func (p *smartNodeServiceProvider) GetTasksLogger() *log.Logger {
	return p.csSp.GetTasksLogger()
}

func (p *smartNodeServiceProvider) GetEthClient() *services.ExecutionClientManager {
	return p.csSp.GetEthClient()
}

func (p *smartNodeServiceProvider) GetQueryManager() *eth.QueryManager {
	return p.csSp.GetQueryManager()
}

func (p *smartNodeServiceProvider) GetTransactionManager() *eth.TransactionManager {
	return p.csSp.GetTransactionManager()
}

func (p *smartNodeServiceProvider) GetBeaconClient() *services.BeaconClientManager {
	return p.csSp.GetBeaconClient()
}

func (p *smartNodeServiceProvider) GetDocker() dclient.APIClient {
	// Not used
	return nil
}

func (p *smartNodeServiceProvider) GetWallet() *nwallet.Wallet {
	// Not used
	return nil
}

func (p *smartNodeServiceProvider) GetBaseContext() context.Context {
	return p.csSp.GetBaseContext()
}

func (p *smartNodeServiceProvider) CancelContextOnShutdown() {
	// Shouldn't be needed, will need some way to signal to the CS daemon to shut down if this ever gets called
	p.csSp.CancelContextOnShutdown()
}

func (p *smartNodeServiceProvider) Close() error {
	// Shouldn't be needed, will need some way to signal to the CS daemon to shut down if this ever gets called
	return p.csSp.Close()
}

// ==========================
// === Internal Functions ===
// ==========================

func (p *smartNodeServiceProvider) getWalletStatus() (wallet.WalletStatus, error) {
	hd := p.csSp.GetHyperdriveClient()
	walletResponse, err := hd.Wallet.Status()
	if err != nil {
		return wallet.WalletStatus{}, fmt.Errorf("error getting wallet status: %w", err)
	}
	return walletResponse.Data.WalletStatus, nil
}

// This is a binding for the Smart Node's config that carries over as much as possible from the Hyperdrive and Constellation configs.
func createSmartNodeConfig(hdCfg *hdconfig.HyperdriveConfig, csCfg *csconfig.ConstellationConfig, res *snconfig.MergedResources) (*snconfig.SmartNodeConfig, error) {
	// Get the network settings
	network := hdCfg.Network.Value
	var settings *config.NetworkSettings
	for _, hdSettings := range hdCfg.GetNetworkSettings() {
		if hdSettings.Key == network {
			settings = hdSettings.NetworkSettings
			break
		}
	}
	if settings == nil {
		return nil, fmt.Errorf("no network settings found for network [%s]", network)
	}

	// Make a new Smart Node config
	snCfg, err := snconfig.NewSmartNodeConfigForNetwork("", false, []*snconfig.SmartNodeSettings{
		{
			NetworkSettings:    settings,
			SmartNodeResources: res.SmartNodeResources,
		},
	}, network)
	if err != nil {
		return nil, fmt.Errorf("error creating Smart Node config: %w", err)
	}

	// Root params
	snCfg.Network.Value = network
	snCfg.ClientMode.Value = hdCfg.ClientMode.Value
	snCfg.AutoTxMaxFee.Value = hdCfg.AutoTxMaxFee.Value
	snCfg.MaxPriorityFee.Value = hdCfg.MaxPriorityFee.Value
	snCfg.AutoTxGasThreshold.Value = hdCfg.AutoTxGasThreshold.Value

	// Common subsections
	config.Clone(hdCfg.Logging, snCfg.Logging, network)
	config.Clone(hdCfg.LocalExecutionClient, snCfg.LocalExecutionClient, network)
	config.Clone(hdCfg.ExternalExecutionClient, snCfg.ExternalExecutionClient, network)
	config.Clone(hdCfg.LocalBeaconClient, snCfg.LocalBeaconClient, network)
	config.Clone(hdCfg.ExternalBeaconClient, snCfg.ExternalBeaconClient, network)
	config.Clone(hdCfg.Fallback, snCfg.Fallback, network)

	// VC config
	config.Clone(csCfg.VcCommon, snCfg.ValidatorClient.VcCommon, network)
	config.Clone(csCfg.Lighthouse, snCfg.ValidatorClient.Lighthouse, network)
	config.Clone(csCfg.Lodestar, snCfg.ValidatorClient.Lodestar, network)
	config.Clone(csCfg.Nimbus, snCfg.ValidatorClient.Nimbus, network)
	config.Clone(csCfg.Prysm, snCfg.ValidatorClient.Prysm, network)
	config.Clone(csCfg.Teku, snCfg.ValidatorClient.Teku, network)

	// Metrics
	config.Clone(hdCfg.Metrics, snCfg.Metrics.MetricsConfig, network)
	return snCfg, nil
}
