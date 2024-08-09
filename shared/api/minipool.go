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

type MinipoolExitInfo struct {
	Address common.Address         `json:"address"`
	Pubkey  beacon.ValidatorPubkey `json:"pubkey"`
	Index   string                 `json:"index"`
}

type MinipoolExitBody struct {
	Infos []MinipoolExitInfo `json:"infos"`
}

/*
type MinipoolDetails struct {
	Address         common.Address         `json:"address"`
	ValidatorPubkey beacon.ValidatorPubkey `json:"validatorPubkey"`
	Version         uint8                  `json:"version"`
	Status          struct {
		Status      types.MinipoolStatus `json:"status"`
		StatusBlock uint64               `json:"statusBlock"`
		StatusTime  time.Time            `json:"statusTime"`
	} `json:"status"`
	Node struct {
		Fee             float64        `json:"fee"`
		DepositBalance  *big.Int       `json:"depositBalance"`
		RefundBalance   *big.Int       `json:"refundBalance"`
		DepositAssigned bool           `json:"depositAssigned"`
	} `json:"node"`
	User struct {
		DepositBalance      *big.Int  `json:"depositBalance"`
		DepositAssigned     bool      `json:"depositAssigned"`
		DepositAssignedTime time.Time `json:"depositAssignedTime"`
	} `json:"user"`
	Balance               *big.Int `json:"balance"`
	NodeShareOfEthBalance *big.Int `json:"nodeShareOfEthBalance"`
	Validator             struct {
		Exists      bool     `json:"exists"`
		Active      bool     `json:"active"`
		Index       string   `json:"index"`
		Balance     *big.Int `json:"balance"`
		NodeBalance *big.Int `json:"nodeBalance"`
	} `json:"validator"`
	CanStake          bool                  `json:"canStake"`
	Queue             minipool.QueueDetails `json:"queue"`
	CloseAvailable    bool                  `json:"closeAvailable"`
	Finalised         bool                  `json:"finalised"`
	UseLatestDelegate bool                  `json:"useLatestDelegate"`
	Delegate          common.Address        `json:"delegate"`
	PreviousDelegate  common.Address        `json:"previousDelegate"`
	EffectiveDelegate common.Address        `json:"effectiveDelegate"`
	TimeUntilDissolve time.Duration         `json:"timeUntilDissolve"`
	Penalties         uint64                `json:"penalties"`
}

type MinipoolStatusData struct {
	Minipools      []MinipoolDetails `json:"minipools"`
	LatestDelegate common.Address    `json:"latestDelegate"`
}
*/

type MinipoolStatusData struct {
	Minipools      []snapi.MinipoolDetails `json:"minipools"`
	LatestDelegate common.Address          `json:"latestDelegate"`
}

type MinipoolCreateData struct {
	CanCreate                       bool                   `json:"canCreate"`
	InsufficientBalance             bool                   `json:"insufficientBalance"`
	InsufficientLiquidity           bool                   `json:"insufficientLiquidity"`
	NotRegisteredWithNodeSet        bool                   `json:"notRegisteredWithNodeSet"`
	NotWhitelistedWithConstellation bool                   `json:"notWhitelistedWithConstellation"`
	InsufficientMinipoolCount       bool                   `json:"insufficientMinipoolCount"`
	RocketPoolDepositingDisabled    bool                   `json:"rocketPoolDepositingDisabled"`
	NodeSetDepositingDisabled       bool                   `json:"noteSetDepositingDisabled"`
	MaxMinipoolsReached             bool                   `json:"maxMinipoolsReached"`
	NodeBalance                     *big.Int               `json:"nodeBalance"`
	LockupAmount                    *big.Int               `json:"lockupAmount"`
	LockupTime                      time.Duration          `json:"lockupTime"`
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
