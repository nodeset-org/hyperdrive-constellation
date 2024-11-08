package cswallet

import (
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/gorilla/mux"
	"github.com/rocket-pool/node-manager-core/wallet"

	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
	"github.com/rocket-pool/node-manager-core/api/types"
)

// ===============
// === Factory ===
// ===============

type walletCreateValidatorKeyContextFactory struct {
	handler *WalletHandler
}

func (f *walletCreateValidatorKeyContextFactory) Create(body csapi.WalletCreateValidatorKeyBody) (*walletCreateValidatorKeyContext, error) {
	c := &walletCreateValidatorKeyContext{
		handler: f.handler,
	}
	c.body = body
	return c, nil
}

func (f *walletCreateValidatorKeyContextFactory) RegisterRoute(router *mux.Router) {
	server.RegisterQuerylessPost[*walletCreateValidatorKeyContext, csapi.WalletCreateValidatorKeyBody, csapi.WalletCreateValidatorKeyData](
		router, "create-validator-key", f, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type walletCreateValidatorKeyContext struct {
	handler *WalletHandler

	// Inputs
	body csapi.WalletCreateValidatorKeyBody
}

func (c *walletCreateValidatorKeyContext) PrepareData(data *csapi.WalletCreateValidatorKeyData, walletStatus wallet.WalletStatus, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	sp := c.handler.serviceProvider
	ctx := c.handler.ctx
	logger := c.handler.logger
	vMgr := sp.GetWallet()

	// Requirements
	err := sp.RequireWalletReady(walletStatus)
	if err != nil {
		return types.ResponseStatus_WalletNotReady, err
	}

	// Regenerate the key, save it, and load into VC if requested
	pubkey := c.body.Pubkey
	index, err := vMgr.RecoverValidatorKey(
		ctx,
		logger.Logger,
		pubkey,
		c.body.StartIndex,
		c.body.MaxAttempts,
		c.body.SlashingProtection,
		c.body.LoadIntoVc,
	)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating validator key for pubkey %s: %w", pubkey.HexWithPrefix(), err)
	}
	data.Index = index
	return types.ResponseStatus_Success, nil
}
