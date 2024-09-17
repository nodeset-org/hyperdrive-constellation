package csminipool

import (
	"context"

	"github.com/gorilla/mux"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	"github.com/rocket-pool/node-manager-core/api/server"
	"github.com/rocket-pool/node-manager-core/log"
	snservices "github.com/rocket-pool/smartnode/v2/rocketpool-daemon/common/services"
)

type MinipoolHandler struct {
	logger            *log.Logger
	ctx               context.Context
	serviceProvider   cscommon.IConstellationServiceProvider
	factories         []server.IContextFactory
	snServiceProvider snservices.ISmartNodeServiceProvider
}

func NewMinipoolHandler(logger *log.Logger, ctx context.Context, serviceProvider cscommon.IConstellationServiceProvider) *MinipoolHandler {
	h := &MinipoolHandler{
		logger:            logger,
		ctx:               ctx,
		serviceProvider:   serviceProvider,
		snServiceProvider: serviceProvider.GetSmartNodeServiceProvider(),
	}
	h.factories = []server.IContextFactory{
		&minipoolCloseDetailsContextFactory{h},
		&minipoolCloseContextFactory{h},
		&minipoolExitContextFactory{h},
		&minipoolExitDetailsContextFactory{h},
		&minipoolCreateContextFactory{h},
		&minipoolStakeContextFactory{h},
		&minipoolStatusContextFactory{h},
		&minipoolUploadSignedExitsContextFactory{h},
		&minipoolVanityContextFactory{h},
	}
	return h
}

func (h *MinipoolHandler) RegisterRoutes(router *mux.Router) {
	subrouter := router.PathPrefix("/minipool").Subrouter()
	for _, factory := range h.factories {
		factory.RegisterRoute(subrouter)
	}
}
