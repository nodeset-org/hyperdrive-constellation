package csminipool

import (
	"errors"
	"net/url"

	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"

	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/node-manager-core/wallet"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/gorilla/mux"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
)

// ===============
// === Factory ===
// ===============

type nodeGetAvailabilityCountContextFactory struct {
	handler *MinipoolHandler
}

func (f *nodeGetAvailabilityCountContextFactory) Create(args url.Values) (*nodeGetAvailabilityCountContext, error) {
	c := &nodeGetAvailabilityCountContext{
		handler: f.handler,
	}
	inputErrs := []error{}
	return c, errors.Join(inputErrs...)
}

func (f *nodeGetAvailabilityCountContextFactory) RegisterRoute(router *mux.Router) {
	server.RegisterQuerylessGet[*nodeGetAvailabilityCountContext, csapi.NodeGetAvailabilityCount](
		router, "get-availability-count", f, f.handler.logger.Logger, f.handler.serviceProvider.ServiceProvider,
	)
}

// ===============
// === Context ===
// ===============

type nodeGetAvailabilityCountContext struct {
	handler *MinipoolHandler
}

func (c *nodeGetAvailabilityCountContext) PrepareData(data *csapi.NodeGetAvailabilityCount, walletStatus wallet.WalletStatus, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	// Call hyperdrive daemon to make the call to NodeSet
	sp := c.handler.serviceProvider
	hd := sp.GetHyperdriveClient()

	// Requirements
	err := sp.RequireNodeAddress(walletStatus)
	if err != nil {
		return types.ResponseStatus_WalletNotReady, err
	}

	response, err := hd.NodeSet_Constellation.GetAvailableMinipoolCount()
	if err != nil {
		return types.ResponseStatus_Error, err
	}

	data.Count = response.Data.Count

	return types.ResponseStatus_Success, nil
}
