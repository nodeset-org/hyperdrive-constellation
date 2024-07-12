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

func (f *minipoolCloseDetailsContextFactory) Create(args url.Values) (*minipoolCloseDetailsContext, error) {
	c := &minipoolCloseDetailsContext{
		handler: f.handler,
	}
	return c, nil
}

func (f *minipoolCloseDetailsContextFactory) RegisterRoute(router *mux.Router) {
	RegisterMinipoolRoute[*minipoolCloseDetailsContext, csapi.MinipoolCloseDetailsData](
		router, "close/details", f, f.handler.ctx, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type minipoolCloseDetailsContext struct {
	handler *MinipoolHandler

	snHandler *snminipool.MinipoolHandler
	snContext *snminipool.MinipoolCloseDetailsContext
	snData    *snapi.MinipoolCloseDetailsData
}

func (c *minipoolCloseDetailsContext) Initialize() (types.ResponseStatus, error) {
	sp := c.handler.serviceProvider
	c.snHandler = snminipool.NewMinipoolHandler(c.handler.logger, c.handler.ctx, sp.GetSmartNodeServiceProvider())
	c.snContext = &snminipool.MinipoolCloseDetailsContext{
		Handler: c.snHandler,
	}
	c.snData = &snapi.MinipoolCloseDetailsData{}

	return c.snContext.Initialize()
}

func (c *minipoolCloseDetailsContext) GetState(node *node.Node, mc *batch.MultiCaller) {
	c.snContext.GetState(node, mc)
}

func (c *minipoolCloseDetailsContext) CheckState(node *node.Node, data *csapi.MinipoolCloseDetailsData) bool {
	return c.snContext.CheckState(node, c.snData)
}

func (c *minipoolCloseDetailsContext) GetMinipoolDetails(mc *batch.MultiCaller, mp minipool.IMinipool, index int) {
	c.snContext.GetMinipoolDetails(mc, mp, index)
}

func (c *minipoolCloseDetailsContext) PrepareData(addresses []common.Address, mps []minipool.IMinipool, data *csapi.MinipoolCloseDetailsData) (types.ResponseStatus, error) {
	code, err := c.snContext.PrepareData(addresses, mps, c.snData)
	data.Details = c.snData.Details
	return code, err
}
