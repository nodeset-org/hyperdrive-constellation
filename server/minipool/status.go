package csminipool

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"net/url"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/gorilla/mux"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/node-manager-core/wallet"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
	"github.com/rocket-pool/rocketpool-go/v2/node"
	snminipool "github.com/rocket-pool/smartnode/v2/rocketpool-daemon/api/minipool"
	snservices "github.com/rocket-pool/smartnode/v2/rocketpool-daemon/common/services"
	snapi "github.com/rocket-pool/smartnode/v2/shared/types/api"
)

// ===============
// === Factory ===
// ===============

type minipoolStatusContextFactory struct {
	handler *MinipoolHandler
}

func (f *minipoolStatusContextFactory) Create(args url.Values) (*MinipoolStatusContext, error) {
	c := &MinipoolStatusContext{
		ServiceProvider:   f.handler.serviceProvider,
		Logger:            f.handler.logger.Logger,
		Context:           f.handler.ctx,
		SnServiceProvider: f.handler.snServiceProvider,
	}
	return c, nil
}

func (f *minipoolStatusContextFactory) RegisterRoute(router *mux.Router) {
	RegisterMinipoolRoute[*MinipoolStatusContext, csapi.MinipoolStatusData](
		router, "status", f, f.handler.ctx, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type MinipoolStatusContext struct {
	// Dependencies
	ServiceProvider   cscommon.IConstellationServiceProvider
	Logger            *slog.Logger
	Context           context.Context
	SnServiceProvider snservices.ISmartNodeServiceProvider

	snContext *snminipool.MinipoolStatusContext
	snData    *snapi.MinipoolStatusData

	// Data
	maxValidators *big.Int
}

func (c *MinipoolStatusContext) Initialize(walletStatus wallet.WalletStatus) (types.ResponseStatus, error) {
	// Create the SN context
	c.snContext = &snminipool.MinipoolStatusContext{
		ServiceProvider: c.SnServiceProvider,
		Logger:          c.Logger,
		Context:         c.Context,
	}

	// Create the data used by the SN context
	c.snData = &snapi.MinipoolStatusData{}

	return c.snContext.Initialize()
}

func (c *MinipoolStatusContext) GetState(node *node.Node, mc *batch.MultiCaller) {
	// Defer to the SN
	c.snContext.GetState(node, mc)
	csMgr := c.ServiceProvider.GetConstellationManager()
	csMgr.SuperNodeAccount.GetMaxValidators(mc, &c.maxValidators)
}

func (c *MinipoolStatusContext) CheckState(node *node.Node, data *csapi.MinipoolStatusData) bool {
	// Defer to the SN
	return c.snContext.CheckState(node, c.snData)
}

func (c *MinipoolStatusContext) GetMinipoolDetails(mc *batch.MultiCaller, mp minipool.IMinipool, index int) {
	// Defer to the SN
	c.snContext.GetMinipoolDetails(mc, mp, index)
}

func (c *MinipoolStatusContext) PrepareData(addresses []common.Address, mps []minipool.IMinipool, data *csapi.MinipoolStatusData, latestBlockHeader *ethtypes.Header, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	code, err := c.snContext.PrepareData(addresses, mps, c.snData)
	data.MaxValidatorsPerNode = c.maxValidators.Uint64()
	data.LatestDelegate = c.snData.LatestDelegate

	// Get the signed exit status from NodeSet
	hd := c.ServiceProvider.GetHyperdriveClient()
	response, err := hd.NodeSet_Constellation.GetValidators()
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting validator status from the nodeset server: %w", err)
	}

	// Add each minipool to the list
	data.Minipools = make([]csapi.MinipoolDetails, len(c.snData.Minipools))
	for i, mp := range c.snData.Minipools {
		newMp := csapi.MinipoolDetails{
			MinipoolDetails: &mp,
		}

		// Check the signed exit upload status
		for _, validator := range response.Data.Validators {
			if validator.Pubkey == mp.ValidatorPubkey {
				newMp.RequiresSignedExit = validator.RequiresExitMessage
			}
		}
		data.Minipools[i] = newMp
	}

	return code, err
}
