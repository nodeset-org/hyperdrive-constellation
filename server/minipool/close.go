package csminipool

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/mux"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	hdserver "github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
	hdservices "github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/api/server"
	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/node-manager-core/utils/input"
	"github.com/rocket-pool/node-manager-core/wallet"
	rpminipool "github.com/rocket-pool/rocketpool-go/v2/minipool"
	rptypes "github.com/rocket-pool/rocketpool-go/v2/types"
)

// ===============
// === Factory ===
// ===============

type minipoolCloseContextFactory struct {
	handler *MinipoolHandler
}

func (f *minipoolCloseContextFactory) Create(args url.Values) (*MinipoolCloseContext, error) {
	c := &MinipoolCloseContext{
		Handler: f.handler,
	}
	inputErrs := []error{
		server.ValidateArgBatch("addresses", args, minipoolDetailsBatchSize, input.ValidateAddress, &c.MinipoolAddresses),
	}
	return c, errors.Join(inputErrs...)
}

func (f *minipoolCloseContextFactory) RegisterRoute(router *mux.Router) {
	hdserver.RegisterSingleStageRoute[*MinipoolCloseContext, types.BatchTxInfoData](
		router, "close", f, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type MinipoolCloseContext struct {
	Handler           *MinipoolHandler
	MinipoolAddresses []common.Address

	nodeAddress  common.Address
	mps          []rpminipool.IMinipool
	csMgr        *cscommon.ConstellationManager
	mpOwnerFlags []bool
}

func (c *MinipoolCloseContext) Initialize(walletStatus wallet.WalletStatus) (types.ResponseStatus, error) {
	sp := c.Handler.serviceProvider
	rpMgr := sp.GetRocketPoolManager()
	ctx := c.Handler.ctx

	// Requirements
	err := sp.RequireRegisteredWithConstellation(ctx, walletStatus, false)
	if err != nil {
		if errors.Is(err, hdservices.ErrNodeAddressNotSet) {
			return types.ResponseStatus_AddressNotPresent, nil
		}
		if errors.Is(err, hdservices.ErrExecutionClientNotSynced) {
			return types.ResponseStatus_ClientsNotSynced, nil
		}
		if errors.Is(err, cscommon.ErrNotRegisteredWithConstellation) {
			return types.ResponseStatus_InvalidChainState, nil
		}
		return types.ResponseStatus_Error, err
	}

	// Refresh RP
	err = rpMgr.RefreshRocketPoolContracts()
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error refreshing Rocket Pool contracts: %w", err)
	}
	rp := rpMgr.RocketPool

	// Create minipool bindings
	mpMgr, err := rpminipool.NewMinipoolManager(rp)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating minipool manager binding: %w", err)
	}
	c.mps, err = mpMgr.CreateMinipoolsFromAddresses(c.MinipoolAddresses, false, nil)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating minipool bindings: %w", err)
	}
	c.mpOwnerFlags = make([]bool, len(c.mps))

	// Get the other params
	c.csMgr = sp.GetConstellationManager()
	c.nodeAddress = walletStatus.Address.NodeAddress
	return types.ResponseStatus_Success, nil
}

func (c *MinipoolCloseContext) GetState(mc *batch.MultiCaller) {
	for i, mp := range c.mps {
		// Get some basic minipool details
		mpCommon := mp.Common()
		eth.AddQueryablesToMulticall(mc,
			mpCommon.NodeAddress,
			mpCommon.Status,
			mpCommon.IsFinalised,
		)

		// Check if the node operator owns the minipool
		c.csMgr.SuperNodeAccount.SubNodeOperatorHasMinipool(mc, &c.mpOwnerFlags[i], c.nodeAddress, mpCommon.Address)
	}
}

func (c *MinipoolCloseContext) PrepareData(data *types.BatchTxInfoData, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	// Validation
	supernodeAddress := c.csMgr.SuperNodeAccount.Address
	for i, mp := range c.mps {
		mpCommon := mp.Common()
		if mpCommon.NodeAddress.Get() != supernodeAddress {
			return types.ResponseStatus_InvalidChainState, fmt.Errorf("minipool %s does not belong to the Constellation supernode %s", mpCommon.Address.Hex(), supernodeAddress.Hex())
		}
		if mpCommon.IsFinalised.Get() {
			return types.ResponseStatus_InvalidChainState, fmt.Errorf("minipool %s is already finalized", mpCommon.Address.Hex())
		}
		if mpCommon.Status.Formatted() != rptypes.MinipoolStatus_Dissolved {
			return types.ResponseStatus_InvalidChainState, fmt.Errorf("minipool %s is not dissolved", mpCommon.Address.Hex())
		}
		if !c.mpOwnerFlags[i] {
			return types.ResponseStatus_InvalidChainState, fmt.Errorf("node [%s] does not own minipool %s", c.nodeAddress.Hex(), mpCommon.Address.Hex())
		}
	}

	// TX Generation
	supernode := c.csMgr.SuperNodeAccount
	for _, mp := range c.mps {
		mpCommon := mp.Common()
		txInfo, err := supernode.Close(c.nodeAddress, mpCommon.Address, opts)
		if err != nil {
			return types.ResponseStatus_Error, fmt.Errorf("error generating close transaction for minipool %s: %w", mpCommon.Address.Hex(), err)
		}
		data.TxInfos = append(data.TxInfos, txInfo)
	}
	return types.ResponseStatus_Success, nil
}
