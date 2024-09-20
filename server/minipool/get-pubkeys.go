package csminipool

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/mux"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/beacon"
	"github.com/rocket-pool/node-manager-core/utils/input"
	"github.com/rocket-pool/node-manager-core/wallet"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"

	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	nmcserver "github.com/rocket-pool/node-manager-core/api/server"
	"github.com/rocket-pool/node-manager-core/api/types"
)

const (
	minipoolPubkeyQueryBatchSize int = 200
)

// ===============
// === Factory ===
// ===============

type minipoolGetPubkeysContextFactory struct {
	handler *MinipoolHandler
}

func (f *minipoolGetPubkeysContextFactory) Create(args url.Values) (*minipoolGetPubkeysContext, error) {
	c := &minipoolGetPubkeysContext{
		handler: f.handler,
	}
	inputErrs := []error{
		nmcserver.ValidateArg("includeExited", args, input.ValidateBool, &c.includeExited),
	}
	return c, errors.Join(inputErrs...)
}

func (f *minipoolGetPubkeysContextFactory) RegisterRoute(router *mux.Router) {
	server.RegisterQuerylessGet[*minipoolGetPubkeysContext, csapi.MinipoolGetPubkeysData](
		router, "get-pubkeys", f, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type minipoolGetPubkeysContext struct {
	handler       *MinipoolHandler
	includeExited bool
}

func (c *minipoolGetPubkeysContext) PrepareData(data *csapi.MinipoolGetPubkeysData, walletStatus wallet.WalletStatus, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	sp := c.handler.serviceProvider
	ctx := c.handler.ctx
	rpMgr := sp.GetRocketPoolManager()
	csMgr := sp.GetConstellationManager()
	qMgr := sp.GetQueryManager()
	bn := sp.GetBeaconClient()

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
	err = sp.RequireBeaconClientSynced(ctx)
	if err != nil {
		if errors.Is(err, services.ErrBeaconNodeNotSynced) {
			return types.ResponseStatus_ClientsNotSynced, err
		}
		return types.ResponseStatus_Error, err
	}

	// Refresh RP
	err = rpMgr.RefreshRocketPoolContracts()
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error refreshing Rocket Pool contracts: %w", err)
	}

	// Refresh constellation contracts
	err = csMgr.LoadContracts()
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error loading Constellation contracts: %w", err)
	}

	// Bindings
	mpMgr, err := minipool.NewMinipoolManager(rpMgr.RocketPool)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating minipool manager binding: %w", err)
	}

	// Get the list of validators for the node wallet
	var minipools []common.Address
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.SuperNodeAccount.GetSubNodeMinipools(mc, &minipools, walletStatus.Wallet.WalletAddress)
		return nil
	}, nil)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting minipools for node wallet: %w", err)
	}

	// If there are no minipools, return success
	if len(minipools) == 0 {
		return types.ResponseStatus_Success, nil
	}

	// Query the pubkeys for each minipool
	mps, err := mpMgr.CreateMinipoolsFromAddresses(minipools, false, nil)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating minipool bindings: %w", err)
	}
	err = qMgr.BatchQuery(len(mps), minipoolPubkeyQueryBatchSize, func(mc *batch.MultiCaller, i int) error {
		mps[i].Common().Pubkey.AddToQuery(mc)
		return nil
	}, nil)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error querying minipool pubkeys: %w", err)
	}

	// Get the list of pubkeys
	pubkeys := make([]beacon.ValidatorPubkey, len(mps))
	for i, mp := range mps {
		pubkeys[i] = mp.Common().Pubkey.Get()
	}

	// Query the Beacon status for the validators
	statuses, err := bn.GetValidatorStatuses(ctx, pubkeys, nil)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting validator statuses from the beacon chain: %w", err)
	}

	// Create the validator info list
	infos := make([]csapi.MinipoolValidatorInfo, 0, len(pubkeys))
	for _, mp := range mps {
		mpCommon := mp.Common()
		pubkey := mpCommon.Pubkey.Get()

		// Done if the minipool has exited and we're not including exited minipools
		status := statuses[pubkey]
		if !c.includeExited {
			switch status.Status {
			case beacon.ValidatorState_ExitedSlashed,
				beacon.ValidatorState_ExitedUnslashed,
				beacon.ValidatorState_WithdrawalPossible,
				beacon.ValidatorState_WithdrawalDone:
				continue
			}
		}

		infos = append(infos, csapi.MinipoolValidatorInfo{
			Address: mpCommon.Address,
			Pubkey:  pubkey,
			Index:   status.Index, // Empty if not seen yet
		})
	}
	data.Infos = infos
	return types.ResponseStatus_Success, nil
}
