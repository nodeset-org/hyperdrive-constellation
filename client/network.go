package csclient

import (
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	"github.com/rocket-pool/node-manager-core/api/client"
	"github.com/rocket-pool/node-manager-core/api/types"
)

type NetworkRequester struct {
	context client.IRequesterContext
}

func NewNetworkRequester(context client.IRequesterContext) *NetworkRequester {
	return &NetworkRequester{
		context: context,
	}
}

func (r *NetworkRequester) GetName() string {
	return "Network"
}
func (r *NetworkRequester) GetRoute() string {
	return "network"
}
func (r *NetworkRequester) GetContext() client.IRequesterContext {
	return r.context
}

// Get information about the Constellation network
func (r *NetworkRequester) Stats() (*types.ApiResponse[csapi.NetworkStatsData], error) {
	args := map[string]string{}
	return client.SendGetRequest[csapi.NetworkStatsData](r, "stats", "Stats", args)
}
