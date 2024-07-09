package cscommon

import (
	"fmt"
	"reflect"

	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
)

type ConstellationServiceProvider struct {
	*services.ServiceProvider
	csCfg     *csconfig.ConstellationConfig
	resources *csconfig.ConstellationResources
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
	// Make the provider
	constellationSp := &ConstellationServiceProvider{
		ServiceProvider: sp,
		csCfg:           cfg,
		resources:       resources,
	}
	return constellationSp, nil
}

func (s *ConstellationServiceProvider) GetModuleConfig() *csconfig.ConstellationConfig {
	return s.csCfg
}

func (s *ConstellationServiceProvider) GetResources() *csconfig.ConstellationResources {
	return s.resources
}
