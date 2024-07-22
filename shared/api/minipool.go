package csapi

import (
	"github.com/ethereum/go-ethereum/common"
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
	MinipoolAddress           common.Address         `json:"minipoolAddress"`
	ValidatorPubKey           beacon.ValidatorPubkey `json:"validatorPubKey"`
	InsufficientLiquidity     bool                   `json:"insufficientLiquidity"`
	NotWhitelisted            bool                   `json:"notWhitelisted"`
	InsufficientMinipoolCount bool                   `json:"insufficientMinipoolCount"`
}

type MinipoolStakeMinipoolData struct {
	TxInfo                    *eth.TransactionInfo `json:"txInfo"`
	InsufficientLiquidity     bool                 `json:"insufficientLiquidity"`
	NotWhitelisted            bool                 `json:"notWhitelisted"`
	InsufficientMinipoolCount bool                 `json:"insufficientMinipoolCount"`
}
