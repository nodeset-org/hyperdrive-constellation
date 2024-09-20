package csminipool

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/gorilla/mux"

	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	modserver "github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
	"github.com/rocket-pool/node-manager-core/api/types"
	nmc_validator "github.com/rocket-pool/node-manager-core/node/validator"
	"github.com/rocket-pool/node-manager-core/wallet"
	eth2types "github.com/wealdtech/go-eth2-types/v2"
)

// ===============
// === Factory ===
// ===============

type minipoolExitContextFactory struct {
	handler *MinipoolHandler
}

func (f *minipoolExitContextFactory) Create(body csapi.MinipoolExitBody) (*MinipoolExitContext, error) {
	c := &MinipoolExitContext{
		ServiceProvider: f.handler.serviceProvider,
		Logger:          f.handler.logger.Logger,
		Context:         f.handler.ctx,
	}
	c.Infos = body.Infos
	return c, nil
}

func (f *minipoolExitContextFactory) RegisterRoute(router *mux.Router) {
	modserver.RegisterQuerylessPost[*MinipoolExitContext, csapi.MinipoolExitBody, types.SuccessData](
		router, "exit", f, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type MinipoolExitContext struct {
	// Dependencies
	ServiceProvider cscommon.IConstellationServiceProvider
	Logger          *slog.Logger
	Context         context.Context

	// Arguments
	Infos []csapi.MinipoolValidatorInfo
}

func (c *MinipoolExitContext) PrepareData(data *types.SuccessData, walletStatus wallet.WalletStatus, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	sp := c.ServiceProvider
	ctx := c.Context
	w := sp.GetWallet()
	bc := sp.GetBeaconClient()

	// Requirements
	err := sp.RequireBeaconClientSynced(c.Context)
	if err != nil {
		return types.ResponseStatus_ClientsNotSynced, err
	}

	// Get beacon head
	head, err := bc.GetBeaconHead(ctx)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting beacon head: %w", err)
	}

	// Get voluntary exit signature domain
	signatureDomain, err := bc.GetDomainData(ctx, eth2types.DomainVoluntaryExit[:], head.Epoch, false)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting beacon domain data: %w", err)
	}

	// Exit each minipool
	for _, info := range c.Infos {
		address := info.Address
		pubkey := info.Pubkey
		index := info.Index

		// Get validator private key
		validatorKey, err := w.LoadValidatorKey(pubkey)
		if err != nil {
			return types.ResponseStatus_Error, fmt.Errorf("error getting private key for minipool %s (pubkey %s): %w", address.Hex(), pubkey.Hex(), err)
		}

		// Get signed voluntary exit message
		signature, err := nmc_validator.GetSignedExitMessage(validatorKey, index, head.Epoch, signatureDomain)
		if err != nil {
			return types.ResponseStatus_Error, fmt.Errorf("error getting exit message signature for minipool %s (pubkey %s): %w", address.Hex(), pubkey.Hex(), err)
		}

		// Broadcast voluntary exit message
		if err := bc.ExitValidator(ctx, index, head.Epoch, signature); err != nil {
			return types.ResponseStatus_Error, fmt.Errorf("error submitting exit message for minipool %s (pubkey %s): %w", address.Hex(), pubkey.Hex(), err)
		}
		c.Logger.Info("Validator exit submitted",
			slog.String("pubkey", pubkey.Hex()),
		)
	}
	return types.ResponseStatus_Success, nil
}
