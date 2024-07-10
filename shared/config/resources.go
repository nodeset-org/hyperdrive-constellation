package csconfig

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	hdconfig "github.com/nodeset-org/hyperdrive-daemon/shared/config"
	"github.com/rocket-pool/node-manager-core/config"
)

// A collection of network-specific resources and getters for them
type ConstellationResources struct {
	*hdconfig.HyperdriveResources

	// The Constellation directory contract address, which houses all of the other contract addresses
	Directory *common.Address

	// Address of the RocketStorage contract, the master contract for all of Rocket Pool
	RocketStorage *common.Address

	// The fee recipient to use for the Constellation VC. This must ALWAYS be set to the Rocket Pool Smoothing Pool contract address.
	// Technically this should come from Directory (or RocketStorage within Directory) but it needs to be set here for templating to use it.
	FeeRecipient *common.Address
}

// Creates a new resource collection for the given network
func newConstellationResources(network config.Network) *ConstellationResources {
	// Mainnet
	mainnetResources := &ConstellationResources{
		HyperdriveResources: hdconfig.NewHyperdriveResources(config.Network_Mainnet),
		Directory:           nil,
		RocketStorage:       config.HexToAddressPtr("0x1d8f8f00cfa6758d7bE78336684788Fb0ee0Fa46"),
		FeeRecipient:        config.HexToAddressPtr("0xd4E96eF8eee8678dBFf4d535E033Ed1a4F7605b7"),
	}

	// Holesky
	holeskyResources := &ConstellationResources{
		HyperdriveResources: hdconfig.NewHyperdriveResources(config.Network_Holesky),
		Directory:           nil,
		RocketStorage:       config.HexToAddressPtr("0x594Fb75D3dc2DFa0150Ad03F99F97817747dd4E1"),
		FeeRecipient:        config.HexToAddressPtr("0xA347C391bc8f740CAbA37672157c8aAcD08Ac567"),
	}

	// Holesky Dev
	holeskyDevResources := &ConstellationResources{
		HyperdriveResources: hdconfig.NewHyperdriveResources(config.Network_Holesky),
		Directory:           nil,
		RocketStorage:       config.HexToAddressPtr("0x594Fb75D3dc2DFa0150Ad03F99F97817747dd4E1"),
		FeeRecipient:        config.HexToAddressPtr("0xA347C391bc8f740CAbA37672157c8aAcD08Ac567"),
	}

	switch network {
	case config.Network_Mainnet:
		return mainnetResources
	case config.Network_Holesky:
		return holeskyResources
	case hdconfig.Network_HoleskyDev:
		return holeskyDevResources
	}

	panic(fmt.Sprintf("network %s is not supported", network))
}
