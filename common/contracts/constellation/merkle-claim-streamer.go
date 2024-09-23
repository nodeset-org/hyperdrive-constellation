package constellation

import (
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/eth"
)

const (
	merkleClaimStreamerAbiString string = `[{"inputs":[],"stateMutability":"nonpayable","type":"constructor"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"previousAdmin","type":"address"},{"indexed":false,"internalType":"address","name":"newAdmin","type":"address"}],"name":"AdminChanged","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"beacon","type":"address"}],"name":"BeaconUpgraded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint8","name":"version","type":"uint8"}],"name":"Initialized","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"uint256","name":"timestamp","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"newEthRewards","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"newRplRewards","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"ethTreasuryPortion","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"ethOperatorPortion","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"rplTreasuryPortion","type":"uint256"}],"name":"MerkleClaimSubmitted","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"implementation","type":"address"}],"name":"Upgraded","type":"event"},{"inputs":[],"name":"getDirectory","outputs":[{"internalType":"contract Directory","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getImplementation","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getStreamedTvlEth","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getStreamedTvlRpl","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"_directory","type":"address"}],"name":"initialize","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"lastClaimTime","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"merkleClaimsEnabled","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"priorEthStreamAmount","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"priorRplStreamAmount","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"proxiableUUID","outputs":[{"internalType":"bytes32","name":"","type":"bytes32"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"bool","name":"_isEnabled","type":"bool"}],"name":"setMerkleClaimsEnabled","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"_newStreamingInterval","type":"uint256"}],"name":"setStreamingInterval","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"streamingInterval","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256[]","name":"rewardIndex","type":"uint256[]"},{"internalType":"uint256[]","name":"amountRPL","type":"uint256[]"},{"internalType":"uint256[]","name":"amountETH","type":"uint256[]"},{"internalType":"bytes32[][]","name":"merkleProof","type":"bytes32[][]"}],"name":"submitMerkleClaim","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"sweepLockedTVL","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newImplementation","type":"address"}],"name":"upgradeTo","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newImplementation","type":"address"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"upgradeToAndCall","outputs":[],"stateMutability":"payable","type":"function"},{"stateMutability":"payable","type":"receive"}]`
)

// ABI cache
var merkleClaimStreamerAbi abi.ABI
var merkleClaimStreamerOnce sync.Once

type MerkleClaimStreamer struct {
	Address  common.Address
	contract *eth.Contract
	txMgr    *eth.TransactionManager
}

// Create a new MerkleClaimStreamer instance
func NewMerkleClaimStreamer(address common.Address, ec eth.IExecutionClient, txMgr *eth.TransactionManager) (*MerkleClaimStreamer, error) {
	// Parse the ABI
	var err error
	merkleClaimStreamerOnce.Do(func() {
		var parsedAbi abi.ABI
		parsedAbi, err = abi.JSON(strings.NewReader(merkleClaimStreamerAbiString))
		if err == nil {
			merkleClaimStreamerAbi = parsedAbi
		}
	})
	if err != nil {
		return nil, fmt.Errorf("error parsing MerkleClaimStreamer ABI: %w", err)
	}

	// Create the contract
	contract := &eth.Contract{
		ContractImpl: bind.NewBoundContract(address, merkleClaimStreamerAbi, ec, ec, ec),
		Address:      address,
		ABI:          &merkleClaimStreamerAbi,
	}

	return &MerkleClaimStreamer{
		Address:  address,
		contract: contract,
		txMgr:    txMgr,
	}, nil
}

// =============
// === Calls ===
// =============

func (c *MerkleClaimStreamer) GetPriorEthStreamAmount(mc *batch.MultiCaller, out **big.Int) {
	eth.AddCallToMulticaller(mc, c.contract, out, "priorEthStreamAmount")
}

func (c *MerkleClaimStreamer) GetPriorRplStreamAmount(mc *batch.MultiCaller, out **big.Int) {
	eth.AddCallToMulticaller(mc, c.contract, out, "priorRplStreamAmount")
}

func (c *MerkleClaimStreamer) GetStreamedTvlEth(mc *batch.MultiCaller, out **big.Int) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getStreamedTvlEth")
}

func (c *MerkleClaimStreamer) GetStreamedTvlRpl(mc *batch.MultiCaller, out **big.Int) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getStreamedTvlRpl")
}

// ====================
// === Transactions ===
// ====================
