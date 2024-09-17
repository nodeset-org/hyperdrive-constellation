package csservice

import (
	"net/url"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/gorilla/mux"
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/node-manager-core/wallet"
)

// ===============
// === Factory ===
// ===============

type serviceGetResourcesContextFactory struct {
	handler *ServiceHandler
}

func (f *serviceGetResourcesContextFactory) Create(args url.Values) (*serviceGetResourcesContext, error) {
	c := &serviceGetResourcesContext{
		handler: f.handler,
	}
	return c, nil
}

func (f *serviceGetResourcesContextFactory) RegisterRoute(router *mux.Router) {
	server.RegisterQuerylessGet[*serviceGetResourcesContext, csapi.ServiceGetResourcesData](
		router, "get-resources", f, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type serviceGetResourcesContext struct {
	handler *ServiceHandler
}

func (c *serviceGetResourcesContext) PrepareData(data *csapi.ServiceGetResourcesData, walletStatus wallet.WalletStatus, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	sp := c.handler.serviceProvider
	data.Resources = sp.GetResources()
	return types.ResponseStatus_Success, nil
}
