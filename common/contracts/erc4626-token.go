package contracts

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
	"github.com/rocket-pool/node-manager-core/eth/contracts"
)

const (
	erc4626AbiString string = `[{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"owner","type":"address"},{"indexed":true,"internalType":"address","name":"spender","type":"address"},{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"}],"name":"Approval","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"sender","type":"address"},{"indexed":true,"internalType":"address","name":"owner","type":"address"},{"indexed":false,"internalType":"uint256","name":"assets","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"shares","type":"uint256"}],"name":"Deposit","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"from","type":"address"},{"indexed":true,"internalType":"address","name":"to","type":"address"},{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"}],"name":"Transfer","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"sender","type":"address"},{"indexed":true,"internalType":"address","name":"receiver","type":"address"},{"indexed":true,"internalType":"address","name":"owner","type":"address"},{"indexed":false,"internalType":"uint256","name":"assets","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"shares","type":"uint256"}],"name":"Withdraw","type":"event"},{"inputs":[{"internalType":"address","name":"owner","type":"address"},{"internalType":"address","name":"spender","type":"address"}],"name":"allowance","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"spender","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"approve","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"asset","outputs":[{"internalType":"address","name":"assetTokenAddress","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"account","type":"address"}],"name":"balanceOf","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"shares","type":"uint256"}],"name":"convertToAssets","outputs":[{"internalType":"uint256","name":"assets","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"assets","type":"uint256"}],"name":"convertToShares","outputs":[{"internalType":"uint256","name":"shares","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"decimals","outputs":[{"internalType":"uint8","name":"","type":"uint8"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"assets","type":"uint256"},{"internalType":"address","name":"receiver","type":"address"}],"name":"deposit","outputs":[{"internalType":"uint256","name":"shares","type":"uint256"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"receiver","type":"address"}],"name":"maxDeposit","outputs":[{"internalType":"uint256","name":"maxAssets","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"receiver","type":"address"}],"name":"maxMint","outputs":[{"internalType":"uint256","name":"maxShares","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"owner","type":"address"}],"name":"maxRedeem","outputs":[{"internalType":"uint256","name":"maxShares","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"owner","type":"address"}],"name":"maxWithdraw","outputs":[{"internalType":"uint256","name":"maxAssets","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"shares","type":"uint256"},{"internalType":"address","name":"receiver","type":"address"}],"name":"mint","outputs":[{"internalType":"uint256","name":"assets","type":"uint256"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"name","outputs":[{"internalType":"string","name":"","type":"string"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"assets","type":"uint256"}],"name":"previewDeposit","outputs":[{"internalType":"uint256","name":"shares","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"shares","type":"uint256"}],"name":"previewMint","outputs":[{"internalType":"uint256","name":"assets","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"shares","type":"uint256"}],"name":"previewRedeem","outputs":[{"internalType":"uint256","name":"assets","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"assets","type":"uint256"}],"name":"previewWithdraw","outputs":[{"internalType":"uint256","name":"shares","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"shares","type":"uint256"},{"internalType":"address","name":"receiver","type":"address"},{"internalType":"address","name":"owner","type":"address"}],"name":"redeem","outputs":[{"internalType":"uint256","name":"assets","type":"uint256"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"symbol","outputs":[{"internalType":"string","name":"","type":"string"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"totalAssets","outputs":[{"internalType":"uint256","name":"totalManagedAssets","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"totalSupply","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"to","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"transfer","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"from","type":"address"},{"internalType":"address","name":"to","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"transferFrom","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"assets","type":"uint256"},{"internalType":"address","name":"receiver","type":"address"},{"internalType":"address","name":"owner","type":"address"}],"name":"withdraw","outputs":[{"internalType":"uint256","name":"shares","type":"uint256"}],"stateMutability":"nonpayable","type":"function"}]`
)

// ==================
// === Interfaces ===
// ==================

