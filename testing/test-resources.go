package testing

import (
	"github.com/ethereum/go-ethereum/common"
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	hdconfig "github.com/nodeset-org/hyperdrive-daemon/shared/config"
	"github.com/rocket-pool/node-manager-core/config"
	snconfig "github.com/rocket-pool/smartnode/v2/shared/config"
)

const (
	// Address of the Directory contract
	DirectoryAddress string = "0x71C95911E9a5D330f4D621842EC243EE1343292e"

	// Address of RocketStorage
	RocketStorageAddress string = "0xe7f1725E7734CE288F8367e1Bb143E90bb3F0512"

	// Address of RocketSmoothingPool
	SmoothingPoolAddress string = "0x0E801D84Fa97b50751Dbf25036d067dCf18858bF"

	// Address of the rETH token
	RethAddress string = "0x5FC8d32690cc91D4c39d9d3abcBD16989F875707"

	// Address of the RPL token
	RplAddress string = "0xa513E6E4b8f2a923D98304ec87F64353C4D5C853"

	// Multicall address
	MulticallAddress string = "0x05Aa229Aec102f78CE0E852A812a388F076Aa555"

	// Balance batcher address
	BalanceBatcherAddressString string = "0x0b48aF34f4c854F5ae1A3D587da471FeA45bAD52"
)

// GetTestResources returns a new ConstellationResources instance with test network values
func getTestResources(hdResources *hdconfig.MergedResources) (*csconfig.MergedResources, *snconfig.MergedResources) {
	csRes := &csconfig.MergedResources{
		MergedResources: hdResources,
		ConstellationResources: &csconfig.ConstellationResources{
			Directory:     config.HexToAddressPtr(DirectoryAddress),
			RocketStorage: config.HexToAddressPtr(RocketStorageAddress),
			FeeRecipient:  config.HexToAddressPtr(SmoothingPoolAddress),
		},
	}
	snRes := &snconfig.MergedResources{
		NetworkResources: hdResources.NetworkResources,
		SmartNodeResources: &snconfig.SmartNodeResources{
			StorageAddress:  common.HexToAddress(RocketStorageAddress),
			RethAddress:     common.HexToAddress(RethAddress),
			RplTokenAddress: common.HexToAddress(RplAddress),
		},
	}
	return csRes, snRes
}

// Provisions a NetworkSettings instance with updated addresses
func provisionNetworkSettings(networkSettings *config.NetworkSettings) *config.NetworkSettings {
	networkSettings.NetworkResources.MulticallAddress = common.HexToAddress(MulticallAddress)
	networkSettings.NetworkResources.BalanceBatcherAddress = common.HexToAddress(BalanceBatcherAddressString)
	return networkSettings
}
