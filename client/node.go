package csclient

import (
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	"github.com/rocket-pool/node-manager-core/api/client"
	"github.com/rocket-pool/node-manager-core/api/types"
)

type NodeRequester struct {
	context client.IRequesterContext
}

func NewNodeRequester(context client.IRequesterContext) *NodeRequester {
	return &NodeRequester{
		context: context,
	}
}

func (r *NodeRequester) GetName() string {
	return "Node"
}
func (r *NodeRequester) GetRoute() string {
	return "node"
}
func (r *NodeRequester) GetContext() client.IRequesterContext {
	return r.context
}

// Get the registration status of the node with the Constellation contracts
func (r *NodeRequester) GetRegistrationStatus() (*types.ApiResponse[csapi.NodeGetRegistrationStatusData], error) {
	args := map[string]string{}
	return client.SendGetRequest[csapi.NodeGetRegistrationStatusData](r, "get-registration-status", "GetRegistrationStatus", args)
}
