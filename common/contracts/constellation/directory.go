package constellation

import (
	"fmt"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/eth"
)

const (
	directoryAbiString string = `[{"inputs":[],"stateMutability":"nonpayable","type":"constructor"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"previousAdmin","type":"address"},{"indexed":false,"internalType":"address","name":"newAdmin","type":"address"}],"name":"AdminChanged","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"beacon","type":"address"}],"name":"BeaconUpgraded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint8","name":"version","type":"uint8"}],"name":"Initialized","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"bytes32","name":"role","type":"bytes32"},{"indexed":true,"internalType":"bytes32","name":"previousAdminRole","type":"bytes32"},{"indexed":true,"internalType":"bytes32","name":"newAdminRole","type":"bytes32"}],"name":"RoleAdminChanged","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"bytes32","name":"role","type":"bytes32"},{"indexed":true,"internalType":"address","name":"account","type":"address"},{"indexed":true,"internalType":"address","name":"sender","type":"address"}],"name":"RoleGranted","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"bytes32","name":"role","type":"bytes32"},{"indexed":true,"internalType":"address","name":"account","type":"address"},{"indexed":true,"internalType":"address","name":"sender","type":"address"}],"name":"RoleRevoked","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"account","type":"address"},{"indexed":false,"internalType":"address","name":"eoa_origin","type":"address"}],"name":"SanctionViolation","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"eoa_origin","type":"address"}],"name":"SanctionViolation","type":"event"},{"anonymous":false,"inputs":[],"name":"SanctionsDisabled","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"implementation","type":"address"}],"name":"Upgraded","type":"event"},{"inputs":[],"name":"DEFAULT_ADMIN_ROLE","outputs":[{"internalType":"bytes32","name":"","type":"bytes32"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"disableSanctions","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"enableSanctions","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"getImplementation","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getMerkleClaimStreamerAddress","outputs":[{"internalType":"address payable","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getOperatorDistributorAddress","outputs":[{"internalType":"address payable","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getOperatorRewardAddress","outputs":[{"internalType":"address payable","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getOracleAddress","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getPriceFetcherAddress","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getProtocol","outputs":[{"components":[{"internalType":"address","name":"whitelist","type":"address"},{"internalType":"address payable","name":"wethVault","type":"address"},{"internalType":"address","name":"rplVault","type":"address"},{"internalType":"address payable","name":"operatorDistributor","type":"address"},{"internalType":"address payable","name":"merkleClaimStreamer","type":"address"},{"internalType":"address payable","name":"operatorReward","type":"address"},{"internalType":"address","name":"oracle","type":"address"},{"internalType":"address","name":"priceFetcher","type":"address"},{"internalType":"address payable","name":"superNode","type":"address"},{"internalType":"address","name":"rocketStorage","type":"address"},{"internalType":"address payable","name":"weth","type":"address"},{"internalType":"address","name":"sanctions","type":"address"}],"internalType":"struct Protocol","name":"","type":"tuple"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getRPLAddress","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getRPLVaultAddress","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getRocketDAOProtocolProposalAddress","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getRocketDAOProtocolSettingsMinipool","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getRocketDAOProtocolSettingsRewardsAddress","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getRocketDepositPoolAddress","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getRocketMerkleDistributorMainnetAddress","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getRocketMinipoolManagerAddress","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getRocketNetworkPenalties","outputs":[{"internalType":"contract IRocketNetworkPenalties","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getRocketNetworkPrices","outputs":[{"internalType":"contract IRocketNetworkPrices","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getRocketNetworkVotingAddress","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getRocketNodeDepositAddress","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getRocketNodeManagerAddress","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getRocketNodeStakingAddress","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"string","name":"_tag","type":"string"}],"name":"getRocketPoolAddressByTag","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getRocketStorageAddress","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"bytes32","name":"role","type":"bytes32"}],"name":"getRoleAdmin","outputs":[{"internalType":"bytes32","name":"","type":"bytes32"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getSuperNodeAddress","outputs":[{"internalType":"address payable","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getTreasuryAddress","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getWETHAddress","outputs":[{"internalType":"address payable","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getWETHVaultAddress","outputs":[{"internalType":"address payable","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getWhitelistAddress","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"bytes32","name":"role","type":"bytes32"},{"internalType":"address","name":"account","type":"address"}],"name":"grantRole","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes32","name":"role","type":"bytes32"},{"internalType":"address","name":"account","type":"address"}],"name":"hasRole","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[{"components":[{"internalType":"address","name":"whitelist","type":"address"},{"internalType":"address payable","name":"wethVault","type":"address"},{"internalType":"address","name":"rplVault","type":"address"},{"internalType":"address payable","name":"operatorDistributor","type":"address"},{"internalType":"address payable","name":"merkleClaimStreamer","type":"address"},{"internalType":"address payable","name":"operatorReward","type":"address"},{"internalType":"address","name":"oracle","type":"address"},{"internalType":"address","name":"priceFetcher","type":"address"},{"internalType":"address payable","name":"superNode","type":"address"},{"internalType":"address","name":"rocketStorage","type":"address"},{"internalType":"address payable","name":"weth","type":"address"},{"internalType":"address","name":"sanctions","type":"address"}],"internalType":"struct Protocol","name":"newProtocol","type":"tuple"},{"internalType":"address","name":"treasury","type":"address"},{"internalType":"address","name":"treasurer","type":"address"},{"internalType":"address","name":"admin","type":"address"}],"name":"initialize","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"_account1","type":"address"},{"internalType":"address","name":"_account2","type":"address"}],"name":"isSanctioned","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address[]","name":"_accounts","type":"address[]"}],"name":"isSanctioned","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"_account","type":"address"}],"name":"isSanctioned","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"proxiableUUID","outputs":[{"internalType":"bytes32","name":"","type":"bytes32"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"bytes32","name":"role","type":"bytes32"},{"internalType":"address","name":"account","type":"address"}],"name":"renounceRole","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes32","name":"role","type":"bytes32"},{"internalType":"address","name":"account","type":"address"}],"name":"revokeRole","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"components":[{"internalType":"address","name":"whitelist","type":"address"},{"internalType":"address payable","name":"wethVault","type":"address"},{"internalType":"address","name":"rplVault","type":"address"},{"internalType":"address payable","name":"operatorDistributor","type":"address"},{"internalType":"address payable","name":"merkleClaimStreamer","type":"address"},{"internalType":"address payable","name":"operatorReward","type":"address"},{"internalType":"address","name":"oracle","type":"address"},{"internalType":"address","name":"priceFetcher","type":"address"},{"internalType":"address payable","name":"superNode","type":"address"},{"internalType":"address","name":"rocketStorage","type":"address"},{"internalType":"address payable","name":"weth","type":"address"},{"internalType":"address","name":"sanctions","type":"address"}],"internalType":"struct Protocol","name":"newProtocol","type":"tuple"}],"name":"setAll","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newOracle","type":"address"}],"name":"setOracle","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newTreasury","type":"address"}],"name":"setTreasurer","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes4","name":"interfaceId","type":"bytes4"}],"name":"supportsInterface","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"newImplementation","type":"address"}],"name":"upgradeTo","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newImplementation","type":"address"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"upgradeToAndCall","outputs":[],"stateMutability":"payable","type":"function"}]`
)

