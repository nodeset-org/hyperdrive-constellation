package cswallet

import (
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/gorilla/mux"
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/node-manager-core/wallet"
)

// ===============
// === Factory ===
// ===============

type walletDeleteValidatorKeysContextFactory struct {
	handler *WalletHandler
}

func (f *walletDeleteValidatorKeysContextFactory) Create(body csapi.WalletDeleteKeyBody) (*walletDeleteValidatorKeysContext, error) {
	c := &walletDeleteValidatorKeysContext{
		handler: f.handler,
	}
	c.body = body
	return c, nil
}

func (f *walletDeleteValidatorKeysContextFactory) RegisterRoute(router *mux.Router) {
	server.RegisterQuerylessPost[*walletDeleteValidatorKeysContext, csapi.WalletDeleteKeyBody, csapi.WalletDeleteKeyData](
		router, "delete-validator-key", f, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type walletDeleteValidatorKeysContext struct {
	handler *WalletHandler

	body csapi.WalletDeleteKeyBody
}

func (c *walletDeleteValidatorKeysContext) PrepareData(data *csapi.WalletDeleteKeyData, walletStatus wallet.WalletStatus, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	sp := c.handler.serviceProvider
	ctx := c.handler.ctx
	logger := c.handler.logger
	wallet := sp.GetWallet()

	slashingProtection, err := wallet.DeleteValidatorKey(ctx, logger.Logger, c.body.Pubkey, c.body.IncludeVc)
	if err != nil {
		return types.ResponseStatus_Error, err
	}
	data.SlashingProtection = slashingProtection
	return types.ResponseStatus_Success, nil
}
