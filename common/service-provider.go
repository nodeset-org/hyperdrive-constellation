package cscommon

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"

	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
	hdconfig "github.com/nodeset-org/hyperdrive-daemon/shared/config"
	config "github.com/rocket-pool/node-manager-core/config"
	"github.com/rocket-pool/node-manager-core/node/validator/keymanager"
	"github.com/rocket-pool/node-manager-core/wallet"
	snservices "github.com/rocket-pool/smartnode/v2/rocketpool-daemon/common/services"
	snconfig "github.com/rocket-pool/smartnode/v2/shared/config"
)

// ==================
// === Interfaces ===
// ==================

// Provides the Constellation module config and resources
type IConstellationConfigProvider interface {
	// Gets the Constellation config
	GetConfig() *csconfig.ConstellationConfig

	// Gets the Constellation resources
	GetResources() *csconfig.MergedResources
}

// Provides the Constellation manager
type IConstellationManagerProvider interface {
	// Gets the Constellation manager
	GetConstellationManager() *ConstellationManager
}

// Provides the Constellation daemon wallet
type IConstellationWalletProvider interface {
	// Gets the wallet
	GetWallet() *Wallet
}

// Provides the services used for Rocket Pool and Smart Node interaction
type ISmartNodeServiceProvider interface {
	// Gets the Rocket Pool manager
	GetRocketPoolManager() *RocketPoolManager

	// Gets the Smart Node service provider
	GetSmartNodeServiceProvider() snservices.ISmartNodeServiceProvider
}

// Provides the requirements for the Constellation daemon
type IConstellationRequirementsProvider interface {
	// Requires either the node address or the wallet address to be registered with Constellation.
	// If useWalletAddress is true, the wallet address will be used to check registration. If false, the node address will be used.
	// Errors include:
	// - services.ErrNodeAddressNotSet
	// - services.ErrNeedPassword
	// - services.ErrWalletLoadFailure
	// - services.ErrNoWallet
	// - services.ErrWalletMismatch
	// - services.ErrExecutionClientNotSynced
	// - ErrNotRegisteredWithConstellation
	RequireRegisteredWithConstellation(ctx context.Context, walletStatus wallet.WalletStatus, useWalletAddress bool) error
}

// Provides the key manager and other VC manager services
type IVcManagerProvider interface {
	// Gets the key manager client
	GetKeyManagerClient() keymanager.IKeyManagerClient

	// Gets the graffiti manager
	GetGraffitiManager() *GraffitiManager
}

// Provides all services for the Constellation daemon
type IConstellationServiceProvider interface {
	IConstellationConfigProvider
	IConstellationManagerProvider
	IConstellationRequirementsProvider
	IConstellationWalletProvider
	ISmartNodeServiceProvider
	IVcManagerProvider

	services.IModuleServiceProvider
}

// ========================
// === Service Provider ===
// ========================

type ConstellationServiceProviderOptions struct {
	// The key manager client
	KeyManagerClient keymanager.IKeyManagerClient
}

type constellationServiceProvider struct {
	services.IModuleServiceProvider
	csCfg     *csconfig.ConstellationConfig
	resources *csconfig.MergedResources
	csMgr     *ConstellationManager
	rpMgr     *RocketPoolManager
	snSp      *smartNodeServiceProvider
	keyMgr    keymanager.IKeyManagerClient
	grafMgr   *GraffitiManager
	wallet    *Wallet
}

// Create a new service provider with Constellation daemon-specific features
func NewConstellationServiceProvider(sp services.IModuleServiceProvider, settingsList []*csconfig.ConstellationSettings) (IConstellationServiceProvider, error) {
	// Create the resources
	csCfg, ok := sp.GetModuleConfig().(*csconfig.ConstellationConfig)
	if !ok {
		return nil, fmt.Errorf("constellation config is not the correct type, it's a %s", reflect.TypeOf(csCfg))
	}
	hdCfg := sp.GetHyperdriveConfig()
	hdRes := sp.GetHyperdriveResources()

	// Get the resources from the selected network
	var csResources *csconfig.MergedResources
	for _, network := range settingsList {
		if network.Key != hdCfg.Network.Value {
			continue
		}
		csResources = &csconfig.MergedResources{
			MergedResources:        hdRes,
			ConstellationResources: network.ConstellationResources,
			SmartNodeResources:     network.SmartNodeResources,
		}
		break
	}
	if csResources == nil {
		return nil, fmt.Errorf("no constellation resources found for selected network [%s]", hdCfg.Network.Value)
	}

	return NewConstellationServiceProviderFromCustomServices(sp, csCfg, csResources, nil)
}

