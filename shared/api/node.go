package csapi

import "github.com/rocket-pool/node-manager-core/eth"

type NodeGetRegistrationStatusData struct {
	Registered bool `json:"registered"`
}

type NodeRegisterData struct {
	TxInfo                   *eth.TransactionInfo `json:"txInfo"`
	NotAuthorized            bool                 `json:"notAuthorized"`
	NotRegisteredWithNodeSet bool                 `json:"notRegisteredWithNodeSet"`
	InvalidPermissions       bool                 `json:"invalidPermissions"`
	IncorrectNodeAddress     bool                 `json:"incorrectNodeAddress"`
}