// ABI cache
var directoryAbi abi.ABI
var directoryOnce sync.Once

type Directory struct {
	Address  common.Address
	contract *eth.Contract
	txMgr    *eth.TransactionManager
}

type Protocol struct {
	Whitelist           common.Address `json:"whitelist" abi:"whitelist"`
	WethVault           common.Address `json:"wethVault" abi:"wethVault"`
	RplVault            common.Address `json:"rplVault" abi:"rplVault"`
	OperatorDistributor common.Address `json:"operatorDistributor" abi:"operatorDistributor"`
	MerkleClaimStreamer common.Address `json:"merkleClaimStreamer" abi:"merkleClaimStreamer"`
	OperatorReward      common.Address `json:"operatorReward" abi:"operatorReward"`
	Oracle              common.Address `json:"oracle" abi:"oracle"`
	PriceFetcher        common.Address `json:"priceFetcher" abi:"priceFetcher"`
	SuperNode           common.Address `json:"superNode" abi:"superNode"`
	RocketStorage       common.Address `json:"rocketStorage" abi:"rocketStorage"`
	Weth                common.Address `json:"weth" abi:"weth"`
	Sanctions           common.Address `json:"sanctions" abi:"sanctions"`
}

// Create a new Directory instance
func NewDirectory(address common.Address, ec eth.IExecutionClient, txMgr *eth.TransactionManager) (*Directory, error) {
	// Parse the ABI
	var err error
	directoryOnce.Do(func() {
		var parsedAbi abi.ABI
		parsedAbi, err = abi.JSON(strings.NewReader(directoryAbiString))
		if err == nil {
			directoryAbi = parsedAbi
		}
	})
	if err != nil {
		return nil, fmt.Errorf("error parsing Directory ABI: %w", err)
	}

	// Create the contract
	contract := &eth.Contract{
		ContractImpl: bind.NewBoundContract(address, directoryAbi, ec, ec, ec),
		Address:      address,
		ABI:          &directoryAbi,
	}

	return &Directory{
		Address:  address,
		contract: contract,
		txMgr:    txMgr,
	}, nil
}

