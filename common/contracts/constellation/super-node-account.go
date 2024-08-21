package constellation

import (
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/beacon"
	"github.com/rocket-pool/node-manager-core/eth"
)

const (
	superNodeAccountAbiString string = `[{"inputs":[{"internalType":"uint256","name":"expectedBondAmount","type":"uint256"},{"internalType":"uint256","name":"actualBondAmount","type":"uint256"}],"name":"BadBondAmount","type":"error"},{"inputs":[{"internalType":"address","name":"expected","type":"address"},{"internalType":"address","name":"actual","type":"address"}],"name":"BadPredictedCreation","type":"error"},{"inputs":[{"internalType":"bytes32","name":"role","type":"bytes32"},{"internalType":"address","name":"user","type":"address"}],"name":"BadRole","type":"error"},{"inputs":[{"internalType":"address","name":"expectedSender","type":"address"}],"name":"BadSender","type":"error"},{"inputs":[{"internalType":"uint256","name":"expectedBalance","type":"uint256"},{"internalType":"uint256","name":"actualBalance","type":"uint256"}],"name":"InsufficientBalance","type":"error"},{"inputs":[{"internalType":"bool","name":"success","type":"bool"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"LowLevelCall","type":"error"},{"inputs":[{"internalType":"bool","name":"success","type":"bool"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"LowLevelEthTransfer","type":"error"},{"inputs":[{"internalType":"address","name":"addr","type":"address"}],"name":"NotAContract","type":"error"},{"inputs":[],"name":"ZeroAddressError","type":"error"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"previousAdmin","type":"address"},{"indexed":false,"internalType":"address","name":"newAdmin","type":"address"}],"name":"AdminChanged","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"beacon","type":"address"}],"name":"BeaconUpgraded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint8","name":"version","type":"uint8"}],"name":"Initialized","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"minipoolAddress","type":"address"},{"indexed":true,"internalType":"address","name":"operatorAddress","type":"address"}],"name":"MinipoolCreated","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"minipoolAddress","type":"address"},{"indexed":true,"internalType":"address","name":"operatorAddress","type":"address"}],"name":"MinipoolDestroyed","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"implementation","type":"address"}],"name":"Upgraded","type":"event"},{"inputs":[],"name":"adminServerCheck","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"adminServerSigExpiry","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"allowSubOpDelegateChanges","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"bond","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"subNodeOperatorAddress","type":"address"},{"internalType":"address","name":"minipoolAddress","type":"address"}],"name":"close","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"components":[{"internalType":"bytes","name":"validatorPubkey","type":"bytes"},{"internalType":"bytes","name":"validatorSignature","type":"bytes"},{"internalType":"bytes32","name":"depositDataRoot","type":"bytes32"},{"internalType":"uint256","name":"salt","type":"uint256"},{"internalType":"address","name":"expectedMinipoolAddress","type":"address"},{"internalType":"uint256","name":"sigGenesisTime","type":"uint256"},{"internalType":"bytes","name":"sig","type":"bytes"}],"internalType":"struct SuperNodeAccount.CreateMinipoolConfig","name":"_config","type":"tuple"}],"name":"createMinipool","outputs":[],"stateMutability":"payable","type":"function"},{"inputs":[{"internalType":"address","name":"_minipool","type":"address"}],"name":"delegateRollback","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"_minipool","type":"address"}],"name":"delegateUpgrade","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"getDirectory","outputs":[{"internalType":"contract Directory","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getImplementation","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"minipool","type":"address"}],"name":"getIsMinipoolRecognized","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getNumMinipools","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getTotalEthMatched","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getTotalEthStaked","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"_bond","type":"uint256"}],"name":"hasSufficientLiquidity","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"_directory","type":"address"}],"name":"initialize","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"lazyInitialize","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"lockThreshold","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"lockedEth","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"maxValidators","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"minimumNodeFee","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"minipoolData","outputs":[{"internalType":"address","name":"subNodeOperator","type":"address"},{"internalType":"uint256","name":"ethTreasuryFee","type":"uint256"},{"internalType":"uint256","name":"noFee","type":"uint256"},{"internalType":"uint256","name":"index","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"","type":"uint256"}],"name":"minipools","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"nonces","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"proxiableUUID","outputs":[{"internalType":"bytes32","name":"","type":"bytes32"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"minipool","type":"address"}],"name":"removeMinipool","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bool","name":"newValue","type":"bool"}],"name":"setAdminServerCheck","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"_newExpiry","type":"uint256"}],"name":"setAdminServerSigExpiry","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bool","name":"newValue","type":"bool"}],"name":"setAllowSubNodeOpDelegateChanges","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"_newBond","type":"uint256"}],"name":"setBond","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"_newLockThreshold","type":"uint256"}],"name":"setLockAmount","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"_maxValidators","type":"uint256"}],"name":"setMaxValidators","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"_newMinimumNodeFee","type":"uint256"}],"name":"setMinimumNodeFee","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bool","name":"_useSmoothingPool","type":"bool"}],"name":"setSmoothingPoolParticipation","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bool","name":"_setting","type":"bool"},{"internalType":"address","name":"_minipool","type":"address"}],"name":"setUseLatestDelegate","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes","name":"","type":"bytes"}],"name":"sigsUsed","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"bytes","name":"_validatorSignature","type":"bytes"},{"internalType":"bytes32","name":"_depositDataRoot","type":"bytes32"},{"internalType":"address","name":"_minipool","type":"address"}],"name":"stake","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"totalEthLocked","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"newImplementation","type":"address"}],"name":"upgradeTo","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newImplementation","type":"address"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"upgradeToAndCall","outputs":[],"stateMutability":"payable","type":"function"},{"stateMutability":"payable","type":"receive"}]`
)

