package csapi

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rocket-pool/node-manager-core/beacon"

	"github.com/rocket-pool/node-manager-core/eth"
	rptypes "github.com/rocket-pool/rocketpool-go/v2/types"
	snapi "github.com/rocket-pool/smartnode/v2/shared/types/api"
)

type MinipoolCloseDetailsData struct {
	Details []snapi.MinipoolCloseDetails `json:"details"`
}

type MinipoolGetAvailableMinipoolCountData struct {
	Count int `json:"count"`
}

type MinipoolExitDetails struct {
	CanExit                bool                   `json:"canExit"`
	InvalidMinipoolStatus  bool                   `json:"invalidMinipoolStatus"`
	AlreadyFinalized       bool                   `json:"alreadyFinalized"`
	InvalidValidatorStatus bool                   `json:"invalidValidatorStatus"`
	ValidatorNotSeenYet    bool                   `json:"validatorNotSeenYet"`
	ValidatorTooYoung      bool                   `json:"validatorTooYoung"`
	Address                common.Address         `json:"address"`
	Pubkey                 beacon.ValidatorPubkey `json:"pubkey"`
	Index                  string                 `json:"index"`
	MinipoolStatus         rptypes.MinipoolStatus `json:"minipoolStatus"`
	MinipoolStatusTime     time.Time              `json:"minipoolStatusTime"`
	ValidatorStatus        beacon.ValidatorState  `json:"validatorStatus"`
	ActivationEpoch        uint64                 `json:"activationEpoch"`
	EligibleExitEpoch      uint64                 `json:"eligibleExitEpoch"`
}
type MinipoolExitDetailsData struct {
	Details      []MinipoolExitDetails `json:"details"`
	CurrentEpoch uint64                `json:"currentEpoch"`
}

type MinipoolValidatorInfo struct {
	Address common.Address         `json:"address"`
	Pubkey  beacon.ValidatorPubkey `json:"pubkey"`
	Index   string                 `json:"index"`
}

type MinipoolExitBody struct {
	Infos []MinipoolValidatorInfo `json:"infos"`
}

type MinipoolDetails struct {
	*snapi.MinipoolDetails
	RequiresSignedExit bool `json:"requiresSignedExit"`
}

type MinipoolStatusData struct {
	NotRegisteredWithNodeSet        bool              `json:"notRegisteredWithNodeSet"`
	NotWhitelistedWithConstellation bool              `json:"notWhitelistedWithConstellation"`
	IncorrectNodeAddress            bool              `json:"incorrectNodeAddress"`
	InvalidPermissions              bool              `json:"invalidPermissions"`
	Minipools                       []MinipoolDetails `json:"minipools"`
	LatestDelegate                  common.Address    `json:"latestDelegate"`
	MaxValidatorsPerNode            uint64            `json:"maxValidatorsPerNode"`
}

type MinipoolCreateData struct {
	CanCreate                       bool                   `json:"canCreate"`
	InsufficientBalance             bool                   `json:"insufficientBalance"`
	InsufficientLiquidity           bool                   `json:"insufficientLiquidity"`
	NotRegisteredWithNodeSet        bool                   `json:"notRegisteredWithNodeSet"`
	NotWhitelistedWithConstellation bool                   `json:"notWhitelistedWithConstellation"`
	IncorrectNodeAddress            bool                   `json:"incorrectNodeAddress"`
	MissingExitMessage              bool                   `json:"missingExitMessage"`
	InvalidPermissions              bool                   `json:"invalidPermissions"`
	RocketPoolDepositingDisabled    bool                   `json:"rocketPoolDepositingDisabled"`
	NodeSetDepositingDisabled       bool                   `json:"noteSetDepositingDisabled"`
	MaxMinipoolsReached             bool                   `json:"maxMinipoolsReached"`
	NodeBalance                     *big.Int               `json:"nodeBalance"`
	LockupAmount                    *big.Int               `json:"lockupAmount"`
	MinipoolAddress                 common.Address         `json:"minipoolAddress"`
	ValidatorPubkey                 beacon.ValidatorPubkey `json:"validatorPubkey"`
	Index                           uint64                 `json:"index"`
	ScrubPeriod                     time.Duration          `json:"scrubPeriod"`
	TxInfo                          *eth.TransactionInfo   `json:"txInfo"`
}

type MinipoolStakeDetails struct {
	CanStake           bool                   `json:"canStake"`
	StillInScrubPeriod bool                   `json:"stillInScrubPeriod"`
	RemainingTime      time.Duration          `json:"remainingTime"`
	TimeUntilDissolve  time.Duration          `json:"timeUntilDissolve"`
	Address            common.Address         `json:"address"`
	Pubkey             beacon.ValidatorPubkey `json:"pubkey"`
	TxInfo             *eth.TransactionInfo   `json:"txInfo"`
}

type MinipoolStakeData struct {
	NotWhitelistedWithConstellation bool                   `json:"notWhitelistedWithConstellation"`
	Details                         []MinipoolStakeDetails `json:"details"`
}

type MinipoolUploadSignedExitBody struct {
	Infos []MinipoolValidatorInfo `json:"infos"`
}

type MinipoolVanityArtifactsData struct {
	SubNodeAddress         common.Address `json:"subNodeAddress"`
	SuperNodeAddress       common.Address `json:"superNodeAddress"`
	MinipoolFactoryAddress common.Address `json:"minipoolFactoryAddress"`
	InitHash               common.Hash    `json:"initHash"`
}
type MinipoolGetPubkeysData struct {
	Infos []MinipoolValidatorInfo `json:"infos"`
}
