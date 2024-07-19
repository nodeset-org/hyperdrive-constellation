package contracts

import (
	"fmt"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rocket-pool/node-manager-core/beacon"
	"github.com/rocket-pool/node-manager-core/eth"
)

const (
	casperDepositAbiString string = `[{"name":"DepositEvent","inputs":[{"type":"bytes","name":"pubkey","indexed":false},{"type":"bytes","name":"withdrawal_credentials","indexed":false},{"type":"bytes","name":"amount","indexed":false},{"type":"bytes","name":"signature","indexed":false},{"type":"bytes","name":"index","indexed":false}],"anonymous":false,"type":"event"},{"outputs":[],"inputs":[],"constant":false,"payable":false,"type":"constructor"},{"name":"get_deposit_root","outputs":[{"type":"bytes32","name":"out"}],"inputs":[],"constant":true,"payable":false,"type":"function","gas":91674},{"name":"get_deposit_count","outputs":[{"type":"bytes","name":"out"}],"inputs":[],"constant":true,"payable":false,"type":"function","gas":10433},{"name":"deposit","outputs":[],"inputs":[{"type":"bytes","name":"pubkey"},{"type":"bytes","name":"withdrawal_credentials"},{"type":"bytes","name":"signature"},{"type":"bytes32","name":"deposit_data_root"}],"constant":false,"payable":true,"type":"function","gas":1334547}]`
)

// ABI cache
var casperDepositAbi abi.ABI
var casperDepositOnce sync.Once

type CasperDeposit struct {
	Address  common.Address
	contract *eth.Contract
	txMgr    *eth.TransactionManager
}

// Create a new CasperDeposit instance
func NewCasperDeposit(address common.Address, ec eth.IExecutionClient, txMgr *eth.TransactionManager) (*CasperDeposit, error) {
	// Parse the ABI
	var err error
	casperDepositOnce.Do(func() {
		var parsedAbi abi.ABI
		parsedAbi, err = abi.JSON(strings.NewReader(casperDepositAbiString))
		if err == nil {
			casperDepositAbi = parsedAbi
		}
	})
	if err != nil {
		return nil, fmt.Errorf("error parsing CasperDeposit ABI: %w", err)
	}

	// Create the contract
	contract := &eth.Contract{
		ContractImpl: bind.NewBoundContract(address, casperDepositAbi, ec, ec, ec),
		Address:      address,
		ABI:          &casperDepositAbi,
	}

	return &CasperDeposit{
		Address:  address,
		contract: contract,
		txMgr:    txMgr,
	}, nil
}

// =============
// === Calls ===
// =============

// ====================
// === Transactions ===
// ====================

func (c *CasperDeposit) Deposit(pubkey beacon.ValidatorPubkey, withdrawalCredentials common.Hash, signature beacon.ValidatorSignature, depositDataRoot common.Hash, opts *bind.TransactOpts) (*eth.TransactionInfo, error) {
	newOpts := &bind.TransactOpts{
		From:  opts.From,
		Value: eth.EthToWei(1),
	}
	return c.txMgr.CreateTransactionInfo(c.contract, "deposit", newOpts, pubkey[:], withdrawalCredentials[:], signature[:], depositDataRoot)
}
