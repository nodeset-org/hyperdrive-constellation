package cswallet

import (
	"context"

	"github.com/gorilla/mux"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	"github.com/rocket-pool/node-manager-core/api/server"
	"github.com/rocket-pool/node-manager-core/log"
)

type WalletHandler struct {
	logger          *log.Logger
	ctx             context.Context
	serviceProvider cscommon.IConstellationServiceProvider
	factories       []server.IContextFactory
}

func NewWalletHandler(logger *log.Logger, ctx context.Context, serviceProvider cscommon.IConstellationServiceProvider) *WalletHandler {
	h := &WalletHandler{
		logger:          logger,
		ctx:             ctx,
		serviceProvider: serviceProvider,
	}
	h.factories = []server.IContextFactory{
		&walletCreateValidatorKeyContextFactory{handler: h},
		&walletGetKeysContextFactory{handler: h},
		&walletDeleteValidatorKeysContextFactory{handler: h},
	}
	return h
}

func (h *WalletHandler) RegisterRoutes(router *mux.Router) {
	subrouter := router.PathPrefix("/wallet").Subrouter()
	for _, factory := range h.factories {
		factory.RegisterRoute(subrouter)
	}
}
