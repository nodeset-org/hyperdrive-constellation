package cstasks

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	"github.com/nodeset-org/hyperdrive-constellation/shared/keys"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/beacon"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/node-manager-core/log"
	"github.com/rocket-pool/node-manager-core/wallet"
	"github.com/rocket-pool/rocketpool-go/v2/dao/oracle"
	"github.com/rocket-pool/rocketpool-go/v2/dao/protocol"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
	rptypes "github.com/rocket-pool/rocketpool-go/v2/types"
)

type MinipoolDetails struct {
	MinipoolAddress       common.Address
	Status                rptypes.MinipoolStatus
	StatusTime            time.Time
	Pubkey                beacon.ValidatorPubkey
	WithdrawalCredentials common.Hash
	ValidatorStatus       *beacon.ValidatorStatus
}

type ConstellationNodeSnapshot struct {
	NodeAddress  common.Address
	IsRegistered bool
	Minipools    []*MinipoolDetails
}

type RocketPoolNetworkSettings struct {
	ScrubPeriod        time.Duration
	LaunchTimeout      time.Duration
	MinipoolStakeValue *big.Int
}

type NetworkSnapshot struct {
	ExecutionBlockHeader      *types.Header
	RocketPoolNetworkSettings *RocketPoolNetworkSettings
	ConstellationNode         *ConstellationNodeSnapshot
}

type NetworkSnapshotTask struct {
	sp     cscommon.IConstellationServiceProvider
	logger *slog.Logger
	ctx    context.Context
	cfg    *csconfig.ConstellationConfig
	res    *csconfig.MergedResources
	csMgr  *cscommon.ConstellationManager
	rpMgr  *cscommon.RocketPoolManager
	ec     eth.IExecutionClient
}

// Creates a new network snapshot task
func NewNetworkSnapshotTask(ctx context.Context, sp cscommon.IConstellationServiceProvider, logger *log.Logger) *NetworkSnapshotTask {
	log := logger.With(slog.String(keys.TaskKey, "Network Snapshot"))
	return &NetworkSnapshotTask{
		ctx:    ctx,
		sp:     sp,
		logger: log,
		cfg:    sp.GetConfig(),
		res:    sp.GetResources(),
		csMgr:  sp.GetConstellationManager(),
		rpMgr:  sp.GetRocketPoolManager(),
		ec:     sp.GetEthClient(),
	}
}

// Run the task
func (t *NetworkSnapshotTask) Run(walletStatus *wallet.WalletStatus) error {
	// Log
	t.logger.Info("Creating network snapshot...")

	// Refresh contract managers
	err := t.refreshContracts()
	if err != nil {
		return err
	}

	// Get the network snapshot
	/*
		snapshot, err := t.createNetworkSnapshot()
		if err != nil {
			log.Error("Error getting network snapshot", slog.Error(err))
			return err
		}

		// Log
		log.Info("Network Snapshot", slog.Any("Snapshot", snapshot))
	*/

	// Return
	return nil
}

// Refresh the contract managers
func (t *NetworkSnapshotTask) refreshContracts() error {
	// Refresh RP
	err := t.rpMgr.RefreshRocketPoolContracts()
	if err != nil {
		return fmt.Errorf("error refreshing Rocket Pool contracts: %w", err)
	}

	// Refresh Constellation
	err = t.csMgr.LoadContracts()
	if err != nil {
		return fmt.Errorf("error refreshing Constellation contracts: %w", err)
	}
	return nil
}

// Get the network snapshot
func (t *NetworkSnapshotTask) createNetworkSnapshot(nodeAddress common.Address) (*NetworkSnapshot, error) {
	// Make some bindings
	rp := t.rpMgr.RocketPool
	var minipoolCount *big.Int
	qMgr := t.sp.GetQueryManager()
	pdaoMgr, err := protocol.NewProtocolDaoManager(rp)
	if err != nil {
		return nil, fmt.Errorf("error creating pDAO manager binding: %w", err)
	}
	odaoMgr, err := oracle.NewOracleDaoManager(rp)
	if err != nil {
		return nil, fmt.Errorf("error creating pDAO manager binding: %w", err)
	}
	mpMgr, err := minipool.NewMinipoolManager(rp)
	if err != nil {
		return nil, fmt.Errorf("error creating minipool manager binding: %w", err)
	}

	// Get the latest header
	header, err := t.ec.HeaderByNumber(t.ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("error getting the latest block header: %w", err)
	}
	callOpts := &bind.CallOpts{
		BlockNumber: header.Number,
	}

	// Run a query
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		t.csMgr.Whitelist.GetActiveValidatorCountForOperator(mc, &minipoolCount, nodeAddress)
		return nil
	}, callOpts,
		odaoMgr.Settings.Minipool.ScrubPeriod,
		pdaoMgr.Settings.Minipool.LaunchTimeout,
		mpMgr.StakeValue,
	)
	if err != nil {
		return nil, fmt.Errorf("error getting contract state: %w", err)
	}

	// Get the minipool addresses
	count := int(minipoolCount.Int64())
	addresses := make([]common.Address, count)
	err = qMgr.BatchQuery(count, minipoolAddressQueryBatchSize, func(mc *batch.MultiCaller, i int) error {
		t.csMgr.SuperNodeAccount.GetSubNodeMinipoolAt(mc, &addresses[i], nodeAddress, big.NewInt(int64(i)))
		return nil
	}, callOpts)
	if err != nil {
		return nil, fmt.Errorf("error getting minipool addresses for node [%s]: %w", nodeAddress.Hex(), err)
	}

	// Make the RP network settings
	rpNetworkSettings := &RocketPoolNetworkSettings{
		ScrubPeriod:        odaoMgr.Settings.Minipool.ScrubPeriod.Formatted(),
		LaunchTimeout:      pdaoMgr.Settings.Minipool.LaunchTimeout.Formatted(),
		MinipoolStakeValue: mpMgr.StakeValue.Get(),
	}

	// Create the network snapshot
	snapshot := &NetworkSnapshot{
		RocketPoolNetworkSettings: rpNetworkSettings,
		//ConstellationNode:         nodeSnapshot,
	}

	// Return
	return snapshot, nil
}
