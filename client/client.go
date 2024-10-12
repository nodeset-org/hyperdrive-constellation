package csclient

import (
	"log/slog"
	"net/http/httptrace"
	"net/url"

	"github.com/nodeset-org/hyperdrive-daemon/shared/auth"
	"github.com/rocket-pool/node-manager-core/api/client"
)

// Binder for the Constellation API server
type ApiClient struct {
	context  client.IRequesterContext
	Minipool *MinipoolRequester
	Network  *NetworkRequester
	Node     *NodeRequester
	Service  *ServiceRequester
	Wallet   *WalletRequester
}

// Creates a new API client instance
func NewApiClient(apiUrl *url.URL, logger *slog.Logger, tracer *httptrace.ClientTrace, authMgr *auth.AuthorizationManager) *ApiClient {
	context := client.NewNetworkRequesterContext(apiUrl, logger, tracer, authMgr.AddAuthHeader)

	client := &ApiClient{
		context:  context,
		Minipool: NewMinipoolRequester(context),
		Network:  NewNetworkRequester(context),
		Node:     NewNodeRequester(context),
		Service:  NewServiceRequester(context),
		Wallet:   NewWalletRequester(context),
	}
	return client
}
