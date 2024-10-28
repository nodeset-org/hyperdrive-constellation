package cstasks

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	"github.com/nodeset-org/hyperdrive-constellation/shared/keys"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/gas"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/tx"
	"github.com/rocket-pool/node-manager-core/beacon"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/node-manager-core/log"
	"github.com/rocket-pool/node-manager-core/node/validator"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
	rptypes "github.com/rocket-pool/rocketpool-go/v2/types"
)

var (
	oneGwei *big.Int = big.NewInt(1e9)
)

// Stake minipools task
type StakeMinipoolsTask struct {
	sp             cscommon.IConstellationServiceProvider
	logger         *slog.Logger
	ctx            context.Context
	res            *csconfig.MergedResources
	w              *cscommon.Wallet
	csMgr          *cscommon.ConstellationManager
	opts           *bind.TransactOpts
	gasThreshold   float64
	maxFee         *big.Int
	maxPriorityFee *big.Int
}

// Create a stake minipools task
func NewStakeMinipoolsTask(ctx context.Context, sp cscommon.IConstellationServiceProvider, logger *log.Logger) *StakeMinipoolsTask {
	hdCfg := sp.GetHyperdriveConfig()
	log := logger.With(slog.String(keys.TaskKey, "Minipool Stake"))
	maxFee, maxPriorityFee := tx.GetAutoTxInfo(hdCfg, log)
	gasThreshold := hdCfg.AutoTxGasThreshold.Value
	if maxFee != nil {
		log.Info("Auto-tx gas threshold is disabled because max fee is set.")
		gasThreshold = -1
	}
	return &StakeMinipoolsTask{
		ctx:            ctx,
		sp:             sp,
		logger:         log,
		res:            sp.GetResources(),
		w:              sp.GetWallet(),
		csMgr:          sp.GetConstellationManager(),
		gasThreshold:   gasThreshold,
		maxFee:         maxFee,
		maxPriorityFee: maxPriorityFee,
	}
}

// Stake prelaunch minipools
func (t *StakeMinipoolsTask) Run(snapshot *NetworkSnapshot) error {
	// Log
	t.logger.Info("Checking for minipools to launch...")

	// Get transactor
	nodeAddress := snapshot.ConstellationNode.NodeAddress
	t.opts = t.sp.GetSigner().GetTransactor(nodeAddress)

	// Get prelaunch minipools
	minipools, err := t.getPrelaunchMinipools(snapshot)
	if err != nil {
		return err
	}
	if len(minipools) == 0 {
		return nil
	}

	// Log
	t.logger.Info(
		"Minipools are ready for staking.",
		slog.Int("count", len(minipools)),
	)

	// Stake minipools
	txSubmissions := make([]*eth.TransactionSubmission, len(minipools))
	for i, mp := range minipools {
		txSubmissions[i], err = t.createStakeMinipoolTx(snapshot, mp)
		if err != nil {
			t.logger.Error(
				"Error preparing submission to stake minipool",
				slog.String("minipool", mp.Common().Address.Hex()),
				log.Err(err),
			)
			return err
		}
	}

	// Stake
	_, err = t.stakeMinipools(snapshot, txSubmissions, minipools)
	if err != nil {
		return fmt.Errorf("error staking minipools: %w", err)
	}

	// Return
	return nil
}

// Get prelaunch minipools
func (t *StakeMinipoolsTask) getPrelaunchMinipools(snapshot *NetworkSnapshot) ([]minipool.IMinipool, error) {
	// Prep data
	scrubPeriod := snapshot.RocketPoolNetworkSettings.ScrubPeriod
	blockTime := time.Unix(int64(snapshot.ExecutionBlockHeader.Time), 0)
	prelaunchMinipools := []minipool.IMinipool{}
	for _, mp := range snapshot.ConstellationNode.Minipools {
		mpCommon := mp.Common()
		if mpCommon.Status.Formatted() == rptypes.MinipoolStatus_Prelaunch {
			creationTime := mpCommon.StatusTime.Formatted()
			remainingTime := creationTime.Add(scrubPeriod).Sub(blockTime)
			if remainingTime < 0 {
				prelaunchMinipools = append(prelaunchMinipools, mp)
			} else {
				t.logger.Info(fmt.Sprintf("Minipool %s has %s left until it can be staked.", mpCommon.Address.Hex(), remainingTime))
			}
		}
	}

	// Return
	return prelaunchMinipools, nil
}

