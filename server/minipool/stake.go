package csminipool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/url"
	"time"

	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"

	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/gorilla/mux"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/node-manager-core/beacon"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/node-manager-core/node/validator"
	"github.com/rocket-pool/node-manager-core/wallet"

	"github.com/rocket-pool/rocketpool-go/v2/dao/oracle"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
	"github.com/rocket-pool/rocketpool-go/v2/node"
	rptypes "github.com/rocket-pool/rocketpool-go/v2/types"
)

// ===============
// === Factory ===
// ===============

type minipoolStakeContextFactory struct {
	handler *MinipoolHandler
}

func (f *minipoolStakeContextFactory) Create(args url.Values) (*MinipoolStakeContext, error) {
	c := &MinipoolStakeContext{
		ServiceProvider: f.handler.serviceProvider,
		Logger:          f.handler.logger.Logger,
		Context:         f.handler.ctx,
	}
	inputErrs := []error{}
	return c, errors.Join(inputErrs...)
}

func (f *minipoolStakeContextFactory) RegisterRoute(router *mux.Router) {
	RegisterMinipoolRoute[*MinipoolStakeContext, csapi.MinipoolStakeData](
		router, "stake", f, f.handler.ctx, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type MinipoolStakeContext struct {
	// Dependencies
	ServiceProvider cscommon.IConstellationServiceProvider
	Logger          *slog.Logger
	Context         context.Context

	// Services
	nodeAddress common.Address
	res         *csconfig.MergedResources
	wallet      *cscommon.Wallet
	rpMgr       *cscommon.RocketPoolManager
	csMgr       *cscommon.ConstellationManager
	odaoMgr     *oracle.OracleDaoManager
	mpMgr       *minipool.MinipoolManager

	// On-chain vars
	isWhitelisted  bool
	currentTime    time.Time
	scrubPeriod    time.Duration
	stakeValueGwei uint64
}

func (c *MinipoolStakeContext) Initialize(walletStatus wallet.WalletStatus) (types.ResponseStatus, error) {
	sp := c.ServiceProvider
	c.rpMgr = sp.GetRocketPoolManager()
	c.csMgr = sp.GetConstellationManager()
	c.res = sp.GetResources()
	c.wallet = sp.GetWallet()

	// Bindings
	var err error
	c.odaoMgr, err = oracle.NewOracleDaoManager(c.rpMgr.RocketPool)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating oDAO manager binding: %w", err)
	}
	c.mpMgr, err = minipool.NewMinipoolManager(c.rpMgr.RocketPool)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating minipool manager binding: %w", err)
	}

	c.nodeAddress = walletStatus.Wallet.WalletAddress
	return types.ResponseStatus_Success, nil
}

func (c *MinipoolStakeContext) GetState(node *node.Node, mc *batch.MultiCaller) {
	c.csMgr.Whitelist.IsAddressInWhitelist(mc, &c.isWhitelisted, c.nodeAddress)
	eth.AddQueryablesToMulticall(mc,
		c.odaoMgr.Settings.Minipool.ScrubPeriod,
		c.mpMgr.StakeValue,
	)
}

func (c *MinipoolStakeContext) CheckState(node *node.Node, data *csapi.MinipoolStakeData) bool {
	if !c.isWhitelisted {
		data.NotWhitelistedWithConstellation = true
		return false
	}
	return true
}

func (c *MinipoolStakeContext) GetMinipoolDetails(mc *batch.MultiCaller, mp minipool.IMinipool, index int) {
	mpCommon := mp.Common()
	eth.AddQueryablesToMulticall(mc,
		mpCommon.Exists,
		mpCommon.Status,
		mpCommon.StatusTime,
		mpCommon.NodeAddress,
		mpCommon.Pubkey,
		mpCommon.WithdrawalCredentials,
	)
}

