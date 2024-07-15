package cscommon

import (
	"fmt"
	"sync"

	"github.com/hashicorp/go-version"
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/rocketpool-go/v2/rocketpool"
)

// Manager for the Rocket Pool binding
type RocketPoolManager struct {
	RocketPool *rocketpool.RocketPool

	// Internal fields
	loadedContractVersion *version.Version
	refreshLock           *sync.Mutex
}

// Creates a new RocketPoolManager instance
func NewRocketPoolManager(res *csconfig.ConstellationResources, ec eth.IExecutionClient, qMgr *eth.QueryManager, txMgr *eth.TransactionManager) (*RocketPoolManager, error) {
	// Create the Rocket Pool binding
	rp, err := rocketpool.NewRocketPool(ec, *res.RocketStorage, res.MulticallAddress, res.BalanceBatcherAddress)
	if err != nil {
		return nil, fmt.Errorf("error creating Rocket Pool binding: %w", err)
	}

	// Create the manager
	defaultVersion, _ := version.NewSemver("0.0.0")
	return &RocketPoolManager{
		RocketPool:            rp,
		loadedContractVersion: defaultVersion,
		refreshLock:           &sync.Mutex{},
	}, nil
}

// Refresh the Rocket Pool contracts if they've been updated since they were last loaded.
// Requires a synced EC to function properly; you're responsible for ensuring it's synced before calling this.
func (m *RocketPoolManager) RefreshRocketPoolContracts() error {
	m.refreshLock.Lock()
	defer m.refreshLock.Unlock()

	// Get the version on-chain
	protocolVersion, err := m.RocketPool.GetProtocolVersion(nil)
	if err != nil {
		return err
	}

	// Reload everything if it's different from what we have
	if !m.loadedContractVersion.Equal(protocolVersion) {
		err := m.RocketPool.LoadAllContracts(nil)
		if err != nil {
			return fmt.Errorf("error updating rocket pool contracts to [%s]: %w", protocolVersion.String(), err)
		}
		m.loadedContractVersion = protocolVersion
	}
	return nil
}
