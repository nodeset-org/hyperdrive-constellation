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
	"github.com/nodeset-org/hyperdrive-constellation/shared/keys"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/node-manager-core/log"
	"github.com/rocket-pool/node-manager-core/wallet"
	"github.com/rocket-pool/rocketpool-go/v2/dao/oracle"
	"github.com/rocket-pool/rocketpool-go/v2/dao/protocol"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
)

const (
	minipoolDetailsBatchSize int = 100
)

type ConstellationNodeSnapshot struct {
	NodeAddress common.Address
	Minipools   []minipool.IMinipool
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
		csMgr:  sp.GetConstellationManager(),
		rpMgr:  sp.GetRocketPoolManager(),
		ec:     sp.GetEthClient(),
	}
}

// Run the task
func (t *NetworkSnapshotTask) Run(walletStatus *wallet.WalletStatus) (*NetworkSnapshot, error) {
	// Log
	t.logger.Info("Creating network snapshot...")

	// Refresh contract managers
	err := t.refreshContracts()
	if err != nil {
		return nil, err
	}

	// Get the network snapshot
	snapshot, err := t.createNetworkSnapshot(walletStatus.Wallet.WalletAddress)
	if err != nil {
		return nil, fmt.Errorf("error creating network snapshot: %w", err)
	}

	// Log
	t.logger.Info("Network snapshot created",
		slog.String("block", snapshot.ExecutionBlockHeader.Number.String()),
	)
	return snapshot, nil
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
	var minipoolAddresses []common.Address
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		t.csMgr.SuperNodeAccount.GetSubNodeMinipools(mc, &minipoolAddresses, nodeAddress)
		return nil
	}, callOpts,
		odaoMgr.Settings.Minipool.ScrubPeriod,
		pdaoMgr.Settings.Minipool.LaunchTimeout,
		mpMgr.StakeValue,
	)
	if err != nil {
		return nil, fmt.Errorf("error getting contract state: %w", err)
	}

	// Create each minipool binding
	mps, err := mpMgr.CreateMinipoolsFromAddresses(minipoolAddresses, false, callOpts)
	if err != nil {
		return nil, fmt.Errorf("error creating minipool bindings: %w", err)
	}

	// Get the minipool details
	err = rp.BatchQuery(len(minipoolAddresses), minipoolDetailsBatchSize, func(mc *batch.MultiCaller, i int) error {
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

	// Create the network snapshot
	snapshot := &NetworkSnapshot{
		ExecutionBlockHeader: header,
		RocketPoolNetworkSettings: &RocketPoolNetworkSettings{
			ScrubPeriod:        odaoMgr.Settings.Minipool.ScrubPeriod.Formatted(),
			LaunchTimeout:      pdaoMgr.Settings.Minipool.LaunchTimeout.Formatted(),
			MinipoolStakeValue: mpMgr.StakeValue.Get(),
		},
		ConstellationNode: &ConstellationNodeSnapshot{
			NodeAddress: nodeAddress,
			Minipools:   mps,
		},
	}

	// Return
	return snapshot, nil
}
