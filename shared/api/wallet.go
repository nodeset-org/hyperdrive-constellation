package csapi

import (
	"github.com/rocket-pool/node-manager-core/beacon"
)

// ================
// === Requests ===
// ================

type WalletCreateValidatorKeyBody struct {
	Pubkey             beacon.ValidatorPubkey         `json:"pubkey"`
	StartIndex         uint64                         `json:"startIndex"`
	MaxAttempts        uint64                         `json:"maxAttempts"`
	LoadIntoVc         bool                           `json:"loadIntoVc"`
	SlashingProtection *beacon.SlashingProtectionData `json:"slashingProtection"`
}

type WalletDeleteKeyBody struct {
	Pubkey    beacon.ValidatorPubkey `json:"pubkey"`
	IncludeVc bool                   `json:"includeVc"`
}

// =================
// === Responses ===
// =================

type WalletCreateValidatorKeyData struct {
	Index uint64 `json:"index"`
}

type WalletGetKeysData struct {
	KeysOnDisk []beacon.ValidatorPubkey `json:"keysOnDisk"`
	KeysInVc   []beacon.ValidatorPubkey `json:"keysInVc"`
}

type WalletDeleteKeyData struct {
	SlashingProtection *beacon.SlashingProtectionData `json:"slashingProtection"`
}
