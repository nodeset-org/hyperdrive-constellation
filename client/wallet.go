package csclient

import (
	"strconv"

	"github.com/rocket-pool/node-manager-core/api/client"
	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/node-manager-core/beacon"
)

type WalletRequester struct {
	context client.IRequesterContext
}

func NewWalletRequester(context client.IRequesterContext) *WalletRequester {
	return &WalletRequester{
		context: context,
	}
}

func (r *WalletRequester) GetName() string {
	return "Wallet"
}
func (r *WalletRequester) GetRoute() string {
	return "wallet"
}
func (r *WalletRequester) GetContext() client.IRequesterContext {
	return r.context
}

// Recover a validator key
func (r *WalletRequester) CreateValidatorKey(pubkey beacon.ValidatorPubkey, index uint64, maxAttempts uint64) (*types.ApiResponse[types.SuccessData], error) {
	args := map[string]string{
		"pubkey":       pubkey.Hex(),
		"start-index":  strconv.FormatUint(index, 10),
		"max-attempts": strconv.FormatUint(maxAttempts, 10),
	}
	return client.SendGetRequest[types.SuccessData](r, "create-validator-key", "CreateValidatorKey", args)
}
