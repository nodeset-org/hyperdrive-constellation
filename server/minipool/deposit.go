package csminipool

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/url"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/mux"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	hdclient "github.com/nodeset-org/hyperdrive-daemon/client"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
	hdapi "github.com/nodeset-org/hyperdrive-daemon/shared/types/api"
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
	"github.com/rocket-pool/rocketpool-go/v2/node"
)

const (
	minipoolPrelaunchAmountGwei uint64 = 1e9                               // 1 gwei, the amount to specify in deposit data
	minipoolPrelaunchAmountWei  uint64 = minipoolPrelaunchAmountGwei * 1e9 // The minipool prelaunch amount in wei
)

// ===============
// === Factory ===
// ===============

type minipoolDepositMinipoolContextFactory struct {
	handler *MinipoolHandler
}

func (f *minipoolDepositMinipoolContextFactory) Create(args url.Values) (*MinipoolDepositMinipoolContext, error) {
	c := &MinipoolDepositMinipoolContext{
		ServiceProvider: f.handler.serviceProvider,
		Context:         f.handler.ctx,
	}
	inputErrs := []error{
		nmcserver.ValidateArg("salt", args, input.ValidateBigInt, &c.Salt),
	}
	return c, errors.Join(inputErrs...)
}

func (f *minipoolDepositMinipoolContextFactory) RegisterRoute(router *mux.Router) {
	server.RegisterSingleStageRoute[*MinipoolDepositMinipoolContext, csapi.MinipoolDepositData](
		router, "deposit-minipool", f, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type MinipoolDepositMinipoolContext struct {
	// Dependencies
	ServiceProvider cscommon.IConstellationServiceProvider
	Context         context.Context

	// Inputs
	ExpectedMinipoolAddress common.Address
	Salt                    *big.Int

	// Services
	nodeAddress        common.Address
	res                *csconfig.MergedResources
	hd                 *hdclient.ApiClient
	ec                 eth.IExecutionClient
	bn                 beacon.IBeaconClient
	wallet             *cscommon.Wallet
	rpMgr              *cscommon.RocketPoolManager
	csMgr              *cscommon.ConstellationManager
	rpSuperNodeBinding *node.Node
	pdaoMgr            *protocol.ProtocolDaoManager
	odaoMgr            *oracle.OracleDaoManager

	// On-chain vars
	hasSufficientLiquidity bool
	isWhitelisted          bool
}

func (c *MinipoolDepositMinipoolContext) Initialize(walletStatus wallet.WalletStatus) (types.ResponseStatus, error) {
	sp := c.ServiceProvider
	ctx := c.Context
	c.rpMgr = sp.GetRocketPoolManager()
	c.csMgr = sp.GetConstellationManager()
	c.res = sp.GetResources()
	c.hd = sp.GetHyperdriveClient()
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
		return types.ResponseStatus_Error, fmt.Errorf("error creating protocol dao manager: %w", err)
	}
	c.odaoMgr, err = oracle.NewOracleDaoManager(c.rpMgr.RocketPool)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating oracle dao manager: %w", err)
	}

	c.nodeAddress = walletStatus.Wallet.WalletAddress
	return types.ResponseStatus_Success, nil
}

func (c *MinipoolDepositMinipoolContext) GetState(mc *batch.MultiCaller) {
	c.rpSuperNodeBinding.GetExpectedMinipoolAddress(mc, &c.ExpectedMinipoolAddress, c.Salt)
	c.csMgr.SuperNodeAccount.HasSufficientLiquidity(mc, &c.hasSufficientLiquidity, eth.EthToWei(8))
	c.csMgr.Whitelist.IsAddressInWhitelist(mc, &c.isWhitelisted, c.nodeAddress)
	eth.AddQueryablesToMulticall(mc,
		c.pdaoMgr.Settings.Node.IsDepositingEnabled,
		c.odaoMgr.Settings.Minipool.ScrubPeriod,
	)
}

func (c *MinipoolDepositMinipoolContext) PrepareData(data *csapi.MinipoolDepositData, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	hd := c.hd
	resources := c.res

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

	// Check the node's balance (must have 1 ETH for temporary depositing)
	data.EthBalance, err = c.ec.BalanceAt(c.Context, c.nodeAddress, nil)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting node balance: %w", err)
	}
	prelaunchRequirement := new(big.Int).SetUint64(minipoolPrelaunchAmountWei)
	data.InsufficientBalance = data.EthBalance.Cmp(prelaunchRequirement) < 0

	availableResponse, err := hd.NodeSet_Constellation.GetAvailableMinipoolCount()
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting available minipool count: %w", err)
	}
	if availableResponse.Data.Count < 1 {
		data.InsufficientMinipoolCount = true
	}

	// Get a deposit signature
	sigResponse, err := hd.NodeSet_Constellation.GetDepositSignature(c.ExpectedMinipoolAddress, c.Salt, c.csMgr.SuperNodeAccount.Address)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting deposit signature: %w", err)
	}
	data.NodeSetDepositingDisabled = false // TODO: once the spec is set up with the flag, put it into this check

	// Check if we can deposit
	data.NotWhitelistedWithConstellation = !c.isWhitelisted
	data.InsufficientLiquidity = !c.hasSufficientLiquidity
	data.RocketPoolDepositingDisabled = !c.pdaoMgr.Settings.Node.IsDepositingEnabled.Get()
	data.CanDeposit = !(data.InsufficientBalance || data.InsufficientLiquidity || data.NotRegisteredWithNodeSet || data.NotWhitelistedWithConstellation || data.InsufficientMinipoolCount || data.RocketPoolDepositingDisabled || data.NodeSetDepositingDisabled)
	if !data.CanDeposit {
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
	withdrawalCredentials := validator.GetWithdrawalCredsFromAddress(c.ExpectedMinipoolAddress)
	depositData, err := validator.GetDepositData(
		validatorKey.PrivateKey,
		withdrawalCredentials,
		resources.GenesisForkVersion,
		minipoolPrelaunchAmountGwei,
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
		Value: prelaunchRequirement,
	}
	depositDataRoot := common.BytesToHash(depositData.DepositDataRoot)
	data.TxInfo, err = c.csMgr.SuperNodeAccount.CreateMinipool(
		validatorKey.PublicKey[:],
		depositData.Signature,
		depositDataRoot,
		c.Salt,
		c.ExpectedMinipoolAddress,
		sigResponse.Data.Time,
		sigResponse.Data.Signature,
		newOpts,
	)
	if err != nil {
		return types.ResponseStatus_Error, err
	}
	return types.ResponseStatus_Success, nil
}
