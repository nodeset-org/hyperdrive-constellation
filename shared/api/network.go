package csapi

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

type NetworkStatsData struct {
	SubnodeCount                 int            `json:"subnodeCount"`
	ActiveMinipoolCount          int            `json:"activeMinipoolCount"`
	InitializedMinipoolCount     int            `json:"initializedMinipoolCount"`
	PrelaunchMinipoolCount       int            `json:"prelaunchMinipoolCount"`
	StakingMinipoolCount         int            `json:"stakingMinipoolCount"`
	DissolvedMinipoolCount       int            `json:"dissolvedMinipoolCount"`
	FinalizedMinipoolCount       int            `json:"finalizedMinipoolCount"`
	SuperNodeAddress             common.Address `json:"superNodeAddress"`
	SuperNodeRplStake            *big.Int       `json:"superNodeRplStake"`
	ConstellationEthBalance      *big.Int       `json:"constellationEthBalance"`
	ConstellationRplBalance      *big.Int       `json:"constellationRplBalance"`
	RocketPoolEthBalance         *big.Int       `json:"rocketPoolEthBalance"`
	MinipoolQueueLength          int            `json:"minipoolQueueLength"`
	MinipoolQueueCapacity        *big.Int       `json:"minipoolQueueCapacity"`
	RplPrice                     *big.Int       `json:"rplPrice"`
	RocketPoolEthUtilizationRate *big.Int       `json:"rocketPoolEthUtilizationRate"`
	ValidatorLimit               int            `json:"validatorLimit"`
}
