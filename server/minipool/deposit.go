package csminipool

import (
	"errors"
	"fmt"
	"math/big"
	"net/url"

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
	server.RegisterSingleStageRoute[*minipoolDepositMinipoolContext, types.TxInfoData](
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

	// Require synced execution + beacon client
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
	c.csMgr.SuperNodeAccount.HasSufficentLiquidity(mc, &c.hasSufficientLiquidity, eth.EthToWei(8))
	c.csMgr.Whitelist.IsAddressInWhitelist(mc, &c.isWhitelisted, c.nodeAddress)
}

func (c *minipoolDepositMinipoolContext) PrepareData(data *csapi.MinipoolDepositMinipoolData, walletStatus wallet.WalletStatus, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	sp := c.handler.serviceProvider
	hd := sp.GetHyperdriveClient()
	resources := sp.GetResources()

	// Validations
	if !c.isWhitelisted {
		data.NotWhitelisted = true
		return types.ResponseStatus_Success, nil
	}
	if !c.hasSufficientLiquidity {
		data.InsufficientLiquidity = true
		return types.ResponseStatus_Success, nil
	}

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

	// w, err := cscommon.NewWallet(sp)
	// if err != nil {
	// 	return types.ResponseStatus_Error, fmt.Errorf("error creating wallet: %w", err)
	// }

	// depositData, err := validator.GetDepositData(
	// 	privateKey,
	// 	withdrawalCredentials,
	// 	resources.GenesisForkVersion,
	// 	eth.EthToWei(1).Uint64(),
	// 	resources.EthNetworkName,
	// )
	// if err != nil {
	// 	return types.ResponseStatus_Error, err
	// }

	// // TODO: Call RP contract (node address + salt) (node/node.go from Rocketpool-go)
	// var expectedMinipoolAddress common.Address
	// err = sp.GetQueryManager().Query(func(mc *batch.MultiCaller) error {
	// 	csMgr.SuperNodeAccount.GetNextMinipool(mc, &expectedMinipoolAddress)
	// 	return nil
	// }, nil)
	// if err != nil {
	// 	return types.ResponseStatus_Error, fmt.Errorf("error getting next minipool: %w", err)
	// }

	validatorConfig := constellation.ValidatorConfig{
		TimezoneLocation:        "",
		BondAmount:              big.NewInt(0),
		MinimumNodeFee:          big.NewInt(0),
		ValidatorPubkey:         []byte(pubkey.Hex()),
		ValidatorSignature:      depositData.Signature,
		DepositDataRoot:         depositData.DepositDataRoot,
		Salt:                    new(big.Int).SetBytes(c.salt),
		ExpectedMinipoolAddress: expectedMinipoolAddress,
	}

	data.TxInfo, err = csMgr.SuperNodeAccount.CreateMinipool(validatorConfig, response.Data.Signature, opts)
	if err != nil {
		return types.ResponseStatus_Error, err
	}
	return types.ResponseStatus_Success, nil
}
