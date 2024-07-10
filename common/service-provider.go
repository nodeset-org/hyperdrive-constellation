package cscommon

import (
	"fmt"
	"reflect"

	"github.com/nodeset-org/hyperdrive-constellation/common/contracts/constellation"
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
	"github.com/rocket-pool/rocketpool-go/v2/rocketpool"
)

type ConstellationServiceProvider struct {
	*services.ServiceProvider
	csCfg     *csconfig.ConstellationConfig
	resources *csconfig.ConstellationResources
	csMgr     *constellation.ConstellationManager
	rp        *rocketpool.RocketPool
}

// Create a new service provider with Constellation daemon-specific features
func NewConstellationServiceProvider(sp *services.ServiceProvider) (*ConstellationServiceProvider, error) {
	// Create the resources
	csCfg, ok := sp.GetModuleConfig().(*csconfig.ConstellationConfig)
	if !ok {
		return nil, fmt.Errorf("constellation config is not the correct type, it's a %s", reflect.TypeOf(csCfg))
	}
	res := csCfg.GetConstellationResources()

	return NewConstellationServiceProviderFromCustomServices(sp, csCfg, res)
}

// Create a new service provider with Constellation daemon-specific features, using custom services instead of loading them from the module service provider.
func NewConstellationServiceProviderFromCustomServices(sp *services.ServiceProvider, cfg *csconfig.ConstellationConfig, resources *csconfig.ConstellationResources) (*ConstellationServiceProvider, error) {
	// Create the Rocket Pool binding
	rp, err := rocketpool.NewRocketPool(sp.GetEthClient(), *resources.RocketStorage, resources.MulticallAddress, resources.BalanceBatcherAddress)
	if err != nil {
		return nil, fmt.Errorf("error creating Rocket Pool binding: %w", err)
	}

	// Create the Constellation manager
	csMgr, err := constellation.NewConstellationManager(resources, sp.GetEthClient(), sp.GetQueryManager(), sp.GetTransactionManager())
	if err != nil {
		return nil, fmt.Errorf("error creating constellation manager: %w", err)
	}

	// Make the provider
	constellationSp := &ConstellationServiceProvider{
		ServiceProvider: sp,
		csCfg:           cfg,
		resources:       resources,
		csMgr:           csMgr,
		rp:              rp,
	}
	return constellationSp, nil
}

func (s *ConstellationServiceProvider) GetModuleConfig() *csconfig.ConstellationConfig {
	return s.csCfg
}

func (s *ConstellationServiceProvider) GetResources() *csconfig.ConstellationResources {
	return s.resources
}

func (s *ConstellationServiceProvider) GetConstellationManager() *constellation.ConstellationManager {
	return s.csMgr
}

func (s *ConstellationServiceProvider) GetRocketPool() *rocketpool.RocketPool {
	return s.rp
}
