package csminipool

import (
	"errors"
	"fmt"
	"math/big"
	"net/url"

	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	"github.com/nodeset-org/hyperdrive-constellation/common/contracts/constellation"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/mux"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
	batch "github.com/rocket-pool/batch-query"
	nmcserver "github.com/rocket-pool/node-manager-core/api/server"
	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/node-manager-core/node/validator"
	"github.com/rocket-pool/node-manager-core/wallet"

	rpminipool "github.com/rocket-pool/rocketpool-go/v2/minipool"
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
		nmcserver.GetStringFromVars("salt", args, &c.salt),
		nmcserver.GetStringFromVars("nodeAddress", args, &c.nodeAddress),
	}
	return c, errors.Join(inputErrs...)
}

func (f *minipoolDepositMinipoolContextFactory) RegisterRoute(router *mux.Router) {
	server.RegisterQuerylessGet[*minipoolDepositMinipoolContext, types.TxInfoData](
		router, "deposit-minipool", f, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type minipoolDepositMinipoolContext struct {
	handler *MinipoolHandler

	MinipoolAddresses []common.Address
	mps               []rpminipool.IMinipool
	mpOwnerFlags      []bool

	salt        string
	nodeAddress string
}

func (c *minipoolDepositMinipoolContext) PrepareData(data *types.TxInfoData, walletStatus wallet.WalletStatus, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	sp := c.handler.serviceProvider
	rpMgr := sp.GetRocketPoolManager()

	csMgr := sp.GetConstellationManager()
	hd := sp.GetHyperdriveClient()
	resources := sp.GetResources()

	// TODO: Implement something similar to close-details.go
	// Requirements
	err := sp.RequireWalletReady(walletStatus)
	if err != nil {
		return types.ResponseStatus_WalletNotReady, err
	}
	nodeAddress := common.HexToAddress(c.nodeAddress)
	saltByte := []byte(c.salt)
	response, err := hd.NodeSet_Constellation.GetDepositSignature(nodeAddress, saltByte)
	if err != nil {
		return types.ResponseStatus_Error, err
	}

	// Refresh RP
	err = rpMgr.RefreshRocketPoolContracts()
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error refreshing Rocket Pool contracts: %w", err)
	}
	rp := rpMgr.RocketPool

	// Create minipool binding
	mpMgr, err := rpminipool.NewMinipoolManager(rp)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating minipool manager binding: %w", err)
	}
	c.mps, err = mpMgr.CreateMinipoolsFromAddresses(c.MinipoolAddresses, false, nil)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating minipool bindings: %w", err)
	}
	c.mpOwnerFlags = make([]bool, len(c.mps))

	// Query pubkey
	qMgr := sp.GetQueryManager()
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		c.mps[0].Common().WithdrawalCredentials.AddToQuery(mc)
		c.mps[0].Common().Pubkey.AddToQuery(mc)
		return nil
	}, nil)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error querying minipool pubkey: %w", err)
	}

	pubkey := c.mps[0].Common().Pubkey.Get()
	w, err := cscommon.NewWallet(sp)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating wallet: %w", err)
	}

	withdrawalCredentials := c.mps[0].Common().WithdrawalCredentials.Get()

	privateKey, err := w.GetPrivateKeyForPubkey(pubkey)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting private key for pubkey: %w", err)
	}

	depositData, err := validator.GetDepositData(
		privateKey,
		withdrawalCredentials,
		resources.GenesisForkVersion,
		eth.EthToWei(1).Uint64(),
		resources.EthNetworkName,
	)
	if err != nil {
		return types.ResponseStatus_Error, err
	}

	var expectedMinipoolAddress common.Address
	err = sp.GetQueryManager().Query(func(mc *batch.MultiCaller) error {
		csMgr.SuperNodeAccount.GetNextMinipool(mc, &expectedMinipoolAddress)
		return nil
	}, nil)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting next minipool: %w", err)
	}

	validatorConfig := constellation.ValidatorConfig{
		TimezoneLocation:        "",
		BondAmount:              big.NewInt(0),
		MinimumNodeFee:          big.NewInt(0),
		ValidatorPubkey:         []byte(pubkey.Hex()),
		ValidatorSignature:      depositData.Signature,
		DepositDataRoot:         depositData.DepositDataRoot,
		Salt:                    new(big.Int).SetBytes(saltByte),
		ExpectedMinipoolAddress: expectedMinipoolAddress,
	}

	data.TxInfo, err = csMgr.SuperNodeAccount.CreateMinipool(validatorConfig, response.Data.Signature, opts)
	if err != nil {
		return types.ResponseStatus_Error, err
	}
	return types.ResponseStatus_Success, nil
}