// Simple binding for ERC4626 tokens.
// See https://eips.ethereum.org/EIPS/eip-4626 for details.
type IErc4626Token interface {
	contracts.IErc20Token

	// The address of the underlying asset token managed by the vault
	Asset() contracts.IErc20Token

	// Converts a number of underlying assets to an equivalent amount of vault shares.
	// This is effectively the "price" of the shares, in terms of the share:asset ratio.
	ConvertToShares(mc *batch.MultiCaller, out **big.Int, assets *big.Int)

	// Converts a number of vault shares to an equivalent amount of underyling asset.
	// This is effectively the "price" of the asset, in terms of the asset:share ratio.
	ConvertToAssets(mc *batch.MultiCaller, out **big.Int, shares *big.Int)

	// Deposits exactly `assets` of underlying tokens into the vault and sends the corresponding amount of vault shares to `receiver`
	Deposit(assets *big.Int, receiver common.Address, opts *bind.TransactOpts) (*eth.TransactionInfo, error)

	// Burns exactly `shares` from `owner` and sends the corresponding amount of underlying tokens to `receiver`
	Redeem(shares *big.Int, receiver common.Address, owner common.Address, opts *bind.TransactOpts) (*eth.TransactionInfo, error)
}

// ABI cache
var erc4626AbiStringAbi abi.ABI
var erc4626AbiStringOnce sync.Once

type erc4626Token struct {
	contracts.IErc20Token
	contract *eth.Contract
	txMgr    *eth.TransactionManager
	asset    contracts.IErc20Token
}

// Create a new Erc4626Token instance
func NewErc4626Token(address common.Address, ec eth.IExecutionClient, qMgr *eth.QueryManager, txMgr *eth.TransactionManager, opts *bind.CallOpts) (IErc4626Token, error) {
	// Parse the ABI
	var err error
	erc4626AbiStringOnce.Do(func() {
		var parsedAbi abi.ABI
		parsedAbi, err = abi.JSON(strings.NewReader(erc4626AbiString))
		if err == nil {
			erc4626AbiStringAbi = parsedAbi
		}
	})
	if err != nil {
		return nil, fmt.Errorf("error parsing Erc4626Token ABI: %w", err)
	}

	// Create the contract
	contract := &eth.Contract{
		ContractImpl: bind.NewBoundContract(address, erc4626AbiStringAbi, ec, ec, ec),
		Address:      address,
		ABI:          &erc4626AbiStringAbi,
	}

	// Get the details
	var asset common.Address
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		eth.AddCallToMulticaller(mc, contract, &asset, "asset")
		return nil
	}, opts)
	if err != nil {
		return nil, fmt.Errorf("error getting ERC-4626 details of token %s: %w", address.Hex(), err)
	}
	assetBinding, err := contracts.NewErc20Contract(asset, ec, qMgr, txMgr, opts)
	if err != nil {
		return nil, fmt.Errorf("error creating ERC20 binding for ERC4626 token asset %s: %w", asset.Hex(), err)
	}

	// Create the ERC20 binding
	erc20, err := contracts.NewErc20Contract(address, ec, qMgr, txMgr, opts)
	if err != nil {
		return nil, fmt.Errorf("error creating ERC20 binding for ERC4626 token: %w", err)
	}

	return &erc4626Token{
		IErc20Token: erc20,
		contract:    contract,
		txMgr:       txMgr,
		asset:       assetBinding,
	}, nil
}

// The address of the underlying asset token managed by the vault
func (c *erc4626Token) Asset() contracts.IErc20Token {
	return c.asset
}

// =============
// === Calls ===
// =============

// Converts a number of underlying assets to an equivalent amount of vault shares.
// This is effectively the "price" of the shares, in terms of the share:asset ratio.
func (c *erc4626Token) ConvertToShares(mc *batch.MultiCaller, out **big.Int, assets *big.Int) {
	eth.AddCallToMulticaller(mc, c.contract, out, "convertToShares", assets)
}

// Converts a number of vault shares to an equivalent amount of underyling asset.
// This is effectively the "price" of the asset, in terms of the asset:share ratio.
func (c *erc4626Token) ConvertToAssets(mc *batch.MultiCaller, out **big.Int, shares *big.Int) {
	eth.AddCallToMulticaller(mc, c.contract, out, "convertToAssets", shares)
}

// ====================
// === Transactions ===
// ====================

// Deposits exactly `assets` of underlying tokens into the vault and sends the corresponding amount of vault shares to `receiver`
func (c *erc4626Token) Deposit(assets *big.Int, receiver common.Address, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "deposit", opts, assets, receiver)
}

// Burns exactly `shares` from `owner` and sends the corresponding amount of underlying tokens to `receiver`
func (c *erc4626Token) Redeem(shares *big.Int, receiver common.Address, owner common.Address, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "redeem", opts, shares, receiver, owner)
}
