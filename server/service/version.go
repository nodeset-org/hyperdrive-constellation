package csservice

import (
	"net/url"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/gorilla/mux"
	"github.com/nodeset-org/hyperdrive-constellation/shared"
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/node-manager-core/wallet"
)

// ===============
// === Factory ===
// ===============

type serviceVersionContextFactory struct {
	handler *ServiceHandler
}

func (f *serviceVersionContextFactory) Create(args url.Values) (*serviceVersionContext, error) {
	c := &serviceVersionContext{
		handler: f.handler,
	}
	return c, nil
}

func (f *serviceVersionContextFactory) RegisterRoute(router *mux.Router) {
	server.RegisterQuerylessGet[*serviceVersionContext, csapi.ServiceVersionData](
		router, "version", f, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type serviceVersionContext struct {
	handler *ServiceHandler
}

func (c *serviceVersionContext) PrepareData(data *csapi.ServiceVersionData, walletStatus wallet.WalletStatus, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	data.Version = shared.ConstellationVersion
	return types.ResponseStatus_Success, nil
}
