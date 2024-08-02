package csminipool

import (
	"context"
	"errors"
	"log/slog"
	"net/url"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/gorilla/mux"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	batch "github.com/rocket-pool/batch-query"
	nmcserver "github.com/rocket-pool/node-manager-core/api/server"
	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/node-manager-core/utils/input"
	"github.com/rocket-pool/node-manager-core/wallet"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
	"github.com/rocket-pool/rocketpool-go/v2/node"
	rptypes "github.com/rocket-pool/rocketpool-go/v2/types"
)

// ===============
// === Factory ===
// ===============

type minipoolExitDetailsContextFactory struct {
	handler *MinipoolHandler
}

func (f *minipoolExitDetailsContextFactory) Create(args url.Values) (*MinipoolExitDetailsContext, error) {
	c := &MinipoolExitDetailsContext{
		ServiceProvider: f.handler.serviceProvider,
		Logger:          f.handler.logger.Logger,
		Context:         f.handler.ctx,
	}
	inputErrs := []error{
		nmcserver.ValidateArg("verbose", args, input.ValidateBool, &c.Verbose),
	}
	return c, errors.Join(inputErrs...)
}

func (f *minipoolExitDetailsContextFactory) RegisterRoute(router *mux.Router) {
	RegisterMinipoolRoute[*MinipoolExitDetailsContext, csapi.MinipoolExitDetailsData](
		router, "exit/details", f, f.handler.ctx, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type MinipoolExitDetailsContext struct {
	// Dependencies
	ServiceProvider cscommon.IConstellationServiceProvider
	Logger          *slog.Logger
	Context         context.Context

	// Arguments
	Verbose bool
}

func (c *MinipoolExitDetailsContext) Initialize(walletStatus wallet.WalletStatus) (types.ResponseStatus, error) {
	return types.ResponseStatus_Success, nil
}

func (c *MinipoolExitDetailsContext) GetState(node *node.Node, mc *batch.MultiCaller) {
}

func (c *MinipoolExitDetailsContext) CheckState(node *node.Node, response *csapi.MinipoolExitDetailsData) bool {
	return true
}

func (c *MinipoolExitDetailsContext) GetMinipoolDetails(mc *batch.MultiCaller, mp minipool.IMinipool, index int) {
	mpCommon := mp.Common()
	eth.AddQueryablesToMulticall(mc,
		mpCommon.Status,
		mpCommon.Pubkey,
	)
}

func (c *MinipoolExitDetailsContext) PrepareData(addresses []common.Address, mps []minipool.IMinipool, data *csapi.MinipoolExitDetailsData, blockHeader *ethtypes.Header, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	// Get the exit details
	details := make([]csapi.MinipoolExitDetails, len(addresses))
	for i, mp := range mps {
		mpCommonDetails := mp.Common()
		status := mpCommonDetails.Status.Formatted()
		mpDetails := csapi.MinipoolExitDetails{
			Address:       mpCommonDetails.Address,
			InvalidStatus: (status != rptypes.MinipoolStatus_Staking),
		}
		mpDetails.CanExit = !mpDetails.InvalidStatus
		details[i] = mpDetails
	}

	data.Details = details
	return types.ResponseStatus_Success, nil
}
