package csapi

import (
	"github.com/rocket-pool/node-manager-core/beacon"

	"github.com/rocket-pool/node-manager-core/eth"
	snapi "github.com/rocket-pool/smartnode/v2/shared/types/api"
)

type MinipoolCloseDetailsData struct {
	Details []snapi.MinipoolCloseDetails `json:"details"`
}

type MinipoolGetAvailableMinipoolCountData struct {
	Count int `json:"count"`
}

type MinipoolDepositMinipoolData struct {
	TxInfo                    *eth.TransactionInfo   `json:"txInfo"`
	ValidatorPubKey           beacon.ValidatorPubkey `json:"validatorPubKey"`
	InsufficientLiquidity     bool                   `json:"insufficientLiquidity"`
	NotWhitelisted            bool                   `json:"notWhitelisted"`
	InsufficientMinipoolCount bool                   `json:"insufficientMinipoolCount"`
}
