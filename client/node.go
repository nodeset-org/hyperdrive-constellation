package csclient

import (
	"math/big"

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

// Gets a TX for claiming rewards
func (r *NodeRequester) ClaimRewards(startInterval *big.Int, endInterval *big.Int) (*types.ApiResponse[types.TxInfoData], error) {
	args := map[string]string{
		"startInterval": startInterval.String(),
		"endInterval":   endInterval.String(),
	}
	return client.SendGetRequest[types.TxInfoData](r, "claim-rewards", "ClaimRewards", args)
}

// Get the registration status of the node with Constellation
func (r *NodeRequester) GetRegistrationStatus() (*types.ApiResponse[csapi.NodeGetRegistrationStatusData], error) {
	args := map[string]string{}
	return client.SendGetRequest[csapi.NodeGetRegistrationStatusData](r, "get-registration-status", "GetRegistrationStatus", args)
}

// Gets a TX for registering the node with Constellation
func (r *NodeRequester) Register() (*types.ApiResponse[csapi.NodeRegisterData], error) {
	args := map[string]string{}
	return client.SendGetRequest[csapi.NodeRegisterData](r, "register", "Register", args)
}
