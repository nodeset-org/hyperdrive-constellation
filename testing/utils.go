package cstesting

import (
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	hdclient "github.com/nodeset-org/hyperdrive-daemon/client"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/stretchr/testify/require"
)

// Mines a new block with the provided transaction
func (m *ConstellationTestManager) MineTx(t *testing.T, txInfo *eth.TransactionInfo, opts *bind.TransactOpts, logMessage string) {
	// Services
	node := m.GetNode()
	sp := node.GetServiceProvider()
	txMgr := sp.GetTransactionManager()

	// Check the simulation
	require.True(t, txInfo.SimulationResult.IsSimulated)
	require.Empty(t, txInfo.SimulationResult.SimulationError)

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
	require.NoError(t, err)

	// Mine the tx
	err = m.CommitBlock()
	require.NoError(t, err)

	// Wait for the tx
	err = txMgr.WaitForTransaction(tx)
	require.NoError(t, err)
	t.Log(logMessage)
}

// Mines a new block with the provided transaction via the Hyperdrive daemon instead of submitting it directly to the EC
func (m *ConstellationTestManager) MineTxViaHyperdrive(t *testing.T, hdClient *hdclient.ApiClient, txInfo *eth.TransactionInfo, logMessage string) {
	// Submit the tx
	maxFee := eth.GweiToWei(1)
	maxPrioFee := eth.GweiToWei(0.1)
	submission, _ := eth.CreateTxSubmissionFromInfo(txInfo, nil)
	txResponse, err := hdClient.Tx.SubmitTx(submission, nil, maxFee, maxPrioFee)
	require.NoError(t, err)

	// Mine the tx
	err = m.CommitBlock()
	require.NoError(t, err)

	// Wait for the tx
	_, err = hdClient.Tx.WaitForTransaction(txResponse.Data.TxHash)
	require.NoError(t, err)
	t.Log(logMessage)
}
