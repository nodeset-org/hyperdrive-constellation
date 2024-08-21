package csnode

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/node-manager-core/utils"
	"github.com/rocket-pool/node-manager-core/wallet"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/gorilla/mux"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
)

// ===============
// === Factory ===
// ===============

type nodeRegisterContextFactory struct {
	handler *NodeHandler
}

func (f *nodeRegisterContextFactory) Create(args url.Values) (*nodeRegisterContext, error) {
	c := &nodeRegisterContext{
		handler: f.handler,
	}
	inputErrs := []error{}
	return c, errors.Join(inputErrs...)
}

func (f *nodeRegisterContextFactory) RegisterRoute(router *mux.Router) {
	server.RegisterQuerylessGet[*nodeRegisterContext, csapi.NodeRegisterData](
		router, "register", f, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type nodeRegisterContext struct {
	handler *NodeHandler
}

func (c *nodeRegisterContext) PrepareData(data *csapi.NodeRegisterData, walletStatus wallet.WalletStatus, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	sp := c.handler.serviceProvider
	hd := sp.GetHyperdriveClient()
	csMgr := sp.GetConstellationManager()
	ctx := c.handler.ctx

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

	// Request a registration signature - note this will use the wallet address, not the node address
	sigResponse, err := hd.NodeSet_Constellation.GetRegistrationSignature()
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting registration signature: %w", err)
	}
	if sigResponse.Data.NotAuthorized {
		data.NotAuthorized = true
		return types.ResponseStatus_Success, nil
	}
	if sigResponse.Data.NotRegistered {
		data.NotRegisteredWithNodeSet = true
		return types.ResponseStatus_Success, nil
	}

	// Print the signature
	logger := c.handler.logger
	sigHex := utils.EncodeHexWithPrefix(sigResponse.Data.Signature)
	logger.Info("Registration signature",
		slog.String("signature", sigHex),
	)

	// Get the registration TX
	data.TxInfo, err = csMgr.Whitelist.AddOperator(walletStatus.Wallet.WalletAddress, sigResponse.Data.Signature, opts)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating registration TX: %w", err)
	}
	return types.ResponseStatus_Success, nil
}
