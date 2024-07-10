package constellation

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/eth"
)

// Manager for Constellation contract bindings
type ConstellationManager struct {
	Directory *Directory
	Whitelist *Whitelist

	// Internal fields
	ec         eth.IExecutionClient
	qMgr       *eth.QueryManager
	txMgr      *eth.TransactionManager
	lastUpdate string
}

// Creates a new ConstellationManager instance
func NewConstellationManager(res *csconfig.ConstellationResources, ec eth.IExecutionClient, qMgr *eth.QueryManager, txMgr *eth.TransactionManager) (*ConstellationManager, error) {
	directory, err := NewDirectory(*res.Directory, ec, txMgr)
	if err != nil {
		return nil, fmt.Errorf("error creating directory binding: %w", err)
	}

	return &ConstellationManager{
		Directory: directory,
		Whitelist: nil,
		ec:        ec,
		qMgr:      qMgr,
		txMgr:     txMgr,
	}, nil
}

// Checks if the contract addresses are stale, and if so, regenerates the bindings with the new addresses.
// Requires a synced EC to function properly; you're responsible for ensuring it's synced before calling this.
func (m *ConstellationManager) RefreshContracts() error {
	// Check if the contract addresses are stale
	// TODO once the contracts have a function for this
	/*
			err := m.qMgr.Query(func(mc *batch.MultiCaller) error {
		        m.Directory.
		    }, nil)
	*/
	if m.lastUpdate != "" {
		return nil
	}

	// Get the addresses
	var whitelistAddress common.Address
	err := m.qMgr.Query(func(mc *batch.MultiCaller) error {
		m.Directory.GetWhitelistAddress(mc, &whitelistAddress)
		return nil
	}, nil)
	if err != nil {
		return fmt.Errorf("error getting contract addresses: %w", err)
	}

	// Regenerate the bindings
	whitelist, err := NewWhitelist(whitelistAddress, m.ec, m.txMgr)
	if err != nil {
		return fmt.Errorf("error creating whitelist binding: %w", err)
	}

	// Update the bindings
	m.Whitelist = whitelist
	m.lastUpdate = "now"
	return nil
}
