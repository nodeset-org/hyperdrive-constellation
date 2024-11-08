package cswallet

import (
	"errors"
	"net/url"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/gorilla/mux"
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
	nmcserver "github.com/rocket-pool/node-manager-core/api/server"
	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/node-manager-core/beacon"
	nmcinput "github.com/rocket-pool/node-manager-core/utils/input"
	"github.com/rocket-pool/node-manager-core/wallet"
)

// ===============
// === Factory ===
// ===============

type walletGetKeysContextFactory struct {
	handler *WalletHandler
}

func (f *walletGetKeysContextFactory) Create(args url.Values) (*walletGetValidatorKeysContext, error) {
	c := &walletGetValidatorKeysContext{
		handler: f.handler,
	}
	inputErrs := []error{
		nmcserver.ValidateArg("include-vc", args, nmcinput.ValidateBool, &c.includeVc),
	}
	return c, errors.Join(inputErrs...)
}

func (f *walletGetKeysContextFactory) RegisterRoute(router *mux.Router) {
	server.RegisterQuerylessGet[*walletGetValidatorKeysContext, csapi.WalletGetKeysData](
		router, "validator-keys", f, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type walletGetValidatorKeysContext struct {
	handler *WalletHandler

	includeVc bool
}

func (c *walletGetValidatorKeysContext) PrepareData(data *csapi.WalletGetKeysData, walletStatus wallet.WalletStatus, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	sp := c.handler.serviceProvider
	ctx := c.handler.ctx
	logger := c.handler.logger
	wallet := sp.GetWallet()

	keys, err := wallet.GetStoredAndLoadedKeys(ctx, logger.Logger, c.includeVc)
	if err != nil {
		return types.ResponseStatus_Error, err
	}

	keysOnDisk := []beacon.ValidatorPubkey{}
	keysInVc := []beacon.ValidatorPubkey{}
	for _, key := range keys {
		if key.IsStoredOnDisk {
			keysOnDisk = append(keysOnDisk, key.Pubkey)
		}
		if key.IsLoadedInValidatorClient {
			keysInVc = append(keysInVc, key.Pubkey)
		}
	}
	data.KeysOnDisk = keysOnDisk
	data.KeysInVc = keysInVc
	return types.ResponseStatus_Success, nil
}
