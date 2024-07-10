package csclient

import (
	"log/slog"
	"net/http/httptrace"
	"net/url"

	"github.com/rocket-pool/node-manager-core/api/client"
)

// Binder for the Constellation API server
type ApiClient struct {
	context client.IRequesterContext
	Node    *NodeRequester
}

// Creates a new API client instance
func NewApiClient(apiUrl *url.URL, logger *slog.Logger, tracer *httptrace.ClientTrace) *ApiClient {
	context := client.NewNetworkRequesterContext(apiUrl, logger, tracer)

	client := &ApiClient{
		context: context,
		Node:    NewNodeRequester(context),
	}
	return client
}
