package csminipool

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gorilla/mux"
	csapi "github.com/nodeset-org/hyperdrive-constellation/shared/api"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/server"
	nmcserver "github.com/rocket-pool/node-manager-core/api/server"
	"github.com/rocket-pool/node-manager-core/api/types"
	"github.com/rocket-pool/node-manager-core/utils"
	"github.com/rocket-pool/node-manager-core/wallet"
	"github.com/rocket-pool/rocketpool-go/v2/rocketpool"
)

const (
	ozMinipoolBytecode string = "0x3d602d80600a3d3981f3363d3d373d3d3d363d73%s5af43d82803e903d91602b57fd5bf3"
)

// ===============
// === Factory ===
// ===============

type minipoolVanityContextFactory struct {
	handler *MinipoolHandler
}

func (f *minipoolVanityContextFactory) Create(args url.Values) (*MinipoolVanityContext, error) {
	c := &MinipoolVanityContext{
		handler: f.handler,
	}
	inputErrs := []error{
		nmcserver.GetStringFromVars("node-address", args, &c.nodeAddressStr),
	}
	return c, errors.Join(inputErrs...)
}

func (f *minipoolVanityContextFactory) RegisterRoute(router *mux.Router) {
	server.RegisterQuerylessGet[*MinipoolVanityContext, csapi.MinipoolVanityArtifactsData](
		router, "vanity-artifacts", f, f.handler.logger.Logger, f.handler.serviceProvider,
	)
}

// ===============
// === Context ===
// ===============

type MinipoolVanityContext struct {
	handler        *MinipoolHandler
	nodeAddressStr string
}

func (c *MinipoolVanityContext) PrepareData(data *csapi.MinipoolVanityArtifactsData, walletStatus wallet.WalletStatus, opts *bind.TransactOpts) (types.ResponseStatus, error) {
	sp := c.handler.serviceProvider
	rpMgr := sp.GetRocketPoolManager()
	csMgr := sp.GetConstellationManager()
	localNodeAddress := walletStatus.Address.NodeAddress
	isSet := walletStatus.Address.HasAddress

	// Requirements
	err := sp.RequireWalletReady(walletStatus)
	if err != nil {
		return types.ResponseStatus_WalletNotReady, err
	}

	// Refresh RP
	err = rpMgr.RefreshRocketPoolContracts()
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error refreshing Rocket Pool contracts: %w", err)
	}
	rp := rpMgr.RocketPool

	// Refresh constellation contracts
	err = csMgr.LoadContracts()
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error loading Constellation contracts: %w", err)
	}

	// Get node account
	var nodeAddress common.Address
	if c.nodeAddressStr == "0" {
		if !isSet {
			return types.ResponseStatus_AddressNotPresent, fmt.Errorf("You are trying to get vanity artifacts for the local node address, but the node address has not been set.")
		}
		nodeAddress = localNodeAddress
	} else {
		if common.IsHexAddress(c.nodeAddressStr) {
			nodeAddress = common.HexToAddress(c.nodeAddressStr)
		} else {
			return types.ResponseStatus_InvalidArguments, fmt.Errorf("%s is not a valid node address", c.nodeAddressStr)
		}
	}

	// Get some contract and ABI dependencies
	rocketMinipoolFactory, err := rp.GetContract(rocketpool.ContractName_RocketMinipoolFactory)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting MinipoolFactory contract: %w", err)
	}
	minipoolAbi, err := rp.GetAbi(rocketpool.ContractName_RocketMinipool)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting RocketMinipool ABI: %w", err)
	}

	// Get the address of rocketMinipoolBase
	rocketMinipoolBase, err := rp.GetContract(rocketpool.ContractName_RocketMinipoolBase)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error getting minipool base address: %w", err)
	}
	bytecodeString := fmt.Sprintf(ozMinipoolBytecode, utils.RemovePrefix(rocketMinipoolBase.Address.Hex()))
	bytecodeString = utils.RemovePrefix(bytecodeString)
	minipoolBytecode, err := hex.DecodeString(bytecodeString)
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error decoding minipool bytecode [%s]: %w", bytecodeString, err)
	}

	// Create the hash of the minipool constructor call
	packedConstructorArgs, err := minipoolAbi.Pack("")
	if err != nil {
		return types.ResponseStatus_Error, fmt.Errorf("error creating minipool constructor args: %w", err)
	}

	// Get the initialization data hash
	initData := append(minipoolBytecode, packedConstructorArgs...)
	initHash := crypto.Keccak256Hash(initData)

	// Update & return response
	data.SubNodeAddress = nodeAddress
	data.SuperNodeAddress = csMgr.SuperNodeAccount.Address
	data.MinipoolFactoryAddress = rocketMinipoolFactory.Address
	data.InitHash = initHash
	return types.ResponseStatus_Success, nil
}
