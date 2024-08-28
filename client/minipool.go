package csclient

import (
	"math/big"
	"strconv"

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

// Deposit to Constellation to create a new minipool
func (r *MinipoolRequester) Create(salt *big.Int) (*types.ApiResponse[csapi.MinipoolCreateData], error) {
	args := map[string]string{
		"salt": salt.String(),
	}
	return client.SendGetRequest[csapi.MinipoolCreateData](r, "create", "Create", args)
}

// Get details of minipools that are eligible for exiting, optionally listing all minipools instead (even ones that are not eligible)
func (r *MinipoolRequester) GetExitDetails(verbose bool) (*types.ApiResponse[csapi.MinipoolExitDetailsData], error) {
	args := map[string]string{
		"verbose": strconv.FormatBool(verbose),
	}
	return client.SendGetRequest[csapi.MinipoolExitDetailsData](r, "exit/details", "GetExitDetails", args)
}

// Submit voluntary exits for minipool validators to the Beacon Chain
func (r *MinipoolRequester) Exit(infos []csapi.MinipoolExitInfo) (*types.ApiResponse[types.SuccessData], error) {
	body := csapi.MinipoolExitBody{
		Infos: infos,
	}
	return client.SendPostRequest[types.SuccessData](r, "exit", "Exit", body)
}

// Get details and transaction info of minipools that are eligible for staking
func (r *MinipoolRequester) Stake() (*types.ApiResponse[csapi.MinipoolStakeData], error) {
	args := map[string]string{}
	return client.SendGetRequest[csapi.MinipoolStakeData](r, "stake", "Stake", args)
}

// Get all status details for minipools
func (r *MinipoolRequester) Status() (*types.ApiResponse[csapi.MinipoolStatusData], error) {
	args := map[string]string{}
	return client.SendGetRequest[csapi.MinipoolStatusData](r, "status", "Status", args)
}

// Upload signed voluntary exit messages for minipool validators to the NodeSet server
func (r *MinipoolRequester) UploadSignedExits(infos []csapi.MinipoolExitInfo) (*types.ApiResponse[types.SuccessData], error) {
	body := csapi.MinipoolUploadSignedExitBody{
		Infos: infos,
	}
	return client.SendPostRequest[types.SuccessData](r, "upload-signed-exits", "UploadSignedExits", body)
}

// Submit a minipool request that takes in a list of addresses and returns whatever type is requested
func sendMultiMinipoolRequest[DataType any](r *MinipoolRequester, method string, requestName string, addresses []common.Address, args map[string]string) (*types.ApiResponse[DataType], error) {
	if args == nil {
		args = map[string]string{}
	}
	args["addresses"] = client.MakeBatchArg(addresses)
	return client.SendGetRequest[DataType](r, method, requestName, args)
}
