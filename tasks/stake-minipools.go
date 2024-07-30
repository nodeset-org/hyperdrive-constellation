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
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/beacon"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/node-manager-core/log"
	"github.com/rocket-pool/node-manager-core/node/validator"
	"github.com/rocket-pool/node-manager-core/wallet"
	"github.com/rocket-pool/rocketpool-go/v2/dao/oracle"
	"github.com/rocket-pool/rocketpool-go/v2/dao/protocol"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
	"github.com/rocket-pool/rocketpool-go/v2/rocketpool"
	rptypes "github.com/rocket-pool/rocketpool-go/v2/types"
)

const (
	minipoolAddressQueryBatchSize int = 1000
	minipoolDetailsBatchSize      int = 100
)

var (
	oneGwei *big.Int = big.NewInt(1e9)
)

// Stake minipools task
type StakeMinipoolsTask struct {
	sp             cscommon.IConstellationServiceProvider
	logger         *slog.Logger
	ctx            context.Context
	cfg            *csconfig.ConstellationConfig
	res            *csconfig.MergedResources
	w              *cscommon.Wallet
	csMgr          *cscommon.ConstellationManager
	rpMgr          *cscommon.RocketPoolManager
	rp             *rocketpool.RocketPool
	bc             beacon.IBeaconClient
	opts           *bind.TransactOpts
	gasThreshold   float64
	maxFee         *big.Int
	maxPriorityFee *big.Int
	launchTimeout  time.Duration
	stakeValueGwei uint64
}

// Create a stake minipools task
func NewStakeMinipoolsTask(ctx context.Context, sp cscommon.IConstellationServiceProvider, logger *log.Logger) *StakeMinipoolsTask {
	hdCfg := sp.GetHyperdriveConfig()
	log := logger.With(slog.String(keys.TaskKey, "Minipool Stake"))
	maxFee, maxPriorityFee := tx.GetAutoTxInfo(hdCfg, log)
	return &StakeMinipoolsTask{
		ctx:            ctx,
		sp:             sp,
		logger:         log,
		cfg:            sp.GetConfig(),
		res:            sp.GetResources(),
		w:              sp.GetWallet(),
		csMgr:          sp.GetConstellationManager(),
		rpMgr:          sp.GetRocketPoolManager(),
		bc:             sp.GetBeaconClient(),
		gasThreshold:   hdCfg.AutoTxGasThreshold.Value,
		maxFee:         maxFee,
		maxPriorityFee: maxPriorityFee,
	}
}

// Stake prelaunch minipools
func (t *StakeMinipoolsTask) Run(walletStatus *wallet.WalletStatus) error {
	// Log
	t.logger.Info("Starting check for minipools to launch.")

	// Refresh RP
	err := t.rpMgr.RefreshRocketPoolContracts()
	if err != nil {
		return fmt.Errorf("error refreshing Rocket Pool contracts: %w", err)
	}
	t.rp = t.rpMgr.RocketPool

	// Refresh Constellation
	err = t.csMgr.LoadContracts()
	if err != nil {
		return fmt.Errorf("error loading Constellation contracts: %w", err)
	}

	// Get transactor
	walletAddress := walletStatus.Wallet.WalletAddress
	t.opts = t.sp.GetSigner().GetTransactor(walletAddress)

	// Get prelaunch minipools
	minipools, err := t.getPrelaunchMinipools(walletAddress)
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
		txSubmissions[i], err = t.createStakeMinipoolTx(mp, walletAddress)
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
	_, err = t.stakeMinipools(txSubmissions, minipools)
	if err != nil {
		return fmt.Errorf("error staking minipools: %w", err)
	}

	// Return
	return nil
}

