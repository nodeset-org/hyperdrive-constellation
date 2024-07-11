package csclient

import (
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	"github.com/rocket-pool/node-manager-core/api/client"
	"github.com/rocket-pool/node-manager-core/api/types"
)

type MinipoolRequester struct {
	context client.IRequesterContext
}

func NewMinipoolRequester(context client.IRequesterContext) *MinipoolRequester {
	return &MinipoolRequester{
		context: context,
	}
}
func (r *MinipoolRequester) GetName() string {
	return "Minipool"
}
func (r *MinipoolRequester) GetRoute() string {
	return "minipool"
}
func (r *MinipoolRequester) GetContext() client.IRequesterContext {
	return r.context
}

func (r *MinipoolRequester) GetAvailableMinipoolCount() (*types.ApiResponse[csapi.MinipoolGetAvailableMinipoolCount], error) {
	args := map[string]string{}
	return client.SendGetRequest[csapi.MinipoolGetAvailableMinipoolCount](r, "get-available-minipool-count", "GetAvailableMinipoolCount", args)
}