func (c *MinipoolStakeContext) PrepareData(addresses []common.Address, mps []minipool.IMinipool, data *csapi.MinipoolStakeData, blockHeader *ethtypes.Header, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	// Prep some data
	c.currentTime = time.Unix(int64(blockHeader.Time), 0)
	c.scrubPeriod = c.odaoMgr.Settings.Minipool.ScrubPeriod.Formatted()
	stakeValueWei := c.mpMgr.StakeValue.Get()
	stakeValueGwei := new(big.Int).Div(stakeValueWei, oneGwei)
	c.stakeValueGwei = stakeValueGwei.Uint64()

	// Process each minipool
	for _, mp := range mps {
		details, err := c.getMinipoolStakeDetails(mp, opts)
		if err != nil {
			return types.ResponseStatus_Error, fmt.Errorf("error getting stake details for minipool [%s]: %w", mp.Common().Address.Hex(), err)
		}
		if details != nil {
			c.Logger.Debug("Added minipool stake details",
				"address", details.Address.Hex(),
				"canStake", details.CanStake,
			)
			data.Details = append(data.Details, *details)
		}
	}
	return types.ResponseStatus_Success, nil
}

func (c *MinipoolStakeContext) getMinipoolStakeDetails(mp minipool.IMinipool, opts *bind.TransactOpts) (*csapi.MinipoolStakeDetails, error) {
	mpCommon := mp.Common()
	if !mpCommon.Exists.Get() {
		// Should never happen, indicates a problem with Constellation where it's returning minipool addresses for the
		// subnode operator that aren't actually registered with Rocket Pool
		c.Logger.Warn("Attempted a stake check on a minipool that is not part of Rocket Pool",
			"address", mpCommon.Address.Hex(),
			"pubkey", mpCommon.Pubkey.Get(),
		)
		return nil, nil
	}

	// Prepare the details
	mpDetails := csapi.MinipoolStakeDetails{
		Address: mpCommon.Address,
		Pubkey:  mpCommon.Pubkey.Get(),
	}

	// Make sure it's in prelaunch
	mpStatus := mpCommon.Status.Formatted()
	if mpStatus != rptypes.MinipoolStatus_Prelaunch {
		c.Logger.Debug("Ignoring stake check on minipool",
			"address", mpCommon.Address.Hex(),
			"state", mpStatus,
		)
		return nil, nil
	}

	// Check if enough time has passed for it to be stakeable
	creationTime := mpCommon.StatusTime.Formatted()
	remainingTime := creationTime.Add(c.scrubPeriod).Sub(c.currentTime)
	if remainingTime > 0 {
		mpDetails.RemainingTime = remainingTime
		mpDetails.StillInScrubPeriod = true
	}
	mpDetails.CanStake = !(mpDetails.StillInScrubPeriod)
	if !mpDetails.CanStake {
		return &mpDetails, nil
	}

	// Load the private key
	pubkey := mpCommon.Pubkey.Get()
	validatorPrivateKey, err := c.wallet.LoadValidatorKey(pubkey)
	if err != nil {
		return nil, fmt.Errorf("error getting validator %s (minipool %s) key: %w", pubkey.Hex(), mpCommon.Address.Hex(), err)
	}

	// Make the deposit data
	withdrawalCredentials := mpCommon.WithdrawalCredentials.Get()
	depositData, err := validator.GetDepositData(
		validatorPrivateKey,
		withdrawalCredentials,
		c.res.GenesisForkVersion,
		c.stakeValueGwei,
		c.res.EthNetworkName,
	)
	if err != nil {
		return nil, fmt.Errorf("error getting deposit data for validator %s: %w", pubkey.Hex(), err)
	}

	// Make the stake TX
	signature := beacon.ValidatorSignature(depositData.Signature)
	depositDataRoot := common.BytesToHash(depositData.DepositDataRoot)
	txInfo, err := c.csMgr.SuperNodeAccount.Stake(signature, depositDataRoot, mpCommon.Address, opts)
	if err != nil {
		return nil, fmt.Errorf("error creating stake transaction for minipool %s: %w", mpCommon.Address.Hex(), err)
	}
	mpDetails.TxInfo = txInfo
	return &mpDetails, nil
}
