package csnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/url"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/mux"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	"github.com/nodeset-org/hyperdrive-constellation/common/contracts/constellation"
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/node-manager-core/wallet"
	"github.com/rocket-pool/rocketpool-go/v2/deposit"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
	"github.com/rocket-pool/rocketpool-go/v2/network"
	"github.com/rocket-pool/rocketpool-go/v2/node"
	"github.com/rocket-pool/rocketpool-go/v2/tokens"
	rptypes "github.com/rocket-pool/rocketpool-go/v2/types"
)

const (
	minipoolDetailsBatchSize int = 100
)

// ===============
// === Factory ===
// ===============

type networkStatsContextFactory struct {
	handler *NetworkHandler
}

func (f *networkStatsContextFactory) Create(args url.Values) (*NetworkStatsContext, error) {
	c := &NetworkStatsContext{
		ServiceProvider: f.handler.serviceProvider,
		Logger:          f.handler.logger.Logger,
		Context:         f.handler.ctx,
	}
	inputErrs := []error{}
	return c, errors.Join(inputErrs...)
}

func (f *networkStatsContextFactory) RegisterRoute(router *mux.Router) {
	server.RegisterSingleStageRoute[*NetworkStatsContext, csapi.NetworkStatsData](
		router, "stats", f, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type NetworkStatsContext struct {
	// Dependencies
	ServiceProvider cscommon.IConstellationServiceProvider
	Logger          *slog.Logger
	Context         context.Context

	// Services
	ec                 eth.IExecutionClient
	depositPool        *deposit.DepositPoolManager
	rpMgr              *cscommon.RocketPoolManager
	csMgr              *cscommon.ConstellationManager
	rpSuperNodeBinding *node.Node
	mpMgr              *minipool.MinipoolManager
	networkMgr         *network.NetworkManager
	rpl                *tokens.TokenRpl

	// On-chain vars
	odRplBalance  *big.Int
	maxValidators *big.Int
}

func (c *NetworkStatsContext) Initialize(walletStatus wallet.WalletStatus) (types.ResponseStatus, error) {
	sp := c.ServiceProvider
	ctx := c.Context
	c.rpMgr = sp.GetRocketPoolManager()
	c.csMgr = sp.GetConstellationManager()
	c.ec = sp.GetEthClient()

	// Requirements
	err := sp.RequireEthClientSynced(ctx)
	if err != nil {
		if errors.Is(err, services.ErrExecutionClientNotSynced) {
			return types.ResponseStatus_ClientsNotSynced, err
		}
		return types.ResponseStatus_Error, err
	}

	// Refresh RP
	err = c.rpMgr.RefreshRocketPoolContracts()
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error refreshing Rocket Pool contracts: %w", err)
	}

	// Refresh constellation contracts
	err = c.csMgr.LoadContracts()
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error loading Constellation contracts: %w", err)
	}

	// Create the bindings
	rp := c.rpMgr.RocketPool
	superNodeAddress := c.csMgr.SuperNodeAccount.Address
	c.rpSuperNodeBinding, err = node.NewNode(c.rpMgr.RocketPool, superNodeAddress)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating node %s binding: %w", superNodeAddress.Hex(), err)
	}
	c.depositPool, err = deposit.NewDepositPoolManager(rp)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting deposit pool manager binding: %w", err)
	}
	c.mpMgr, err = minipool.NewMinipoolManager(rp)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting minipool manager binding: %w", err)
	}
	c.networkMgr, err = network.NewNetworkManager(rp)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating network prices binding: %w", err)
	}
	c.rpl, err = tokens.NewTokenRpl(rp)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating RPL token binding: %w", err)
	}
	return types.ResponseStatus_Success, nil
}

func (c *NetworkStatsContext) GetState(mc *batch.MultiCaller) {
	eth.AddQueryablesToMulticall(mc,
		c.depositPool.Balance,
		c.mpMgr.TotalQueueLength,
		c.mpMgr.TotalQueueCapacity,
		c.networkMgr.EthUtilizationRate,
		c.networkMgr.RplPrice,
		c.rpSuperNodeBinding.RplStake,
		c.rpSuperNodeBinding.MinipoolCount,
	)
	c.rpl.BalanceOf(mc, &c.odRplBalance, c.csMgr.OperatorDistributor.Address)
	c.csMgr.SuperNodeAccount.GetMaxValidators(mc, &c.maxValidators)
}

func (c *NetworkStatsContext) PrepareData(data *csapi.NetworkStatsData, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	qMgr := c.ServiceProvider.GetQueryManager()

	// Populate initial fields
	data.SuperNodeAddress = c.csMgr.SuperNodeAccount.Address
	data.SuperNodeRplStake = c.rpSuperNodeBinding.RplStake.Get()
	data.ConstellationRplBalance = c.odRplBalance
	data.RocketPoolEthBalance = c.depositPool.Balance.Get()
	data.MinipoolQueueLength = int(c.mpMgr.TotalQueueLength.Formatted())
	data.MinipoolQueueCapacity = c.mpMgr.TotalQueueCapacity.Get()
	data.RplPrice = c.networkMgr.RplPrice.Raw()
	data.RocketPoolEthUtilizationRate = c.networkMgr.EthUtilizationRate.Raw()
	data.ValidatorLimit = int(c.maxValidators.Uint64())

	// Get the OD balance
	odEthBalance, err := c.ec.BalanceAt(c.Context, c.csMgr.OperatorDistributor.Address, nil)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting Constellation's available ETH: %w", err)
	}
	data.ConstellationEthBalance = odEthBalance

	// Get all of the CS minipools
	addresses, err := c.rpSuperNodeBinding.GetMinipoolAddresses(c.rpSuperNodeBinding.MinipoolCount.Formatted(), nil)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting minipool addresses: %w", err)
	}
	mps, err := c.mpMgr.CreateMinipoolsFromAddresses(addresses, false, nil)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating minipool bindings: %w", err)
	}

	// Get MP details
	csDetails := make([]constellation.MinipoolData, len(mps))
	err = qMgr.BatchQuery(len(mps), minipoolDetailsBatchSize, func(mc *batch.MultiCaller, i int) error {
		mp := mps[i]
		mpCommon := mp.Common()
		eth.AddQueryablesToMulticall(mc,
			mpCommon.Status,
			mpCommon.IsFinalised,
		)

		// Make the CS binding
		c.csMgr.SuperNodeAccount.GetMinipoolData(mc, &csDetails[i], mpCommon.Address)
		return nil
	}, nil)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting minipool details: %w", err)
	}

	// Get the unique node count
	nodes := map[common.Address]bool{}
	for _, mp := range csDetails {
		nodes[mp.NodeAddress] = true
	}
	data.SubnodeCount = len(nodes)

	// Get the minipool status counts
	for _, mp := range mps {
		mpCommon := mp.Common()
		if mpCommon.IsFinalised.Get() {
			data.FinalizedMinipoolCount++
			continue
		}

		switch mpCommon.Status.Formatted() {
		case rptypes.MinipoolStatus_Initialized:
			data.InitializedMinipoolCount++
		case rptypes.MinipoolStatus_Prelaunch:
			data.PrelaunchMinipoolCount++
		case rptypes.MinipoolStatus_Staking:
			data.StakingMinipoolCount++
		case rptypes.MinipoolStatus_Dissolved:
			data.DissolvedMinipoolCount++
		}
	}
	data.ActiveMinipoolCount = len(mps) - data.FinalizedMinipoolCount
	return types.ResponseStatus_Success, nil
}
