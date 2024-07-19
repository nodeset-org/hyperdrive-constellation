package constellation

import (
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/eth"
)

const (
	superNodeAccountAbiString string = `[{"inputs":[{"internalType":"uint256","name":"expectedBondAmount","type":"uint256"},{"internalType":"uint256","name":"actualBondAmount","type":"uint256"}],"name":"BadBondAmount","type":"error"},{"inputs":[{"internalType":"address","name":"expected","type":"address"},{"internalType":"address","name":"actual","type":"address"}],"name":"BadPredictedCreation","type":"error"},{"inputs":[{"internalType":"bytes32","name":"role","type":"bytes32"},{"internalType":"address","name":"user","type":"address"}],"name":"BadRole","type":"error"},{"inputs":[{"internalType":"address","name":"expectedSender","type":"address"}],"name":"BadSender","type":"error"},{"inputs":[{"internalType":"uint256","name":"expectedBalance","type":"uint256"},{"internalType":"uint256","name":"actualBalance","type":"uint256"}],"name":"InsufficientBalance","type":"error"},{"inputs":[{"internalType":"bool","name":"success","type":"bool"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"LowLevelCall","type":"error"},{"inputs":[{"internalType":"bool","name":"success","type":"bool"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"LowLevelEthTransfer","type":"error"},{"inputs":[{"internalType":"address","name":"addr","type":"address"}],"name":"NotAContract","type":"error"},{"inputs":[],"name":"ZeroAddressError","type":"error"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"previousAdmin","type":"address"},{"indexed":false,"internalType":"address","name":"newAdmin","type":"address"}],"name":"AdminChanged","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"beacon","type":"address"}],"name":"BeaconUpgraded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint8","name":"version","type":"uint8"}],"name":"Initialized","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"implementation","type":"address"}],"name":"Upgraded","type":"event"},{"inputs":[],"name":"adminServerCheck","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"adminServerSigExpiry","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"bond","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"_subNodeOperator","type":"address"},{"internalType":"address","name":"_minipool","type":"address"}],"name":"close","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"configs","outputs":[{"internalType":"bytes","name":"validatorPubkey","type":"bytes"},{"internalType":"bytes","name":"validatorSignature","type":"bytes"},{"internalType":"bytes32","name":"depositDataRoot","type":"bytes32"},{"internalType":"uint256","name":"salt","type":"uint256"},{"internalType":"address","name":"expectedMinipoolAddress","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[{"components":[{"internalType":"bytes","name":"validatorPubkey","type":"bytes"},{"internalType":"bytes","name":"validatorSignature","type":"bytes"},{"internalType":"bytes32","name":"depositDataRoot","type":"bytes32"},{"internalType":"uint256","name":"salt","type":"uint256"},{"internalType":"address","name":"expectedMinipoolAddress","type":"address"}],"internalType":"struct SuperNodeAccount.ValidatorConfig","name":"_config","type":"tuple"},{"internalType":"uint256","name":"_sigGenesisTime","type":"uint256"},{"internalType":"bytes","name":"_sig","type":"bytes"}],"name":"createMinipool","outputs":[],"stateMutability":"payable","type":"function"},{"inputs":[],"name":"currentMinipool","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"_minipool","type":"address"}],"name":"delegateRollback","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"_minipool","type":"address"}],"name":"delegateUpgrade","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"disableAdminServerCheck","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bool","name":"_rewardsOnly","type":"bool"},{"internalType":"address","name":"_subNodeOperator","type":"address"},{"internalType":"address","name":"_minipool","type":"address"}],"name":"distributeBalance","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"getDirectory","outputs":[{"internalType":"contract Directory","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getImplementation","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getNextMinipool","outputs":[{"internalType":"contract IMinipool","name":"","type":"address"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"_bond","type":"uint256"}],"name":"hasSufficientLiquidity","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"_directory","type":"address"}],"name":"initialize","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"lazyInitialize","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"lockStarted","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"lockThreshhold","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"lockUpTime","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"lockedEth","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"_nodeAddress","type":"address"},{"internalType":"uint256[]","name":"_rewardIndex","type":"uint256[]"},{"internalType":"uint256[]","name":"_amountRPL","type":"uint256[]"},{"internalType":"uint256[]","name":"_amountETH","type":"uint256[]"},{"internalType":"bytes32[][]","name":"_merkleProof","type":"bytes32[][]"}],"name":"merkleClaim","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"minimumNodeFee","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"minipoolIndex","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"","type":"uint256"}],"name":"minipools","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"proxiableUUID","outputs":[{"internalType":"bytes32","name":"","type":"bytes32"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"_newExpiry","type":"uint256"}],"name":"setAdminServerSigExpiry","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"_newBond","type":"uint256"}],"name":"setBond","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"_newLockThreshold","type":"uint256"}],"name":"setLockAmount","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"_newLockUpTime","type":"uint256"}],"name":"setLockUpTime","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"_newMinimumNodeFee","type":"uint256"}],"name":"setMiniumNodeFee","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bool","name":"_setting","type":"bool"},{"internalType":"address","name":"_minipool","type":"address"}],"name":"setUseLatestDelegate","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes","name":"","type":"bytes"}],"name":"sigsUsed","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"_minipool","type":"address"}],"name":"stake","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"_subNodeOperator","type":"address"}],"name":"stopTrackingOperatorMinipools","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes32","name":"","type":"bytes32"}],"name":"subNodeOperatorHasMinipool","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"","type":"address"},{"internalType":"uint256","name":"","type":"uint256"}],"name":"subNodeOperatorMinipools","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"totalEthLocked","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"totalEthStaking","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"_minipool","type":"address"}],"name":"unlockEth","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newImplementation","type":"address"}],"name":"upgradeTo","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newImplementation","type":"address"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"upgradeToAndCall","outputs":[],"stateMutability":"payable","type":"function"},{"inputs":[],"name":"useAdminServerCheck","outputs":[],"stateMutability":"nonpayable","type":"function"},{"stateMutability":"payable","type":"receive"}]`
)

