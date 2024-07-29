package csapi

import (
	"math/big"
	"time"

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

type MinipoolDepositData struct {
	CanDeposit                      bool                   `json:"canDeposit"`
	InsufficientBalance             bool                   `json:"insufficientBalance"`
	EthBalance                      *big.Int               `json:"balance"`
	InsufficientLiquidity           bool                   `json:"insufficientLiquidity"`
	NotRegisteredWithNodeSet        bool                   `json:"notRegisteredWithNodeSet"`
	NotWhitelistedWithConstellation bool                   `json:"notWhitelistedWithConstellation"`
	InsufficientMinipoolCount       bool                   `json:"insufficientMinipoolCount"`
	RocketPoolDepositingDisabled    bool                   `json:"rocketPoolDepositingDisabled"`
	NodeSetDepositingDisabled       bool                   `json:"noteSetDepositingDisabled"`
	MinipoolAddress                 common.Address         `json:"minipoolAddress"`
	ValidatorPubkey                 beacon.ValidatorPubkey `json:"validatorPubkey"`
	Index                           uint64                 `json:"index"`
	ScrubPeriod                     time.Duration          `json:"scrubPeriod"`
	TxInfo                          *eth.TransactionInfo   `json:"txInfo"`
}

type MinipoolStakeDetails struct {
	CanStake           bool                   `json:"canStake"`
	RemainingTime      time.Duration          `json:"remainingTime"`
	StillInScrubPeriod bool                   `json:"stillInScrubPeriod"`
	Address            common.Address         `json:"address"`
	Pubkey             beacon.ValidatorPubkey `json:"pubkey"`
	TxInfo             *eth.TransactionInfo   `json:"txInfo"`
}

type MinipoolStakeData struct {
	NotWhitelistedWithConstellation bool                   `json:"notWhitelistedWithConstellation"`
	Details                         []MinipoolStakeDetails `json:"details"`
}
