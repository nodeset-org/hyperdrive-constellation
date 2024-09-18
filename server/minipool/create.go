package csminipool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/url"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gorilla/mux"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
	hdapi "github.com/nodeset-org/hyperdrive-daemon/shared/types/api"
	v2constellation "github.com/nodeset-org/nodeset-client-go/api-v2/constellation"
	batch "github.com/rocket-pool/batch-query"
	nmcserver "github.com/rocket-pool/node-manager-core/api/server"
	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/node-manager-core/beacon"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/node-manager-core/node/validator"
	"github.com/rocket-pool/node-manager-core/utils/input"
	"github.com/rocket-pool/node-manager-core/wallet"
	"github.com/rocket-pool/rocketpool-go/v2/dao/oracle"
	"github.com/rocket-pool/rocketpool-go/v2/dao/protocol"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
	"github.com/rocket-pool/rocketpool-go/v2/node"
)

// ===============
// === Factory ===
// ===============

type minipoolCreateContextFactory struct {
	handler *MinipoolHandler
}

func (f *minipoolCreateContextFactory) Create(args url.Values) (*MinipoolCreateContext, error) {
	c := &MinipoolCreateContext{
		ServiceProvider: f.handler.serviceProvider,
		Logger:          f.handler.logger.Logger,
		Context:         f.handler.ctx,
	}
	inputErrs := []error{
		nmcserver.ValidateArg("salt", args, input.ValidateBigInt, &c.Salt),
	}
	return c, errors.Join(inputErrs...)
}

func (f *minipoolCreateContextFactory) RegisterRoute(router *mux.Router) {
	server.RegisterSingleStageRoute[*MinipoolCreateContext, csapi.MinipoolCreateData](
		router, "create", f, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type MinipoolCreateContext struct {
	// Dependencies
	ServiceProvider cscommon.IConstellationServiceProvider
	Logger          *slog.Logger
	Context         context.Context

	// Inputs
	ExpectedMinipoolAddress common.Address
	Salt                    *big.Int

	// Services
	nodeAddress        common.Address
	ec                 eth.IExecutionClient
	bn                 beacon.IBeaconClient
	wallet             *cscommon.Wallet
	rpMgr              *cscommon.RocketPoolManager
	csMgr              *cscommon.ConstellationManager
	rpSuperNodeBinding *node.Node
	pdaoMgr            *protocol.ProtocolDaoManager
	odaoMgr            *oracle.OracleDaoManager
	mpMgr              *minipool.MinipoolManager

	// On-chain vars
	lockThreshold              *big.Int
	minipoolBondAmount         *big.Int
	maxActiveValidatorsPerNode *big.Int
	activeValidatorCount       *big.Int
	isWhitelisted              bool
	internalSalt               *big.Int
}

func (c *MinipoolCreateContext) Initialize(walletStatus wallet.WalletStatus) (types.ResponseStatus, error) {
	sp := c.ServiceProvider
	ctx := c.Context
	c.rpMgr = sp.GetRocketPoolManager()
	c.csMgr = sp.GetConstellationManager()
	c.ec = sp.GetEthClient()
	c.bn = sp.GetBeaconClient()
	c.wallet = sp.GetWallet()

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
	c.pdaoMgr, err = protocol.NewProtocolDaoManager(c.rpMgr.RocketPool)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating protocol dao manager binding: %w", err)
	}
	c.odaoMgr, err = oracle.NewOracleDaoManager(c.rpMgr.RocketPool)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating oracle dao manager binding: %w", err)
	}
	c.mpMgr, err = minipool.NewMinipoolManager(c.rpMgr.RocketPool)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating minipool manager binding: %w", err)
	}

	// Adjust the salt
	c.nodeAddress = walletStatus.Wallet.WalletAddress
	saltBytes := [32]byte{}
	c.Salt.FillBytes(saltBytes[:])
	saltWithNodeAddress := crypto.Keccak256(saltBytes[:], c.nodeAddress[:])
	c.internalSalt = new(big.Int).SetBytes(saltWithNodeAddress)
	return types.ResponseStatus_Success, nil
}

func (c *MinipoolCreateContext) GetState(mc *batch.MultiCaller) {
	c.rpSuperNodeBinding.GetExpectedMinipoolAddress(mc, &c.ExpectedMinipoolAddress, c.internalSalt)
	c.csMgr.SuperNodeAccount.LockThreshold(mc, &c.lockThreshold)
	c.csMgr.SuperNodeAccount.Bond(mc, &c.minipoolBondAmount)
	c.csMgr.Whitelist.IsAddressInWhitelist(mc, &c.isWhitelisted, c.nodeAddress)
	c.csMgr.SuperNodeAccount.GetMaxValidators(mc, &c.maxActiveValidatorsPerNode)
	c.csMgr.Whitelist.GetActiveValidatorCountForOperator(mc, &c.activeValidatorCount, c.nodeAddress)
	eth.AddQueryablesToMulticall(mc,
		c.pdaoMgr.Settings.Node.IsDepositingEnabled,
		c.odaoMgr.Settings.Minipool.ScrubPeriod,
		c.mpMgr.PrelaunchValue,
	)
}

