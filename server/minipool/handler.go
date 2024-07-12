package csminipool

import (
	"context"

	"github.com/gorilla/mux"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	"github.com/rocket-pool/node-manager-core/api/server"
	"github.com/rocket-pool/node-manager-core/log"
	snminipool "github.com/rocket-pool/smartnode/v2/rocketpool-daemon/api/minipool"
)

type MinipoolHandler struct {
	logger          *log.Logger
	ctx             context.Context
	serviceProvider cscommon.IConstellationServiceProvider
	factories       []server.IContextFactory
	snHandler       *snminipool.MinipoolHandler
}

func NewMinipoolHandler(logger *log.Logger, ctx context.Context, serviceProvider cscommon.IConstellationServiceProvider) *MinipoolHandler {
	h := &MinipoolHandler{
		logger:          logger,
		ctx:             ctx,
		serviceProvider: serviceProvider,
		snHandler:       snminipool.NewMinipoolHandler(logger, ctx, serviceProvider.GetSmartNodeServiceProvider()),
	}
	h.factories = []server.IContextFactory{
		&minipoolCloseDetailsContextFactory{h},
		&minipoolCloseContextFactory{h},
		&minipoolGetAvailableMinipoolCountContextFactory{h},
	}
	return h
}

func (h *MinipoolHandler) RegisterRoutes(router *mux.Router) {
	subrouter := router.PathPrefix("/minipool").Subrouter()
	for _, factory := range h.factories {
		factory.RegisterRoute(subrouter)
	}
}
