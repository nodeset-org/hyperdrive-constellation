package csnode

import (
	"errors"
	"fmt"
	"net/url"

	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"

	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/node-manager-core/wallet"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/gorilla/mux"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
)

// ===============
// === Factory ===
// ===============

type nodeGetRegistrationStatusContextFactory struct {
	handler *NodeHandler
}

func (f *nodeGetRegistrationStatusContextFactory) Create(args url.Values) (*nodeGetRegistrationStatusContext, error) {
	c := &nodeGetRegistrationStatusContext{
		handler: f.handler,
	}
	inputErrs := []error{}
	return c, errors.Join(inputErrs...)
}

func (f *nodeGetRegistrationStatusContextFactory) RegisterRoute(router *mux.Router) {
	server.RegisterQuerylessGet[*nodeGetRegistrationStatusContext, csapi.NodeGetRegistrationStatusData](
		router, "get-registration-status", f, f.handler.logger.Logger, f.handler.serviceProvider.ServiceProvider,
	)
}

// ===============
// === Context ===
// ===============

type nodeGetRegistrationStatusContext struct {
	handler *NodeHandler
}

func (c *nodeGetRegistrationStatusContext) PrepareData(data *csapi.NodeGetRegistrationStatusData, walletStatus wallet.WalletStatus, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	sp := c.handler.serviceProvider
	csMgr := sp.GetConstellationManager()
	ctx := c.handler.ctx

	// Requirements
	err := sp.RequireNodeAddress(walletStatus)
	if err != nil {
		return types.ResponseStatus_WalletNotReady, err
	}
	err = sp.RequireEthClientSynced(ctx)
	if err != nil {
		if errors.Is(err, services.ErrBeaconNodeNotSynced) {
			return types.ResponseStatus_ClientsNotSynced, err
		}
		return types.ResponseStatus_Error, err
	}

	// Refresh the Constellation contracts
	err = csMgr.RefreshContracts()
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error refreshing contracts: %w", err)
	}

	// Get the registration status
	err = sp.GetQueryManager().Query(func(mc *batch.MultiCaller) error {
		csMgr.Whitelist.IsAddressInWhitelist(mc, &data.Registered, walletStatus.Address.NodeAddress)
		return nil
	}, nil)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error checking if node is registered: %w", err)
	}
	return types.ResponseStatus_Success, nil
}
