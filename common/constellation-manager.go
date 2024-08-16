package cscommon

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/nodeset-org/hyperdrive-constellation/common/contracts/constellation"
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/eth"
)

// Manager for Constellation contract bindings
type ConstellationManager struct {
	Directory           *constellation.Directory
	Whitelist           *constellation.Whitelist
	SuperNodeAccount    *constellation.SuperNodeAccount
	PriceFetcher        *constellation.PriceFetcher
	OperatorDistributor *constellation.OperatorDistributor
	WethVault           *constellation.WethVault
	RplVault            *constellation.RplVault
	PoABeaconOracle     *constellation.PoABeaconOracle
	Treasury            *constellation.Treasury

	// Internal fields
	ec       eth.IExecutionClient
	qMgr     *eth.QueryManager
	txMgr    *eth.TransactionManager
	isLoaded bool
}

// Creates a new ConstellationManager instance
func NewConstellationManager(res *csconfig.ConstellationResources, ec eth.IExecutionClient, qMgr *eth.QueryManager, txMgr *eth.TransactionManager) (*ConstellationManager, error) {
	directory, err := constellation.NewDirectory(*res.Directory, ec, txMgr)
	if err != nil {
		return nil, fmt.Errorf("error creating directory binding: %w", err)
	}

	return &ConstellationManager{
		Directory: directory,
		ec:        ec,
		qMgr:      qMgr,
		txMgr:     txMgr,
	}, nil
}

// Checks if the contract addresses have been loaded yet, and if not, generates the bindings with the on-chain addresses.
// Requires a synced EC to function properly; you're responsible for ensuring it's synced before calling this.
func (m *ConstellationManager) LoadContracts() error {
	if m.isLoaded {
		return nil
	}

	// Get the addresses
	var whitelistAddress common.Address
	var superNodeAccountAddress common.Address
	var priceFetcherAddress common.Address
	var operatorDistributorAddress common.Address
	var wethVaultAddress common.Address
	var rplVaultAddress common.Address
	var poaBeaconOracleAddress common.Address
	var treasuryAddress common.Address
	var nodeSetOperatorRewardsDistributorAddress common.Address
	err := m.qMgr.Query(func(mc *batch.MultiCaller) error {
		m.Directory.GetWhitelistAddress(mc, &whitelistAddress)
		m.Directory.GetSuperNodeAddress(mc, &superNodeAccountAddress)
		m.Directory.GetPriceFetcherAddress(mc, &priceFetcherAddress)
		m.Directory.GetOperatorDistributorAddress(mc, &operatorDistributorAddress)
		m.Directory.GetWethVaultAddress(mc, &wethVaultAddress)
		m.Directory.GetRplVaultAddress(mc, &rplVaultAddress)
		m.Directory.GetPoABeaconOracleAddress(mc, &poaBeaconOracleAddress)
		m.Directory.GetTreasuryAddress(mc, &treasuryAddress)
		m.Directory.GetNodeSetOperatorRewardsDistributorAddress(mc, &nodeSetOperatorRewardsDistributorAddress)
		return nil
	}, nil)
	if err != nil {
		return fmt.Errorf("error getting contract addresses: %w", err)
	}

	// Generate the bindings
	whitelist, err := constellation.NewWhitelist(whitelistAddress, m.ec, m.txMgr)
	if err != nil {
		return fmt.Errorf("error creating whitelist binding: %w", err)
	}
	superNodeAccount, err := constellation.NewSuperNodeAccount(superNodeAccountAddress, m.ec, m.txMgr)
	if err != nil {
		return fmt.Errorf("error creating super node account binding: %w", err)
	}
	priceFetcher, err := constellation.NewPriceFetcher(priceFetcherAddress, m.ec, m.txMgr)
	if err != nil {
		return fmt.Errorf("error creating price fetcher binding: %w", err)
	}
	operatorDistributor, err := constellation.NewOperatorDistributor(operatorDistributorAddress, m.ec, m.txMgr)
	if err != nil {
		return fmt.Errorf("error creating operator distributor binding: %w", err)
	}
	wethVault, err := constellation.NewWethVault(wethVaultAddress, m.ec, m.qMgr, m.txMgr, nil)
	if err != nil {
		return fmt.Errorf("error creating WETH vault binding: %w", err)
	}
	rplVault, err := constellation.NewRplVault(rplVaultAddress, m.ec, m.qMgr, m.txMgr, nil)
	if err != nil {
		return fmt.Errorf("error creating RPL vault binding: %w", err)
	}
	poaBeaconOracle, err := constellation.NewPoABeaconOracle(poaBeaconOracleAddress, m.ec, m.txMgr)
	if err != nil {
		return fmt.Errorf("error creating PoA Beacon Oracle binding: %w", err)
	}
	treasury, err := constellation.NewTreasury(treasuryAddress, m.ec, m.txMgr)
	if err != nil {
		return fmt.Errorf("error creating treasury binding: %w", err)
	}

	// Update the bindings
	m.Whitelist = whitelist
	m.SuperNodeAccount = superNodeAccount
	m.PriceFetcher = priceFetcher
	m.OperatorDistributor = operatorDistributor
	m.WethVault = wethVault
	m.RplVault = rplVault
	m.PoABeaconOracle = poaBeaconOracle
	m.Treasury = treasury
	m.isLoaded = true
	return nil
}
