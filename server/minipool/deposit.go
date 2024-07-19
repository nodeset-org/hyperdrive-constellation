package csminipool

import (
	"errors"
	"fmt"
	"math/big"
	"net/url"

	"github.com/rocket-pool/node-manager-core/beacon"

	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"

	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	"github.com/nodeset-org/hyperdrive-constellation/common/contracts/constellation"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/mux"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
	batch "github.com/rocket-pool/batch-query"
	nmcserver "github.com/rocket-pool/node-manager-core/api/server"
	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/node-manager-core/node/validator"
	"github.com/rocket-pool/node-manager-core/utils/input"
	"github.com/rocket-pool/node-manager-core/wallet"

	"github.com/rocket-pool/rocketpool-go/v2/node"
)

// ===============
// === Factory ===
// ===============

type minipoolDepositMinipoolContextFactory struct {
	handler *MinipoolHandler
}

func (f *minipoolDepositMinipoolContextFactory) Create(args url.Values) (*minipoolDepositMinipoolContext, error) {
	c := &minipoolDepositMinipoolContext{
		handler: f.handler,
	}
	inputErrs := []error{
		nmcserver.ValidateArg("salt", args, input.ValidateBigInt, &c.salt),
	}
	return c, errors.Join(inputErrs...)
}

func (f *minipoolDepositMinipoolContextFactory) RegisterRoute(router *mux.Router) {
	server.RegisterSingleStageRoute[*minipoolDepositMinipoolContext, csapi.MinipoolDepositMinipoolData](
		router, "deposit-minipool", f, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type minipoolDepositMinipoolContext struct {
	handler *MinipoolHandler

	rpSuperNodeBinding      *node.Node
	expectedMinipoolAddress common.Address
	salt                    *big.Int
	hasSufficientLiquidity  bool
	isWhitelisted           bool
	nodeAddress             common.Address

	rpMgr *cscommon.RocketPoolManager
	csMgr *cscommon.ConstellationManager
}

func (c *minipoolDepositMinipoolContext) Initialize(walletStatus wallet.WalletStatus) (types.ResponseStatus, error) {
	sp := c.handler.serviceProvider
	c.rpMgr = sp.GetRocketPoolManager()
	c.csMgr = sp.GetConstellationManager()
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

	err = sp.RequireBeaconClientSynced(ctx)
	if err != nil {
		if errors.Is(err, services.ErrBeaconNodeNotSynced) {
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
	superNodeAddress := c.csMgr.SuperNodeAccount.Address
	c.rpSuperNodeBinding, err = node.NewNode(c.rpMgr.RocketPool, superNodeAddress)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating node %s binding: %w", superNodeAddress.Hex(), err)
	}

	c.nodeAddress = walletStatus.Wallet.WalletAddress

	return types.ResponseStatus_Success, nil
}

func (c *minipoolDepositMinipoolContext) GetState(mc *batch.MultiCaller) {
	c.rpSuperNodeBinding.GetExpectedMinipoolAddress(mc, &c.expectedMinipoolAddress, c.salt)
	c.csMgr.SuperNodeAccount.HasSufficientLiquidity(mc, &c.hasSufficientLiquidity, eth.EthToWei(8))
	c.csMgr.Whitelist.IsAddressInWhitelist(mc, &c.isWhitelisted, c.nodeAddress)
}

func (c *minipoolDepositMinipoolContext) PrepareData(data *csapi.MinipoolDepositMinipoolData, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	sp := c.handler.serviceProvider
	hd := sp.GetHyperdriveClient()
	resources := sp.GetResources()

	// Validations
	if !c.isWhitelisted {
		data.NotWhitelisted = true
		return types.ResponseStatus_Success, nil
	}

	// TODO: Implement our own InsufficientLiquidity check
	//      1. [CONST] Is there enough WETH in the Constellation WETH vault to cover bond?
	// 		2. [RP] Is there enough RPL staked to cover creating minipool?

	// if !c.hasSufficientLiquidity {
	// 	data.InsufficientLiquidity = true
	// 	return types.ResponseStatus_Success, nil
	// }

	availableResponse, err := hd.NodeSet_Constellation.GetAvailableMinipoolCount()
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting available minipool count: %w", err)
	}
	if availableResponse.Data.Count < 1 {
		data.InsufficientMinipoolCount = true
		return types.ResponseStatus_Success, nil
	}

	response, err := hd.NodeSet_Constellation.GetDepositSignature(c.expectedMinipoolAddress, c.salt)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting deposit signature: %w", err)
	}

	w, err := cscommon.NewWallet(sp)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating wallet: %w", err)
	}
	validatorKey, err := w.GenerateNewValidatorKey()
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error generating new validator key: %w", err)
	}

	withdrawalCredentials := validator.GetWithdrawalCredsFromAddress(c.expectedMinipoolAddress)

	depositData, err := validator.GetDepositData(
		validatorKey,
		withdrawalCredentials,
		resources.GenesisForkVersion,
		eth.EthToWei(1).Uint64(),
		resources.EthNetworkName,
	)
	if err != nil {
		return types.ResponseStatus_Error, err
	}
	validatorPubkey := beacon.ValidatorPubkey(validatorKey.PublicKey().Marshal())
	data.ValidatorPubKey = validatorPubkey

	depositDataRoot := common.BytesToHash(depositData.DepositDataRoot)

	validatorConfig := constellation.ValidatorConfig{
		TimezoneLocation:        "",
		BondAmount:              big.NewInt(0),
		MinimumNodeFee:          big.NewInt(0),
		ValidatorPubkey:         validatorPubkey[:],
		ValidatorSignature:      depositData.Signature,
		DepositDataRoot:         depositDataRoot,
		Salt:                    c.salt,
		ExpectedMinipoolAddress: c.expectedMinipoolAddress,
	}

	newOpts := &bind.TransactOpts{
		From:  opts.From,
		Value: big.NewInt(0).Set(eth.EthToWei(1)),
	}

	data.TxInfo, err = c.csMgr.SuperNodeAccount.CreateMinipool(validatorConfig, response.Data.Signature, response.Data.Time, newOpts)
	if err != nil {
		return types.ResponseStatus_Error, err
	}
	return types.ResponseStatus_Success, nil
}
