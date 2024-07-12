package csminipool

import (
	"context"

	"github.com/gorilla/mux"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	"github.com/rocket-pool/node-manager-core/api/server"
	"github.com/rocket-pool/node-manager-core/log"
)

type MinipoolHandler struct {
	logger          *log.Logger
	ctx             context.Context
	serviceProvider *cscommon.ConstellationServiceProvider
	factories       []server.IContextFactory
}

func NewMinipoolHandler(logger *log.Logger, ctx context.Context, serviceProvider *cscommon.ConstellationServiceProvider) *MinipoolHandler {
	h := &MinipoolHandler{
		logger:          logger,
		ctx:             ctx,
		serviceProvider: serviceProvider,
	}
	h.factories = []server.IContextFactory{
		&minipoolGetAvailableMinipoolCountContextFactory{h},
		&minipoolDepositMinipoolContextFactory{h},
	}
	return h
}

func (h *MinipoolHandler) RegisterRoutes(router *mux.Router) {
	subrouter := router.PathPrefix("/minipool").Subrouter()
	for _, factory := range h.factories {
		factory.RegisterRoute(subrouter)
	}
}
