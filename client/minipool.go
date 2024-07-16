package csclient

import (
	"github.com/ethereum/go-ethereum/common"
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

// Close
func (r *MinipoolRequester) Close(addresses []common.Address) (*types.ApiResponse[types.BatchTxInfoData], error) {
	return sendMultiMinipoolRequest[types.BatchTxInfoData](r, "close", "Close", addresses, nil)
}

// Get close details
func (r *MinipoolRequester) GetCloseDetails() (*types.ApiResponse[csapi.MinipoolCloseDetailsData], error) {
	return client.SendGetRequest[csapi.MinipoolCloseDetailsData](r, "close/details", "GetCloseDetails", nil)
}

// Get the number of minipools that can currently be created by the node
func (r *MinipoolRequester) GetAvailableMinipoolCount() (*types.ApiResponse[csapi.MinipoolGetAvailableMinipoolCount], error) {
	args := map[string]string{}
	return client.SendGetRequest[csapi.MinipoolGetAvailableMinipoolCount](r, "get-available-minipool-count", "GetAvailableMinipoolCount", args)
}

// Deposit minipool
func (r *MinipoolRequester) Deposit() (*types.ApiResponse[csapi.MinipoolDepositMinipool], error) {
	args := map[string]string{}
	return client.SendGetRequest[csapi.MinipoolDepositMinipool](r, "deposit-minipool", "DepositMinipool", args)
}

// Submit a minipool request that takes in a list of addresses and returns whatever type is requested
func sendMultiMinipoolRequest[DataType any](r *MinipoolRequester, method string, requestName string, addresses []common.Address, args map[string]string) (*types.ApiResponse[DataType], error) {
	if args == nil {
		args = map[string]string{}
	}
	args["addresses"] = client.MakeBatchArg(addresses)
	return client.SendGetRequest[DataType](r, method, requestName, args)
}
