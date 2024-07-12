package csminipool

import (
	"errors"
	"fmt"
	"math/big"
	"net/url"

	"github.com/nodeset-org/hyperdrive-constellation/common/contracts/constellation"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/mux"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/node-manager-core/node/validator"
	"github.com/rocket-pool/node-manager-core/wallet"

	eth2types "github.com/wealdtech/go-eth2-types/v2"
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
	inputErrs := []error{}
	return c, errors.Join(inputErrs...)
}

func (f *minipoolDepositMinipoolContextFactory) RegisterRoute(router *mux.Router) {
	server.RegisterQuerylessGet[*minipoolDepositMinipoolContext, types.TxInfoData](
		router, "deposit-minipool", f, f.handler.logger.Logger, f.handler.serviceProvider.ServiceProvider,
	)
}

// ===============
// === Context ===
// ===============

type minipoolDepositMinipoolContext struct {
	handler     *MinipoolHandler
	salt        []byte
	nodeAddress common.Address
}

func (c *minipoolDepositMinipoolContext) PrepareData(data *types.TxInfoData, walletStatus wallet.WalletStatus, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	sp := c.handler.serviceProvider
	csMgr := sp.GetConstellationManager()
	hd := sp.GetHyperdriveClient()
	resources := sp.GetResources()

	// TODO: Implement something similar to close-details.go
	// Requirements
	err := sp.RequireWalletReady(walletStatus)
	if err != nil {
		return types.ResponseStatus_WalletNotReady, err
	}

	response, err := hd.NodeSet_Constellation.GetDepositSignature(c.nodeAddress, c.salt)
	if err != nil {
		return types.ResponseStatus_Error, err
	}

	// TODO
	var validatorKey *eth2types.BLSPrivateKey
	var withdrawalCredentials common.Hash

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
		ValidatorPubkey:         "",
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