// Get prelaunch minipools
func (t *StakeMinipoolsTask) getPrelaunchMinipools(walletAddress common.Address) ([]minipool.IMinipool, error) {
	// Make some bindings
	var minipoolCount *big.Int
	qMgr := t.sp.GetQueryManager()
	pdaoMgr, err := protocol.NewProtocolDaoManager(t.rp)
	if err != nil {
		return nil, fmt.Errorf("error creating pDAO manager binding: %w", err)
	}
	odaoMgr, err := oracle.NewOracleDaoManager(t.rp)
	if err != nil {
		return nil, fmt.Errorf("error creating pDAO manager binding: %w", err)
	}
	mpMgr, err := minipool.NewMinipoolManager(t.rp)
	if err != nil {
		return nil, fmt.Errorf("error creating minipool manager binding: %w", err)
	}

	// Get the time of the target header
	header, err := t.rp.Client.HeaderByNumber(t.ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("error getting the latest block time: %w", err)
	}
	blockTime := time.Unix(int64(header.Time), 0)
	callOpts := &bind.CallOpts{
		BlockNumber: header.Number,
	}

	// Run a query
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		t.csMgr.Whitelist.GetNumberOfValidators(mc, &minipoolCount, walletAddress)
		return nil
	}, callOpts,
		odaoMgr.Settings.Minipool.ScrubPeriod,
		pdaoMgr.Settings.Minipool.LaunchTimeout,
		mpMgr.StakeValue,
	)
	if err != nil {
		return nil, fmt.Errorf("error getting contract state: %w", err)
	}

	// Prep data
	scrubPeriod := odaoMgr.Settings.Minipool.ScrubPeriod.Formatted()
	t.launchTimeout = pdaoMgr.Settings.Minipool.LaunchTimeout.Formatted()
	stakeValueWei := mpMgr.StakeValue.Get()
	stakeValueGwei := new(big.Int).Div(stakeValueWei, oneGwei)
	t.stakeValueGwei = stakeValueGwei.Uint64()

	// Get the minipool addresses
	count := int(minipoolCount.Int64())
	addresses := make([]common.Address, count)
	err = qMgr.BatchQuery(count, minipoolAddressQueryBatchSize, func(mc *batch.MultiCaller, i int) error {
		t.csMgr.SuperNodeAccount.GetSubNodeMinipoolAt(mc, &addresses[i], walletAddress, big.NewInt(int64(i)))
		return nil
	}, callOpts)
	if err != nil {
		return nil, fmt.Errorf("error getting minipool addresses for node [%s]: %w", walletAddress.Hex(), err)
	}

	// Create each minipool binding
	mps, err := mpMgr.CreateMinipoolsFromAddresses(addresses, false, callOpts)
	if err != nil {
		return nil, fmt.Errorf("error creating minipool bindings: %w", err)
	}

	// Get the minipool details
	err = t.rp.BatchQuery(len(addresses), minipoolDetailsBatchSize, func(mc *batch.MultiCaller, i int) error {
		mp := mps[i]
		mpCommon := mp.Common()
		eth.AddQueryablesToMulticall(mc,
			mpCommon.Exists,
			mpCommon.Status,
			mpCommon.StatusTime,
			mpCommon.NodeAddress,
			mpCommon.Pubkey,
			mpCommon.WithdrawalCredentials,
		)
		return nil
	}, callOpts)
	if err != nil {
		return nil, fmt.Errorf("error getting minipool details: %w", err)
	}

	// Filter minipools by status
	prelaunchMinipools := []minipool.IMinipool{}
	for _, mp := range mps {
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
func (t *StakeMinipoolsTask) createStakeMinipoolTx(mp minipool.IMinipool, walletAddress common.Address) (*eth.TransactionSubmission, error) {
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
	depositData, err := validator.GetDepositData(validatorKey, withdrawalCredentials, t.res.GenesisForkVersion, t.stakeValueGwei, t.res.EthNetworkName)
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
func (t *StakeMinipoolsTask) stakeMinipools(submissions []*eth.TransactionSubmission, minipools []minipool.IMinipool) (bool, error) {
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
	forceSubmissions := []*eth.TransactionSubmission{}
	forceMinipools := []minipool.IMinipool{}
	if !gas.PrintAndCheckGasInfoForBatch(submissions, true, t.gasThreshold, t.logger, maxFee) {
		// Check for the timeout buffers
		for i, mp := range minipools {
			mpCommon := mp.Common()
			prelaunchTime := mpCommon.StatusTime.Formatted()
			isDue, timeUntilDue := isTransactionDue(prelaunchTime, t.launchTimeout)
			if !isDue {
				t.logger.Info(fmt.Sprintf("Time until staking minipool %s will be forced for safety: %s", mpCommon.Address.Hex(), timeUntilDue))
				continue
			}
			t.logger.Warn(
				"NOTICE: Minipool has exceeded half of the timeout period, so it will be force-staked at the current gas price.",
				slog.String("minipool", mpCommon.Address.Hex()),
			)
			forceSubmissions = append(forceSubmissions, submissions[i])
			forceMinipools = append(forceMinipools, mp)
		}

		if len(forceSubmissions) == 0 {
			return false, nil
		}
		submissions = forceSubmissions
		minipools = forceMinipools
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
