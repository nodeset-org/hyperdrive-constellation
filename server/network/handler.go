package csnetwork

import (
	"context"

	"github.com/gorilla/mux"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	"github.com/rocket-pool/node-manager-core/api/server"
	"github.com/rocket-pool/node-manager-core/log"
)

type NetworkHandler struct {
	logger          *log.Logger
	ctx             context.Context
	serviceProvider cscommon.IConstellationServiceProvider
	factories       []server.IContextFactory
}

func NewNetworkHandler(logger *log.Logger, ctx context.Context, serviceProvider cscommon.IConstellationServiceProvider) *NetworkHandler {
	h := &NetworkHandler{
		logger:          logger,
		ctx:             ctx,
		serviceProvider: serviceProvider,
	}
	h.factories = []server.IContextFactory{
		&networkStatsContextFactory{h},
	}
	return h
}

func (h *NetworkHandler) RegisterRoutes(router *mux.Router) {
	subrouter := router.PathPrefix("/network").Subrouter()
	for _, factory := range h.factories {
		factory.RegisterRoute(subrouter)
	}
}
