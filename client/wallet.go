package csclient

import (
	"strconv"

	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
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
func (r *WalletRequester) CreateValidatorKey(pubkey beacon.ValidatorPubkey, index uint64, maxAttempts uint64, loadIntoVc bool, slashingProtection *beacon.SlashingProtectionData) (*types.ApiResponse[csapi.WalletCreateValidatorKeyData], error) {
	body := &csapi.WalletCreateValidatorKeyBody{
		Pubkey:             pubkey,
		StartIndex:         index,
		MaxAttempts:        maxAttempts,
		LoadIntoVc:         loadIntoVc,
		SlashingProtection: slashingProtection,
	}

	return client.SendPostRequest[csapi.WalletCreateValidatorKeyData](r, "create-validator-key", "CreateValidatorKey", body)
}

// Get all validator keys
func (r *WalletRequester) GetValidatorKeys(includeVc bool) (*types.ApiResponse[csapi.WalletGetKeysData], error) {
	args := map[string]string{
		"include-vc": strconv.FormatBool(includeVc),
	}
	return client.SendGetRequest[csapi.WalletGetKeysData](r, "validator-keys", "GetValidatorKeys", args)
}

// Delete a validator key
func (r *WalletRequester) DeleteValidatorKey(pubkey beacon.ValidatorPubkey, includeVc bool) (*types.ApiResponse[csapi.WalletDeleteKeyData], error) {
	body := &csapi.WalletDeleteKeyBody{
		Pubkey:    pubkey,
		IncludeVc: includeVc,
	}

	return client.SendPostRequest[csapi.WalletDeleteKeyData](r, "delete-validator-key", "DeleteValidatorKey", body)
}
