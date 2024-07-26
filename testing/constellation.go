package cstesting

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/nodeset-org/hyperdrive-constellation/common/contracts"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/rocketpool-go/v2/rocketpool"
	"github.com/rocket-pool/rocketpool-go/v2/tokens"
)

// Mint RPL and deposit it into the RPL Vault
func (m *ConstellationTestManager) Constellation_DepositToRplVault(rplVault contracts.IErc4626Token, amount *big.Int, depositOpts *bind.TransactOpts, owner *bind.TransactOpts) error {
	// Make some bindings
	rp := m.sp.GetRocketPoolManager().RocketPool
	txMgr := m.sp.GetTransactionManager()
	rplContract, err := rp.GetContract(rocketpool.ContractName_RocketTokenRPL)
	if err != nil {
		return fmt.Errorf("error getting RPL contract: %w", err)
	}
	fsrpl, err := tokens.NewTokenRplFixedSupply(rp)
	if err != nil {
		return fmt.Errorf("error getting fixed supply RPL token binding: %w", err)
	}
	rpl, err := tokens.NewTokenRpl(rp)
	if err != nil {
		return fmt.Errorf("error getting RPL token binding: %w", err)
	}

	// Mint RPL
	submissions, err := eth.BatchCreateTransactionSubmissions([]func() (string, *eth.TransactionInfo, error){
		func() (string, *eth.TransactionInfo, error) {
			txInfo, err := m.RocketPool_MintLegacyRpl(depositOpts.From, amount, owner)
			return "minting legacy RPL", txInfo, err
		},
	}, true)
	if err != nil {
		return err
	}
	txs, err := txMgr.BatchExecuteTransactions(submissions, &bind.TransactOpts{
		From:      owner.From,
		Signer:    owner.Signer,
		Nonce:     nil,
		Context:   owner.Context,
		GasFeeCap: owner.GasFeeCap,
		GasTipCap: owner.GasTipCap,
	})
	if err != nil {
		return fmt.Errorf("error submitting mint transactions: %w", err)
	}

	// Mint and deposit RPL
	submissions, err = eth.BatchCreateTransactionSubmissions([]func() (string, *eth.TransactionInfo, error){
		func() (string, *eth.TransactionInfo, error) {
			txInfo, err := fsrpl.Approve(rplContract.Address, amount, depositOpts)
			return "approve legacy RPL for swap", txInfo, err
		},
		func() (string, *eth.TransactionInfo, error) {
			txInfo, err := rpl.SwapFixedSupplyRplForRpl(amount, depositOpts)
			return "swap legacy RPL for new RPL", txInfo, err
		},
		func() (string, *eth.TransactionInfo, error) {
			txInfo, err := rpl.Approve(rplVault.Address(), amount, depositOpts)
			return "approve RPL for deposit", txInfo, err
		},
		func() (string, *eth.TransactionInfo, error) {
			txInfo, err := rplVault.Deposit(amount, depositOpts.From, depositOpts)
			return "deposit RPL into the vault", txInfo, err
		},
	}, false)
	if err != nil {
		return err
	}

	// Submit the TX's
	txs, err = txMgr.BatchExecuteTransactions(submissions, &bind.TransactOpts{
		From:      depositOpts.From,
		Signer:    depositOpts.Signer,
		Nonce:     nil,
		Context:   depositOpts.Context,
		GasFeeCap: depositOpts.GasFeeCap,
		GasTipCap: depositOpts.GasTipCap,
	})
	if err != nil {
		return fmt.Errorf("error submitting deposit transactions: %w", err)
	}

	// Mine the block
	err = m.CommitBlock()
	if err != nil {
		return fmt.Errorf("error committing block: %w", err)
	}
	err = txMgr.WaitForTransactions(txs)
	if err != nil {
		return fmt.Errorf("error waiting for deploy transactions: %w", err)
	}
	return nil
}

// Swap ETH for WETH and deposit it into the WETH Vault
func (m *ConstellationTestManager) Constellation_DepositToWethVault(weth *contracts.Weth, wethVault contracts.IErc4626Token, amount *big.Int, opts *bind.TransactOpts) error {
	// Services
	txMgr := m.sp.GetTransactionManager()

	// Mint and deposit WETH
	submissions, err := eth.BatchCreateTransactionSubmissions([]func() (string, *eth.TransactionInfo, error){
		func() (string, *eth.TransactionInfo, error) {
			wethOpts := &bind.TransactOpts{
				From:  opts.From,
				Value: amount,
			}
			txInfo, err := weth.Deposit(wethOpts)
			return "minting WETH", txInfo, err
		},
		func() (string, *eth.TransactionInfo, error) {
			txInfo, err := weth.Approve(wethVault.Address(), amount, opts)
			return "approve WETH for deposit", txInfo, err
		},
		func() (string, *eth.TransactionInfo, error) {
			txInfo, err := wethVault.Deposit(amount, opts.From, opts)
			return "deposit WETH into the vault", txInfo, err
		},
	}, false)
	if err != nil {
		return err
	}

	// Submit the TX's
	txs, err := txMgr.BatchExecuteTransactions(submissions, &bind.TransactOpts{
		From:      opts.From,
		Signer:    opts.Signer,
		Nonce:     nil,
		Context:   opts.Context,
		GasFeeCap: opts.GasFeeCap,
		GasTipCap: opts.GasTipCap,
	})
	if err != nil {
		return fmt.Errorf("error submitting deposit transactions: %w", err)
	}

	// Mine the block
	err = m.CommitBlock()
	if err != nil {
		return fmt.Errorf("error committing block: %w", err)
	}
	err = txMgr.WaitForTransactions(txs)
	if err != nil {
		return fmt.Errorf("error waiting for deposit transactions: %w", err)
	}
	return nil
}