// =============
// === Calls ===
// =============

func (c *Directory) HasRole(mc *batch.MultiCaller, out *bool, role [32]byte, account common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "hasRole", role, account)
}

func (c *Directory) GetRocketStorageAddress(mc *batch.MultiCaller, out *common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getRocketStorageAddress")
}

func (c *Directory) GetSuperNodeAddress(mc *batch.MultiCaller, out *common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getSuperNodeAddress")
}

func (c *Directory) GetWhitelistAddress(mc *batch.MultiCaller, out *common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getWhitelistAddress")
}

func (c *Directory) GetOperatorDistributorAddress(mc *batch.MultiCaller, out *common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getOperatorDistributorAddress")
}

func (c *Directory) GetPriceFetcherAddress(mc *batch.MultiCaller, out *common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getPriceFetcherAddress")
}

func (c *Directory) GetWethVaultAddress(mc *batch.MultiCaller, out *common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getWETHVaultAddress")
}

func (c *Directory) GetRplVaultAddress(mc *batch.MultiCaller, out *common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getRPLVaultAddress")
}

func (c *Directory) GetWethAddress(mc *batch.MultiCaller, out *common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getWETHAddress")
}

func (c *Directory) GetTreasuryAddress(mc *batch.MultiCaller, out *common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getTreasuryAddress")
}

func (c *Directory) GetOracleAddress(mc *batch.MultiCaller, out *common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getOracleAddress")
}

func (c *Directory) GetOperatorRewardAddress(mc *batch.MultiCaller, out *common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getOperatorRewardAddress")
}

func (c *Directory) GetMerkleClaimStreamerAddress(mc *batch.MultiCaller, out *common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getMerkleClaimStreamerAddress")
}

func (c *Directory) GetRplAddress(mc *batch.MultiCaller, out *common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getRPLAddress")
}

func (c *Directory) GetRocketDaoProtocolProposalAddress(mc *batch.MultiCaller, out *common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getRocketDAOProtocolProposalAddress")
}

func (c *Directory) GetRocketDaoProtocolSettingsMinipool(mc *batch.MultiCaller, out *common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getRocketDAOProtocolSettingsMinipool")
}

func (c *Directory) GetRocketDaoProtocolSettingsRewardsAddress(mc *batch.MultiCaller, out *common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getRocketDAOProtocolSettingsRewardsAddress")
}

func (c *Directory) GetRocketDepositPoolAddress(mc *batch.MultiCaller, out *common.Address) {
	eth.AddCallToMulticaller(mc, c.contract, out, "getRocketDepositPoolAddress")
}

// ====================
// === Transactions ===
// ====================

func (c *Directory) SetAll(protocol Protocol, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "setAll", opts, protocol)
}