// ABI cache
var superNodeAccountAbi abi.ABI
var superNodeAccountOnce sync.Once

type createMinipoolConfig struct {
	ValidatorPubkey         []byte         `abi:"validatorPubkey"`
	ValidatorSignature      []byte         `abi:"validatorSignature"`
	DepositDataRoot         common.Hash    `abi:"depositDataRoot"`
	Salt                    *big.Int       `abi:"salt"`
	ExpectedMinipoolAddress common.Address `abi:"expectedMinipoolAddress"`
	Signature               []byte         `abi:"sig"`
}

type MinipoolData struct {
	NodeAddress    common.Address `abi:"subNodeOperator"`
	EthTreasuryFee *big.Int       `abi:"ethTreasuryFee"`
	NodeFee        *big.Int       `abi:"noFee"`
	RplTreasuryFee *big.Int       `abi:"rplTreasuryFee"`
}

type SuperNodeAccount struct {
	Address  common.Address
	contract *eth.Contract
	txMgr    *eth.TransactionManager
}

type MerkleRewardsConfig struct {
	Signature             []byte   `abi:"sig"`
	SignatureGenesisTime  *big.Int `abi:"sigGenesisTime"`
	AverageEthTreasuryFee *big.Int `abi:"avgEthTreasuryFee"`
	AverageEthOperatorFee *big.Int `abi:"avgEthOperatorFee"`
	AverageRplTreasuryFee *big.Int `abi:"avgRplTreasuryFee"`
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

// The total amount of ETH that will be bonded by both your lockup and Constellation as part of minipool creation
func (c *SuperNodeAccount) Bond(mc *batch.MultiCaller, out **big.Int) {
	eth.AddCallToMulticaller(mc, c.contract, out, "bond")
}

// The amount of ETH required to be sent by the subnode operator during a minipool deposit
func (c *SuperNodeAccount) LockThreshold(mc *batch.MultiCaller, out **big.Int) {
	eth.AddCallToMulticaller(mc, c.contract, out, "lockThreshold")
}

// The max number of minipools a subnode operator is allowed to have active
func (c *SuperNodeAccount) GetMaxValidators(mc *batch.MultiCaller, out **big.Int) {
	eth.AddCallToMulticaller(mc, c.contract, out, "maxValidators")
}

// The total number of minipools made via Constellation
func (c *SuperNodeAccount) GetMinipoolCount(mc *batch.MultiCaller, out **big.Int) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getNumMinipools")
}

// Get the address of the minipool at the given index
func (c *SuperNodeAccount) GetMinipoolAddress(mc *batch.MultiCaller, out *common.Address, index *big.Int) {
	eth.AddCallToMulticaller(mc, c.contract, out, "minipools", index)
}

// Get a minipool's data
func (c *SuperNodeAccount) GetMinipoolData(mc *batch.MultiCaller, out *MinipoolData, address common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "minipoolData", address)
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

func (c *SuperNodeAccount) CreateMinipool(
	validatorPubkey beacon.ValidatorPubkey,
	validatorSignature beacon.ValidatorSignature,
	depositDataRoot common.Hash,
	constellationSalt *big.Int,
	expectedMinipoolAddress common.Address,
	nodeSetSignature []byte, opts *bind.TransactOpts,
) (*eth.TransactionInfo, error) {

	cfg := createMinipoolConfig{
		ValidatorPubkey:         validatorPubkey[:],
		ValidatorSignature:      validatorSignature[:],
		DepositDataRoot:         depositDataRoot,
		Salt:                    constellationSalt,
		ExpectedMinipoolAddress: expectedMinipoolAddress,
		Signature:               nodeSetSignature,
	}

	return c.txMgr.CreateTransactionInfo(c.contract, "createMinipool", opts, cfg)
}

func (c *SuperNodeAccount) Stake(validatorSignature beacon.ValidatorSignature, depositDataRoot common.Hash, minipool common.Address, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "stake", opts, validatorSignature[:], depositDataRoot, minipool)
}

func (c *SuperNodeAccount) UnlockEth(minipool common.Address, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "unlockEth", opts, minipool)
}

func (c *SuperNodeAccount) SetLockAmount(newLockThreshold *big.Int, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "setLockAmount", opts, newLockThreshold)
}

func (c *SuperNodeAccount) SetMaxValidators(maxValidators *big.Int, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "setMaxValidators", opts, maxValidators)
}

func (c *SuperNodeAccount) MerkleClaim(rewardIndex []*big.Int, amountRPL []*big.Int, amountETH []*big.Int, merkleProof [][]common.Hash, config *MerkleRewardsConfig, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "merkleClaim", opts, rewardIndex, amountRPL, amountETH, merkleProof, config)
}
