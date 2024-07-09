package testing

import (
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	hdconfig "github.com/nodeset-org/hyperdrive-daemon/shared/config"
	"github.com/rocket-pool/node-manager-core/config"
)

const (
	// Address of the Directory contract testing
	DirectoryString     string = "0x71C95911E9a5D330f4D621842EC243EE1343292e"
	SmoothingPoolString string = "0x0E801D84Fa97b50751Dbf25036d067dCf18858bF"
)

// GetTestResources returns a new ConstellationResources instance with test network values
func GetTestResources(hdResources *hdconfig.HyperdriveResources) *csconfig.ConstellationResources {
	return &csconfig.ConstellationResources{
		HyperdriveResources: hdResources,
		Directory:           config.HexToAddressPtr(DirectoryString),
		FeeRecipient:        config.HexToAddressPtr(SmoothingPoolString),
	}
}
