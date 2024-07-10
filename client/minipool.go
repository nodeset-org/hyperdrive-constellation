package csclient

import (
	"log/slog"
	"net/http/httptrace"
	"net/url"

	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	"github.com/rocket-pool/node-manager-core/api/client"
	"github.com/rocket-pool/node-manager-core/api/types"
)

type MinipoolRequester struct {
	context client.IRequesterContext
}

func NewMinipoolRequester(apiUrl *url.URL, logger *slog.Logger, tracer *httptrace.ClientTrace) *MinipoolRequester {
	context := client.NewNetworkRequesterContext(apiUrl, logger, tracer)

	client := &MinipoolRequester{
		context: context,
	}
	return client
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

func (r *MinipoolRequester) GetAvailabilityCount() (*types.ApiResponse[csapi.NodeGetAvailabilityCount], error) {
	args := map[string]string{}
	return client.SendGetRequest[csapi.NodeGetAvailabilityCount](r, "get-availability-count", "GetAvailabilityCount", args)
}
