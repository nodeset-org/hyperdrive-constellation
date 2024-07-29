package cswallet

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/gorilla/mux"
	"github.com/rocket-pool/node-manager-core/beacon"
	"github.com/rocket-pool/node-manager-core/wallet"

	"github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
	nmcserver "github.com/rocket-pool/node-manager-core/api/server"
	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/node-manager-core/utils/input"
)

// ===============
// === Factory ===
// ===============

type walletCreateValidatorKeyContextFactory struct {
	handler *WalletHandler
}

func (f *walletCreateValidatorKeyContextFactory) Create(args url.Values) (*walletCreateValidatorKeyContext, error) {
	c := &walletCreateValidatorKeyContext{
		handler: f.handler,
	}
	inputErrs := []error{
		nmcserver.ValidateArg("pubkey", args, input.ValidatePubkey, &c.pubkey),
		nmcserver.ValidateArg("start-index", args, input.ValidateUint, &c.index),
		nmcserver.ValidateArg("max-attempts", args, input.ValidateUint, &c.maxAttempts),
	}
	return c, errors.Join(inputErrs...)
}

func (f *walletCreateValidatorKeyContextFactory) RegisterRoute(router *mux.Router) {
	server.RegisterQuerylessGet[*walletCreateValidatorKeyContext, types.SuccessData](
		router, "create-validator-key", f, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type walletCreateValidatorKeyContext struct {
	handler     *WalletHandler
	pubkey      beacon.ValidatorPubkey
	index       uint64
	maxAttempts uint64
}

func (c *walletCreateValidatorKeyContext) PrepareData(data *types.SuccessData, walletStatus wallet.WalletStatus, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	sp := c.handler.serviceProvider
	vMgr := sp.GetWallet()

	// Requirements
	err := sp.RequireWalletReady(walletStatus)
	if err != nil {
		return types.ResponseStatus_WalletNotReady, err
	}

	_, err = vMgr.RecoverValidatorKey(c.pubkey, c.index, c.maxAttempts)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating validator key: %w", err)
	}
	return types.ResponseStatus_Success, nil
}
