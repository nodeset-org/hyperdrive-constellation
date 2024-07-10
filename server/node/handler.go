package csnode

import (
	"context"

	"github.com/gorilla/mux"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	"github.com/rocket-pool/node-manager-core/api/server"
	"github.com/rocket-pool/node-manager-core/log"
)

type NodeHandler struct {
	logger          *log.Logger
	ctx             context.Context
	serviceProvider *cscommon.ConstellationServiceProvider
	factories       []server.IContextFactory
}

func NewNodeHandler(logger *log.Logger, ctx context.Context, serviceProvider *cscommon.ConstellationServiceProvider) *NodeHandler {
	h := &NodeHandler{
		logger:          logger,
		ctx:             ctx,
		serviceProvider: serviceProvider,
	}
	h.factories = []server.IContextFactory{
		&nodeGetRegistrationStatusContextFactory{h},
		&nodeRegisterContextFactory{h},
	}
	return h
}

func (h *NodeHandler) RegisterRoutes(router *mux.Router) {
	subrouter := router.PathPrefix("/node").Subrouter()
	for _, factory := range h.factories {
		factory.RegisterRoute(subrouter)
	}
}
