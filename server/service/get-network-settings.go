package csservice

import (
	"fmt"
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

type serviceGetNetworkSettingsContextFactory struct {
	handler *ServiceHandler
}

func (f *serviceGetNetworkSettingsContextFactory) Create(args url.Values) (*serviceGetNetworkSettingsContext, error) {
	c := &serviceGetNetworkSettingsContext{
		handler: f.handler,
	}
	return c, nil
}

func (f *serviceGetNetworkSettingsContextFactory) RegisterRoute(router *mux.Router) {
	server.RegisterQuerylessGet[*serviceGetNetworkSettingsContext, csapi.ServiceGetNetworkSettingsData](
		router, "get-network-settings", f, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type serviceGetNetworkSettingsContext struct {
	handler *ServiceHandler
}

func (c *serviceGetNetworkSettingsContext) PrepareData(data *csapi.ServiceGetNetworkSettingsData, walletStatus wallet.WalletStatus, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	sp := c.handler.serviceProvider
	hdCfg := sp.GetHyperdriveConfig()
	csCfg := sp.GetConfig()
	settingsList := csCfg.GetNetworkSettings()
	network := hdCfg.Network.Value
	for _, settings := range settingsList {
		if settings.Key == network {
			data.Settings = settings
			return types.ResponseStatus_Success, nil
		}
	}
	return types.ResponseStatus_Error, fmt.Errorf("hyperdrive has network [%s] selected but constellation has no settings for it", network)
}
