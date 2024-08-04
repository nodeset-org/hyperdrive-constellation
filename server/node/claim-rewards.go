package csnode

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/url"

	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	nmcserver "github.com/rocket-pool/node-manager-core/api/server"
	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/node-manager-core/utils/input"
	"github.com/rocket-pool/node-manager-core/wallet"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/gorilla/mux"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
)

// ===============
// === Factory ===
// ===============

type nodeClaimRewardsContextFactory struct {
	handler *NodeHandler
}

func (f *nodeClaimRewardsContextFactory) Create(args url.Values) (*nodeClaimRewardsContext, error) {
	c := &nodeClaimRewardsContext{
		ServiceProvider: f.handler.serviceProvider,
		Logger:          f.handler.logger.Logger,
		Context:         f.handler.ctx,
	}
	inputErrs := []error{
		nmcserver.ValidateArg("startInterval", args, input.ValidateBigInt, &c.StartInterval),
		nmcserver.ValidateArg("endInterval", args, input.ValidateBigInt, &c.EndInterval),
	}
	return c, errors.Join(inputErrs...)
}

func (f *nodeClaimRewardsContextFactory) RegisterRoute(router *mux.Router) {
	server.RegisterQuerylessGet[*nodeClaimRewardsContext, types.TxInfoData](
		router, "claim-rewards", f, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type nodeClaimRewardsContext struct {
	// Dependencies
	ServiceProvider cscommon.IConstellationServiceProvider
	Logger          *slog.Logger
	Context         context.Context

	// Arguments
	StartInterval *big.Int
	EndInterval   *big.Int
}

func (c *nodeClaimRewardsContext) PrepareData(data *types.TxInfoData, walletStatus wallet.WalletStatus, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	sp := c.ServiceProvider
	csMgr := sp.GetConstellationManager()
	ctx := c.Context

	// Requirements
	err := sp.RequireWalletReady(walletStatus)
	if err != nil {
		return types.ResponseStatus_WalletNotReady, err
	}
	err = sp.RequireEthClientSynced(ctx)
	if err != nil {
		if errors.Is(err, services.ErrExecutionClientNotSynced) {
			return types.ResponseStatus_ClientsNotSynced, err
		}
		return types.ResponseStatus_Error, err
	}

	// Load the Constellation contracts
	err = csMgr.LoadContracts()
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error loading Constellation contracts: %w", err)
	}

	// Get the claim TX
	data.TxInfo, err = csMgr.YieldDistributor.Harvest(walletStatus.Wallet.WalletAddress, c.StartInterval, c.EndInterval, opts)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating claim-rewards TX: %w", err)
	}
	return types.ResponseStatus_Success, nil
}
