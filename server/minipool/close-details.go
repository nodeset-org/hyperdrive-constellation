package csminipool

import (
	"net/url"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/mux"
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
	"github.com/rocket-pool/rocketpool-go/v2/node"
	snminipool "github.com/rocket-pool/smartnode/v2/rocketpool-daemon/api/minipool"
	snapi "github.com/rocket-pool/smartnode/v2/shared/types/api"
)

// ===============
// === Factory ===
// ===============

type minipoolCloseDetailsContextFactory struct {
	handler *MinipoolHandler
}

func (f *minipoolCloseDetailsContextFactory) Create(args url.Values) (*MinipoolCloseDetailsContext, error) {
	c := &MinipoolCloseDetailsContext{
		Handler: f.handler,
	}
	return c, nil
}

func (f *minipoolCloseDetailsContextFactory) RegisterRoute(router *mux.Router) {
	RegisterMinipoolRoute[*MinipoolCloseDetailsContext, csapi.MinipoolCloseDetailsData](
		router, "close/details", f, f.handler.ctx, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type MinipoolCloseDetailsContext struct {
	Handler *MinipoolHandler

	snContext *snminipool.MinipoolCloseDetailsContext
	snData    *snapi.MinipoolCloseDetailsData
}

func (c *MinipoolCloseDetailsContext) Initialize() (types.ResponseStatus, error) {
	// Create the SN context
	c.snContext = &snminipool.MinipoolCloseDetailsContext{
		Handler: c.Handler.snHandler,
	}

	// Create the data used by the SN context
	c.snData = &snapi.MinipoolCloseDetailsData{}

	return c.snContext.Initialize()
}

func (c *MinipoolCloseDetailsContext) GetState(node *node.Node, mc *batch.MultiCaller) {
	// Defer to the SN
	c.snContext.GetState(node, mc)
}

func (c *MinipoolCloseDetailsContext) CheckState(node *node.Node, data *csapi.MinipoolCloseDetailsData) bool {
	// Defer to the SN
	return c.snContext.CheckState(node, c.snData)
}

func (c *MinipoolCloseDetailsContext) GetMinipoolDetails(mc *batch.MultiCaller, mp minipool.IMinipool, index int) {
	// Defer to the SN
	c.snContext.GetMinipoolDetails(mc, mp, index)
}

func (c *MinipoolCloseDetailsContext) PrepareData(addresses []common.Address, mps []minipool.IMinipool, data *csapi.MinipoolCloseDetailsData) (types.ResponseStatus, error) {
	// Defer to the SN for data preparation and response, but copy the data over to the CS type first
	code, err := c.snContext.PrepareData(addresses, mps, c.snData)
	data.Details = c.snData.Details
	return code, err
}