func (c *MinipoolCreateContext) PrepareData(data *csapi.MinipoolCreateData, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	sp := c.ServiceProvider
	hd := sp.GetHyperdriveClient()
	resources := sp.GetHyperdriveResources()
	qMgr := sp.GetQueryManager()

	// Make sure the node's registered
	regResponse, err := hd.NodeSet.GetRegistrationStatus()
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting node registration status: %w", err)
	}
	switch regResponse.Data.Status {
	case hdapi.NodeSetRegistrationStatus_Unknown:
		return types.ResponseStatus_Error, fmt.Errorf("node registration status is unknown: %s", regResponse.Data.ErrorMessage)
	case hdapi.NodeSetRegistrationStatus_NoWallet:
		// Shouldn't get hit because of the requirement in Initialize
		return types.ResponseStatus_WalletNotReady, fmt.Errorf("node does not have a wallet loaded")
	case hdapi.NodeSetRegistrationStatus_Unregistered:
		data.NotRegisteredWithNodeSet = true
	}

	// Make sure the salt hasn't been used (no existing minipool at the given address)
	code, err := c.ec.CodeAt(c.Context, c.ExpectedMinipoolAddress, nil)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting code at expected minipool address [%s]: %w", c.ExpectedMinipoolAddress.Hex(), err)
	}
	if len(code) > 0 {
		return types.ResponseStatus_InvalidChainState, fmt.Errorf("something already exists at expected minipool address [%s]", c.ExpectedMinipoolAddress.Hex())
	}

	// Check the node's balance (must have enough ETH for the lockup)
	data.LockupAmount = c.lockThreshold
	data.NodeBalance, err = c.ec.BalanceAt(c.Context, c.nodeAddress, nil)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting node balance: %w", err)
	}
	data.InsufficientBalance = c.lockThreshold.Cmp(data.NodeBalance) > 0

	// Check for sufficient liquidity
	var hasSufficientLiquidity bool
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		c.csMgr.SuperNodeAccount.HasSufficientLiquidity(mc, &hasSufficientLiquidity, c.minipoolBondAmount)
		return nil
	}, nil)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error checking for sufficient liquidity: %w", err)
	}
	data.InsufficientLiquidity = !hasSufficientLiquidity

	// Check the minipool limit
	data.MaxMinipoolsReached = c.activeValidatorCount.Cmp(c.maxActiveValidatorsPerNode) >= 0

	// Get a deposit signature
	sigResponse, err := hd.NodeSet_Constellation.GetDepositSignature(c.ExpectedMinipoolAddress, c.Salt)
	if err != nil {
		if errors.Is(err, v2constellation.ErrValidatorRequiresExitMessage) {
			data.MissingExitMessage = true
		} else {
			return types.ResponseStatus_Error, fmt.Errorf("error getting deposit signature: %w", err)
		}
	}
	data.NodeSetDepositingDisabled = false // TODO: once the spec is set up with the flag, put it into this check

	// Check if we can deposit
	data.NotWhitelistedWithConstellation = !c.isWhitelisted
	data.RocketPoolDepositingDisabled = !c.pdaoMgr.Settings.Node.IsDepositingEnabled.Get()
	data.CanCreate = !(data.InsufficientBalance || data.InsufficientLiquidity || data.NotRegisteredWithNodeSet || data.NotWhitelistedWithConstellation || data.MissingExitMessage || data.RocketPoolDepositingDisabled || data.NodeSetDepositingDisabled || data.MaxMinipoolsReached)
	if !data.CanCreate {
		return types.ResponseStatus_Success, nil
	}

	// Create a new validator key
	w := c.wallet
	validatorKey, err := w.GetNextValidatorKey()
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error generating new validator key: %w", err)
	}

	// Check to see if it already exists on Beacon
	status, err := c.bn.GetValidatorStatus(c.Context, validatorKey.PublicKey, nil)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting validator status: %w", err)
	}
	if status.Exists {
		// This pubkey is already on the chain, can't reuse it
		return types.ResponseStatus_InvalidChainState, fmt.Errorf("validator pubkey %s already exists on the Beacon chain", validatorKey.PublicKey.Hex())
	}

	// Create deposit data
	prelaunchValueWei := c.mpMgr.PrelaunchValue.Get()
	prelaunchValueGwei := new(big.Int).Div(prelaunchValueWei, oneGwei)
	withdrawalCredentials := validator.GetWithdrawalCredsFromAddress(c.ExpectedMinipoolAddress)
	depositData, err := validator.GetDepositData(
		c.Logger,
		validatorKey.PrivateKey,
		withdrawalCredentials,
		resources.GenesisForkVersion,
		prelaunchValueGwei.Uint64(),
		resources.EthNetworkName,
	)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating deposit data for validator [%s]: %w", validatorKey.PublicKey.Hex(), err)
	}
	data.ValidatorPubkey = validatorKey.PublicKey
	data.Index = validatorKey.WalletIndex
	data.MinipoolAddress = c.ExpectedMinipoolAddress
	data.ScrubPeriod = c.odaoMgr.Settings.Minipool.ScrubPeriod.Formatted()

	// Make the TX
	newOpts := &bind.TransactOpts{
		From:  opts.From,
		Value: prelaunchValueWei,
	}
	depositDataSignature := beacon.ValidatorSignature(depositData.Signature)
	depositDataRoot := common.BytesToHash(depositData.DepositDataRoot)
	data.TxInfo, err = c.csMgr.SuperNodeAccount.CreateMinipool(
		validatorKey.PublicKey,
		depositDataSignature,
		depositDataRoot,
		c.Salt,
		c.ExpectedMinipoolAddress,
		sigResponse.Data.Signature,
		newOpts,
	)
	if err != nil {
		return types.ResponseStatus_Error, err
	}
	return types.ResponseStatus_Success, nil
}