// Get submission info for staking a minipool
func (t *StakeMinipoolsTask) createStakeMinipoolTx(snapshot *NetworkSnapshot, mp minipool.IMinipool) (*eth.TransactionSubmission, error) {
	mpCommon := mp.Common()

	// Log
	t.logger.Info(
		"Preparing to stake minipool...",
		slog.String("minipool", mpCommon.Address.Hex()),
	)

	// Get minipool withdrawal credentials
	withdrawalCredentials := mpCommon.WithdrawalCredentials.Get()

	// Get the validator key for the minipool
	validatorPubkey := mpCommon.Pubkey.Get()
	validatorKey, err := t.w.LoadValidatorKey(validatorPubkey)
	if err != nil {
		return nil, err
	}

	// Get validator deposit data
	stakeValueWei := snapshot.RocketPoolNetworkSettings.MinipoolStakeValue
	stakeValueGwei := new(big.Int).Div(stakeValueWei, oneGwei).Uint64()
	depositData, err := validator.GetDepositData(t.logger, validatorKey, withdrawalCredentials, t.res.GenesisForkVersion, stakeValueGwei, t.res.EthNetworkName)
	if err != nil {
		return nil, err
	}

	// Get the tx info
	signature := beacon.ValidatorSignature(depositData.Signature)
	depositDataRoot := common.BytesToHash(depositData.DepositDataRoot)
	txInfo, err := t.csMgr.SuperNodeAccount.Stake(signature, depositDataRoot, mpCommon.Address, t.opts)
	if err != nil {
		return nil, fmt.Errorf("error estimating the gas required to stake the minipool: %w", err)
	}
	if txInfo.SimulationResult.SimulationError != "" {
		return nil, fmt.Errorf("simulating stake minipool tx for %s failed: %s", mpCommon.Address.Hex(), txInfo.SimulationResult.SimulationError)
	}

	submission, err := eth.CreateTxSubmissionFromInfo(txInfo, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating stake tx submission for minipool %s: %w", mpCommon.Address.Hex(), err)
	}
	return submission, nil
}

// Stake all available minipools
func (t *StakeMinipoolsTask) stakeMinipools(snapshot *NetworkSnapshot, submissions []*eth.TransactionSubmission, minipools []minipool.IMinipool) (bool, error) {
	// Get the max fee
	maxFee := t.maxFee
	if maxFee == nil || maxFee.Uint64() == 0 {
		var err error
		maxFee, err = gas.GetMaxFeeWeiForDaemon(t.logger)
		if err != nil {
			return false, err
		}
	}
	opts := &bind.TransactOpts{
		From:      t.opts.From,
		Value:     nil,
		Nonce:     nil,
		Signer:    t.opts.Signer,
		GasFeeCap: maxFee,
		GasTipCap: t.maxPriorityFee,
		Context:   t.ctx,
	}
	opts.GasFeeCap = maxFee
	opts.GasTipCap = t.maxPriorityFee

	// Print the gas info
	launchTimeout := snapshot.RocketPoolNetworkSettings.LaunchTimeout
	forceSubmissions := []*eth.TransactionSubmission{}
	if !gas.PrintAndCheckGasInfoForBatch(submissions, true, t.gasThreshold, t.logger, maxFee) {
		// Check for the timeout buffers
		for i, mp := range minipools {
			mpCommon := mp.Common()
			prelaunchTime := mpCommon.StatusTime.Formatted()
			isDue, timeUntilDue := isTransactionDue(prelaunchTime, launchTimeout)
			if !isDue {
				t.logger.Info(fmt.Sprintf("Time until staking minipool %s will be forced for safety: %s", mpCommon.Address.Hex(), timeUntilDue))
				continue
			}
			t.logger.Warn(
				"NOTICE: Minipool has exceeded half of the timeout period, so it will be force-staked at the current gas price.",
				slog.String("minipool", mpCommon.Address.Hex()),
			)
			forceSubmissions = append(forceSubmissions, submissions[i])
		}

		if len(forceSubmissions) == 0 {
			return false, nil
		}
		submissions = forceSubmissions
	}

	// Print TX info and wait for them to be included in a block
	txMgr := t.sp.GetTransactionManager()
	err := tx.PrintAndWaitForTransactionBatch(t.res.NetworkResources, txMgr, t.logger, submissions, opts)
	if err != nil {
		return false, err
	}

	// Log
	t.logger.Info("Successfully staked all minipools.")
	return true, nil
}
