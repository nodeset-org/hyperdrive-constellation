package cscommon

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/node-manager-core/wallet"
)

var (
	ErrNotRegisteredWithConstellation error = errors.New("The node is not registered with Constellation yet.")
)

// Makes sure the node is registered with Constellation.
// If useWalletAddress is true, the wallet address will be used to check registration.
// If false, the node address will be used.
func (p *constellationServiceProvider) RequireRegisteredWithConstellation(ctx context.Context, walletStatus wallet.WalletStatus, useWalletAddress bool) error {
	// Make sure there's a node address or wallet loaded
	var address common.Address
	if useWalletAddress {
		err := p.RequireNodeAddress(walletStatus)
		if err != nil {
			return err
		}
		address = walletStatus.Address.NodeAddress
	} else {
		err := p.RequireWalletReady(walletStatus)
		if err != nil {
			return err
		}
		address = walletStatus.Wallet.WalletAddress
	}

	// Make sure the EC is synced
	err := p.RequireEthClientSynced(ctx)
	if err != nil {
		return err
	}

	// Load the Constellation contracts
	err = p.csMgr.LoadContracts()
	if err != nil {
		return fmt.Errorf("error loading Constellation contracts: %w", err)
	}

	// Check if the node is registered with Constellation
	qMgr := p.GetQueryManager()
	var registered bool
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		p.csMgr.Whitelist.IsAddressInWhitelist(mc, &registered, address)
		return nil
	}, nil)
	if err != nil {
		return fmt.Errorf("error checking if address is in whitelist: %w", err)
	}
	if !registered {
		return ErrNotRegisteredWithConstellation
	}
	return nil
}