// Create a new service provider with Constellation daemon-specific features, using custom services instead of loading them from the module service provider.
func NewConstellationServiceProviderFromCustomServices(sp services.IModuleServiceProvider, cfg *csconfig.ConstellationConfig, csresources *csconfig.MergedResources, opts *ConstellationServiceProviderOptions) (IConstellationServiceProvider, error) {
	moduleDir := sp.GetModuleDir()
	keystoreBaseDir := filepath.Join(moduleDir, hdconfig.ValidatorsDirectory)

	// Create the Constellation manager
	csMgr, err := NewConstellationManager(csresources.ConstellationResources, sp.GetEthClient(), sp.GetQueryManager(), sp.GetTransactionManager())
	if err != nil {
		return nil, fmt.Errorf("error creating Constellation manager: %w", err)
	}

	// Create the Rocket Pool manager
	rpMgr, err := NewRocketPoolManager(csresources, sp.GetEthClient(), sp.GetQueryManager(), sp.GetTransactionManager())
	if err != nil {
		return nil, fmt.Errorf("error creating Rocket Pool manager: %w", err)
	}

	// Create the key manager client if not provided
	if opts == nil {
		opts = &ConstellationServiceProviderOptions{}
	}
	if opts.KeyManagerClient == nil || (reflect.ValueOf(opts.KeyManagerClient).Kind() == reflect.Ptr && reflect.ValueOf(opts.KeyManagerClient).IsNil()) {
		hdCfg := sp.GetHyperdriveConfig()
		bn := hdCfg.GetSelectedBeaconNode()
		vcEndpoint := fmt.Sprintf("http://%s:%d", csconfig.ContainerID_ConstellationValidator, cfg.VcCommon.KeyManagerPort.Value)

		// Get the validator keystore base directory and JWT file path
		kmJwtPath := filepath.Join(keystoreBaseDir, csconfig.KeyManagerJwtFile)

		// Create the key manager
		switch bn {
		case config.BeaconNode_Lighthouse:
			opts.KeyManagerClient, err = keymanager.NewLighthouseKeyManagerClient(vcEndpoint, keystoreBaseDir, nil)
		default:
			opts.KeyManagerClient, err = keymanager.NewStandardKeyManagerClient(vcEndpoint, kmJwtPath, nil)
		}
	}

	// Create the wallet
	wallet, err := NewWallet(sp, keystoreBaseDir, opts.KeyManagerClient)
	if err != nil {
		return nil, fmt.Errorf("error creating wallet: %w", err)
	}

	// Make the provider
	constellationSp := &constellationServiceProvider{
		IModuleServiceProvider: sp,
		csCfg:                  cfg,
		resources:              csresources,
		csMgr:                  csMgr,
		rpMgr:                  rpMgr,
		keyMgr:                 opts.KeyManagerClient,
		wallet:                 wallet,
	}

	// Create the Smart Node service provider
	snRes := &snconfig.MergedResources{
		NetworkResources:   csresources.NetworkResources,
		SmartNodeResources: csresources.SmartNodeResources,
	}
	snSp, err := newSmartNodeServiceProvider(constellationSp, sp.GetHyperdriveConfig(), cfg, snRes)
	if err != nil {
		return nil, fmt.Errorf("error creating Smart Node service provider: %w", err)
	}
	constellationSp.snSp = snSp

	// Create the graffiti manager
	graffitiMgr := NewGraffitiManager(constellationSp)
	constellationSp.grafMgr = graffitiMgr

	return constellationSp, nil
}

func (s *constellationServiceProvider) GetConfig() *csconfig.ConstellationConfig {
	return s.csCfg
}

func (s *constellationServiceProvider) GetResources() *csconfig.MergedResources {
	return s.resources
}

func (s *constellationServiceProvider) GetConstellationManager() *ConstellationManager {
	return s.csMgr
}

func (s *constellationServiceProvider) GetRocketPoolManager() *RocketPoolManager {
	return s.rpMgr
}

func (s *constellationServiceProvider) GetSmartNodeServiceProvider() snservices.ISmartNodeServiceProvider {
	return s.snSp
}

func (s *constellationServiceProvider) GetKeyManagerClient() keymanager.IKeyManagerClient {
	return s.keyMgr
}

func (s *constellationServiceProvider) GetGraffitiManager() *GraffitiManager {
	return s.grafMgr
}

func (s *constellationServiceProvider) GetWallet() *Wallet {
	return s.wallet
}
