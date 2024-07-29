package csminipool

import "math/big"

const (
	minipoolAddressQueryBatchSize  int  = 1000
	minipoolDetailsBatchSize       int  = 100
	minipoolCompleteShareBatchSize int  = 500
	validatorKeyRetrievalLimit     uint = 2000
)

var (
	oneGwei *big.Int = big.NewInt(1e9) // TODO: put into NMC
)
