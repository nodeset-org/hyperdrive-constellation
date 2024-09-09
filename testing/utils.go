package cstesting

import (
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	hdclient "github.com/nodeset-org/hyperdrive-daemon/client"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/stretchr/testify/require"
)

// Mines a new block with the provided transaction
func (m *ConstellationTestManager) MineTxBeforeTest(txInfo *eth.TransactionInfo, opts *bind.TransactOpts) error {
	// Services
	node := m.GetNode()
	sp := node.GetServiceProvider()
	txMgr := sp.GetTransactionManager()

	// Check the simulation
	if !txInfo.SimulationResult.IsSimulated {
		return fmt.Errorf("tx is not simulated")
	}
	if txInfo.SimulationResult.SimulationError != "" {
		return fmt.Errorf("tx simulation error: %s", txInfo.SimulationResult.SimulationError)
	}

	// Submit the tx
	submission, _ := eth.CreateTxSubmissionFromInfo(txInfo, nil)
	tx, err := txMgr.ExecuteTransaction(txInfo,
		&bind.TransactOpts{
			From:      opts.From,
			Signer:    opts.Signer,
			GasLimit:  submission.GasLimit,
			Value:     submission.TxInfo.Value,
			Nonce:     nil,
			GasPrice:  nil,
			GasFeeCap: nil,
			GasTipCap: nil,
			Context:   opts.Context,
			NoSend:    opts.NoSend,
		},
	)
	if err != nil {
		return fmt.Errorf("error executing transaction: %v", err)
	}

	// Mine the tx
	err = m.CommitBlock()
	if err != nil {
		return fmt.Errorf("error committing block: %v", err)
	}

	// Wait for the tx
	err = txMgr.WaitForTransaction(tx)
	if err != nil {
		return fmt.Errorf("error waiting for transaction: %v", err)
	}
	return nil
}

// Mines a new block with the provided transaction
func (m *ConstellationTestManager) MineTx(t *testing.T, txInfo *eth.TransactionInfo, opts *bind.TransactOpts, logMessage string) {
	err := m.MineTxBeforeTest(txInfo, opts)
	require.NoError(t, err)
	t.Log(logMessage)
}

// Mines a new block with the provided transaction via the Hyperdrive daemon instead of submitting it directly to the EC
func (m *ConstellationTestManager) MineTxViaHyperdrive(hdClient *hdclient.ApiClient, txInfo *eth.TransactionInfo) error {
	// Submit the tx
	maxFee := eth.GweiToWei(1)
	maxPrioFee := eth.GweiToWei(0.1)
	submission, _ := eth.CreateTxSubmissionFromInfo(txInfo, nil)
	txResponse, err := hdClient.Tx.SubmitTx(submission, nil, maxFee, maxPrioFee)
	if err != nil {
		return fmt.Errorf("error submitting tx: %w", err)
	}

	// Mine the tx
	err = m.CommitBlock()
	if err != nil {
		return fmt.Errorf("error committing block: %w", err)
	}

	// Wait for the tx
	_, err = hdClient.Tx.WaitForTransaction(txResponse.Data.TxHash)
	if err != nil {
		return fmt.Errorf("error waiting for transaction: %w", err)
	}
	return nil
}
