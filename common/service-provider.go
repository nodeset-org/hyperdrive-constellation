package cscommon

import (
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
)

type ConstellationServiceProvider struct {
	*services.ServiceProvider

	csCfg         *csconfig.ConstellationConfig
	resources     *csconfig.ConstellationResources
	nodesetClient *NodesetClient

	// rocketPool *rocketpool.RocketPool
}

// Create a new service provider with Constellation daemon-specific features
func NewConstellationServiceProvider(sp *services.ServiceProvider) (*ConstellationServiceProvider, error) {
	// Create the resources
	res := csconfig.NewConstellationResources(sp.GetResources().Network)

	constellationSp := &ConstellationServiceProvider{
		ServiceProvider: sp,
		resources:       res,
	}
	// Create the nodeset client
	nc := NewNodesetClient(constellationSp)
	constellationSp.nodesetClient = nc
	return constellationSp, nil
}

func (s *ConstellationServiceProvider) GetModuleConfig() *csconfig.ConstellationConfig {
	return s.csCfg
}

func (s *ConstellationServiceProvider) GetResources() *csconfig.ConstellationResources {
	return s.resources
}
