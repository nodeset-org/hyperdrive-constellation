package contracts

import (
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/node-manager-core/eth/contracts"
)

const (
	wethAbiString string = `[{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"owner","type":"address"},{"indexed":true,"internalType":"address","name":"spender","type":"address"},{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"}],"name":"Approval","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"from","type":"address"},{"indexed":true,"internalType":"address","name":"to","type":"address"},{"indexed":false,"internalType":"uint256","name":"value","type":"uint256"}],"name":"Transfer","type":"event"},{"inputs":[{"internalType":"address","name":"owner","type":"address"},{"internalType":"address","name":"spender","type":"address"}],"name":"allowance","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"guy","type":"address"},{"internalType":"uint256","name":"wad","type":"uint256"}],"name":"approve","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"account","type":"address"}],"name":"balanceOf","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"deposit","outputs":[],"stateMutability":"payable","type":"function"},{"inputs":[],"name":"totalSupply","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"dst","type":"address"},{"internalType":"uint256","name":"wad","type":"uint256"}],"name":"transfer","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"src","type":"address"},{"internalType":"address","name":"dst","type":"address"},{"internalType":"uint256","name":"wad","type":"uint256"}],"name":"transferFrom","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"wad","type":"uint256"}],"name":"withdraw","outputs":[],"stateMutability":"nonpayable","type":"function"}]`
)

// ABI cache
var wethAbiStringAbi abi.ABI
var wethAbiStringOnce sync.Once

type Weth struct {
	contracts.IErc20Token
	Address  common.Address
	contract *eth.Contract
	txMgr    *eth.TransactionManager
}

// Create a new Weth instance
func NewWeth(address common.Address, ec eth.IExecutionClient, qMgr *eth.QueryManager, txMgr *eth.TransactionManager, opts *bind.CallOpts) (*Weth, error) {
	// Parse the ABI
	var err error
	wethAbiStringOnce.Do(func() {
		var parsedAbi abi.ABI
		parsedAbi, err = abi.JSON(strings.NewReader(wethAbiString))
		if err == nil {
			wethAbiStringAbi = parsedAbi
		}
	})
	if err != nil {
		return nil, fmt.Errorf("error parsing Weth ABI: %w", err)
	}

	// Create the contract
	contract := &eth.Contract{
		ContractImpl: bind.NewBoundContract(address, wethAbiStringAbi, ec, ec, ec),
		Address:      address,
		ABI:          &wethAbiStringAbi,
	}

	// Create the ERC20 binding
	erc20, err := contracts.NewErc20Contract(address, ec, qMgr, txMgr, opts)
	if err != nil {
		return nil, fmt.Errorf("error creating Weth ERC20 binding: %w", err)
	}

	return &Weth{
		IErc20Token: erc20,
		Address:     address,
		contract:    contract,
		txMgr:       txMgr,
	}, nil
}

// =============
// === Calls ===
// =============

// ====================
// === Transactions ===
// ====================

// Get info for approving fixed-supply RPL's usage by a spender
// TEMP: move into IERC20Token
func (c *Weth) Approve(spender common.Address, amount *big.Int, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "approve", opts, spender, amount)
}

// Deposit an amount of ETH and receive WETH in return
func (c *Weth) Deposit(opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	return c.txMgr.CreateTransactionInfo(c.contract, "deposit", opts)
}