// ABI cache
var superNodeAccountAbi abi.ABI
var superNodeAccountOnce sync.Once

type SuperNodeAccount struct {
	Address  common.Address
	contract *eth.Contract
	txMgr    *eth.TransactionManager
}

// TODO: Need Json tag (name of parameter in abi)
type ValidatorConfig struct {
	TimezoneLocation        string         `json:"timezoneLocation"`
	BondAmount              *big.Int       `json:"bondAmount"`
	MinimumNodeFee          *big.Int       `json:"minimumNodeFee"`
	ValidatorPubkey         []byte         `json:"validatorPubkey"`
	ValidatorSignature      []byte         `json:"validatorSignature"`
	DepositDataRoot         [32]byte       `json:"depositDataRoot"`
	Salt                    *big.Int       `json:"salt"`
	ExpectedMinipoolAddress common.Address `json:"expectedMinipoolAddress"`
}

// Create a new SuperNodeAccount instance
func NewSuperNodeAccount(address common.Address, ec eth.IExecutionClient, txMgr *eth.TransactionManager) (*SuperNodeAccount, error) {
	// Parse the ABI
	var err error
	superNodeAccountOnce.Do(func() {
		var parsedAbi abi.ABI
		parsedAbi, err = abi.JSON(strings.NewReader(superNodeAccountAbiString))
		if err == nil {
			superNodeAccountAbi = parsedAbi
		}
	})
	if err != nil {
		return nil, fmt.Errorf("error parsing SuperNodeAccount ABI: %w", err)
	}

	// Create the contract
	contract := &eth.Contract{
		ContractImpl: bind.NewBoundContract(address, superNodeAccountAbi, ec, ec, ec),
		Address:      address,
		ABI:          &superNodeAccountAbi,
	}

	return &SuperNodeAccount{
		Address:  address,
		contract: contract,
		txMgr:    txMgr,
	}, nil
}

// =============
// === Calls ===
// =============

func (c *SuperNodeAccount) GetNextMinipool(mc *batch.MultiCaller, out *common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getNextMinipool")
}

func (c *SuperNodeAccount) SubNodeOperatorHasMinipool(mc *batch.MultiCaller, out *bool, subNode common.Address, minipoolAddress common.Address) {
	key := crypto.Keccak256(subNode[:], minipoolAddress[:]) // Temp until there's a proper view for this
	eth.AddCallToMulticaller(mc, c.contract, out, "subNodeOperatorHasMinipool", key)
}

func (c *SuperNodeAccount) GetSubNodeMinipoolAt(mc *batch.MultiCaller, out *common.Address, subNode common.Address, index *big.Int) {
	eth.AddCallToMulticaller(mc, c.contract, out, "subNodeOperatorMinipools", subNode, index)
}

func (c *SuperNodeAccount) HasSufficientLiquidity(mc *batch.MultiCaller, out *bool, bondAmount *big.Int) {
	eth.AddCallToMulticaller(mc, c.contract, out, "hasSufficientLiquidity", bondAmount)
}

// ====================
// === Transactions ===
// ====================

func (c *SuperNodeAccount) Close(subNode common.Address, minipool common.Address, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "close", opts, subNode, minipool)
}

func (c *SuperNodeAccount) DistributeBalance(rewardsOnly bool, subNode common.Address, minipool common.Address, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "distributeBalance", opts, rewardsOnly, subNode, minipool)
}

func (c *SuperNodeAccount) DelegateRollback(minipool common.Address, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "delegateRollback", opts, minipool)
}

func (c *SuperNodeAccount) DelegateUpgrade(minipool common.Address, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "delegateUpgrade", opts, minipool)
}

func (c *SuperNodeAccount) SetUseLatestDelegate(setting bool, minipool common.Address, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "setUseLatestDelegate", opts, setting, minipool)
}

func (c *SuperNodeAccount) CreateMinipool(config ValidatorConfig, sig []byte, signatureGenesisTime time.Time, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	timestamp := signatureGenesisTime.UTC().Unix()
	timestampBig := big.NewInt(timestamp)

	return c.txMgr.CreateTransactionInfo(c.contract, "createMinipool", opts, config, timestampBig, sig)
}

func (c *SuperNodeAccount) Stake(minipool common.Address, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "stake", opts, minipool)
}
