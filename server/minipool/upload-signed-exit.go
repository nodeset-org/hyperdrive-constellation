package csminipool

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/gorilla/mux"

	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	modserver "github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
	nscommon "github.com/nodeset-org/nodeset-client-go/common"
	"github.com/rocket-pool/node-manager-core/api/types"
	nmc_validator "github.com/rocket-pool/node-manager-core/node/validator"
	"github.com/rocket-pool/node-manager-core/wallet"
	eth2types "github.com/wealdtech/go-eth2-types/v2"
)

// ===============
// === Factory ===
// ===============

type minipoolUploadSignedExitsContextFactory struct {
	handler *MinipoolHandler
}

func (f *minipoolUploadSignedExitsContextFactory) Create(body csapi.MinipoolUploadSignedExitBody) (*MinipoolUploadSignedExitsContext, error) {
	c := &MinipoolUploadSignedExitsContext{
		ServiceProvider: f.handler.serviceProvider,
		Logger:          f.handler.logger.Logger,
		Context:         f.handler.ctx,
	}
	c.Infos = body.Infos
	return c, nil
}

func (f *minipoolUploadSignedExitsContextFactory) RegisterRoute(router *mux.Router) {
	modserver.RegisterQuerylessPost[*MinipoolUploadSignedExitsContext, csapi.MinipoolUploadSignedExitBody, types.SuccessData](
		router, "upload-signed-exits", f, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type MinipoolUploadSignedExitsContext struct {
	// Dependencies
	ServiceProvider cscommon.IConstellationServiceProvider
	Logger          *slog.Logger
	Context         context.Context

	// Arguments
	Infos []csapi.MinipoolExitInfo
}

func (c *MinipoolUploadSignedExitsContext) PrepareData(data *types.SuccessData, walletStatus wallet.WalletStatus, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	sp := c.ServiceProvider
	ctx := c.Context
	w := sp.GetWallet()
	bc := sp.GetBeaconClient()
	hd := sp.GetHyperdriveClient()

	// Requirements
	err := sp.RequireBeaconClientSynced(c.Context)
	if err != nil {
		return types.ResponseStatus_ClientsNotSynced, err
	}

	// Get beacon head
	head, err := bc.GetBeaconHead(ctx)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting beacon head: %w", err)
	}
	epoch := head.FinalizedEpoch // Use the finalized epoch for signed exits

	// Get voluntary exit signature domain
	signatureDomain, err := bc.GetDomainData(ctx, eth2types.DomainVoluntaryExit[:], epoch, false)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting beacon domain data: %w", err)
	}

	// Get a signed exit for each pubkey
	messages := make([]nscommon.ExitData, len(c.Infos))
	for i, info := range c.Infos {
		address := info.Address
		pubkey := info.Pubkey
		index := info.Index

		if index == "" {
			return types.ResponseStatus_Error, fmt.Errorf("minipool %s (pubkey %s) does not have an index on the Beacon chain yet", address.Hex(), pubkey.Hex())
		}

		// Get validator private key
		validatorKey, err := w.LoadValidatorKey(pubkey)
		if err != nil {
			return types.ResponseStatus_Error, fmt.Errorf("error getting private key for minipool %s (pubkey %s): %w", address.Hex(), pubkey.Hex(), err)
		}

		// Get signed voluntary exit message
		signature, err := nmc_validator.GetSignedExitMessage(validatorKey, index, epoch, signatureDomain)
		if err != nil {
			return types.ResponseStatus_Error, fmt.Errorf("error getting exit message signature for minipool %s (pubkey %s): %w", address.Hex(), pubkey.Hex(), err)
		}

		// Add it to the list
		messages[i] = nscommon.ExitData{
			Pubkey: pubkey.HexWithPrefix(),
			ExitMessage: nscommon.ExitMessage{
				Message: nscommon.ExitMessageDetails{
					Epoch:          strconv.FormatUint(epoch, 10),
					ValidatorIndex: index,
				},
				Signature: signature.HexWithPrefix(),
			},
		}
		c.Logger.Debug("Created signed exit",
			slog.String("minipool", address.Hex()),
			slog.String("pubkey", pubkey.Hex()),
		)
	}

	// Submit it to the server
	uploadResponse, err := hd.NodeSet_Constellation.UploadSignedExits(messages)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error submitting signed exits: %w", err)
	}
	if uploadResponse.Data.NotAuthorized {
		return types.ResponseStatus_Error, fmt.Errorf("node is not authorized for constellation")
	}
	if uploadResponse.Data.NotRegistered {
		return types.ResponseStatus_Error, fmt.Errorf("node is not registered with nodeset yet")
	}

	// Get the list of validators for the node now
	validatorsResponse, err := hd.NodeSet_Constellation.GetValidators()
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error checking validators list from NodeSet: %w", err)
	}

	// Make sure each minipool is marked as submitted
	for _, info := range c.Infos {
		found := false
		for _, validator := range validatorsResponse.Data.Validators {
			if validator.Pubkey != info.Pubkey {
				continue
			}
			found = true
			if !validator.ExitMessageUploaded {
				return types.ResponseStatus_Error, fmt.Errorf("validator %s exit message was uploaded but has not been marked on the NodeSet server?", info.Pubkey.Hex())
			}
			break
		}
		if !found {
			return types.ResponseStatus_Error, fmt.Errorf("validator %s was uploaded but not found in the NodeSet's list for this node", info.Pubkey.Hex())
		}
	}

	return types.ResponseStatus_Success, nil
}
