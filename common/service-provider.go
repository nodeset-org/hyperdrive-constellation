package cscommon

import (
	"context"
	"fmt"
	"reflect"

	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
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
	GetResources() *csconfig.ConstellationResources
}

// Provides the Constellation manager
type IConstellationManagerProvider interface {
	// Gets the Constellation manager
	GetConstellationManager() *ConstellationManager
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

// Provides all services for the Constellation daemon
type IConstellationServiceProvider interface {
	IConstellationConfigProvider
	IConstellationManagerProvider
	IConstellationRequirementsProvider
	ISmartNodeServiceProvider

	services.IModuleServiceProvider
}

// ========================
// === Service Provider ===
// ========================

type constellationServiceProvider struct {
	services.IModuleServiceProvider
	csCfg     *csconfig.ConstellationConfig
	resources *csconfig.ConstellationResources
	csMgr     *ConstellationManager
	rpMgr     *RocketPoolManager
	snSp      *smartNodeServiceProvider
	wallet    *Wallet
}

// Create a new service provider with Constellation daemon-specific features
func NewConstellationServiceProvider(sp services.IModuleServiceProvider) (IConstellationServiceProvider, error) {
	// Create the resources
	csCfg, ok := sp.GetModuleConfig().(*csconfig.ConstellationConfig)
	if !ok {
		return nil, fmt.Errorf("constellation config is not the correct type, it's a %s", reflect.TypeOf(csCfg))
	}
	hdCfg := sp.GetHyperdriveConfig()
	csRes := csconfig.NewConstellationResources(hdCfg.Network.Value)
	snRes := snconfig.NewRocketPoolResources(hdCfg.Network.Value)

	return NewConstellationServiceProviderFromCustomServices(sp, csCfg, csRes, snRes)
}

// Create a new service provider with Constellation daemon-specific features, using custom services instead of loading them from the module service provider.
func NewConstellationServiceProviderFromCustomServices(sp services.IModuleServiceProvider, cfg *csconfig.ConstellationConfig, csresources *csconfig.ConstellationResources, snresources *snconfig.RocketPoolResources) (IConstellationServiceProvider, error) {
	// Create the Constellation manager
	csMgr, err := NewConstellationManager(csresources, sp.GetEthClient(), sp.GetQueryManager(), sp.GetTransactionManager())
	if err != nil {
		return nil, fmt.Errorf("error creating Constellation manager: %w", err)
	}

	// Create the Rocket Pool manager
	rpMgr, err := NewRocketPoolManager(csresources, sp.GetEthClient(), sp.GetQueryManager(), sp.GetTransactionManager())
	if err != nil {
		return nil, fmt.Errorf("error creating Rocket Pool manager: %w", err)
	}

	// Create the wallet
	wallet, err := NewWallet(sp)
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
		wallet:                 wallet,
	}

	// Create the Smart Node service provider
	snSp := newSmartNodeServiceProvider(constellationSp, sp.GetHyperdriveConfig(), cfg, snresources)
	constellationSp.snSp = snSp
	return constellationSp, nil
}

func (s *constellationServiceProvider) GetConfig() *csconfig.ConstellationConfig {
	return s.csCfg
}

func (s *constellationServiceProvider) GetResources() *csconfig.ConstellationResources {
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
