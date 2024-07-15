package cscommon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
	"github.com/nodeset-org/hyperdrive-daemon/shared"

	"github.com/nodeset-org/hyperdrive-daemon/shared/config"
	"github.com/rocket-pool/node-manager-core/node/validator"

	eth2types "github.com/wealdtech/go-eth2-types/v2"
)

const (
	walletDataFilename string = "wallet_data"
)

// Data relating to Constellation's wallet
type constellationWalletData struct {
	// The next account to generate the key for
	NextAccount uint64 `json:"nextAccount"`
}

// Wallet manager for the Constellation daemon
type Wallet struct {
	validatorManager             *validator.ValidatorManager
	constellationKeystoreManager *constellationKeystoreManager
	data                         constellationWalletData
	sp                           services.IModuleServiceProvider
}

// Create a new wallet
func NewWallet(sp services.IModuleServiceProvider) (*Wallet, error) {
	moduleDir := sp.GetModuleDir()
	validatorPath := filepath.Join(moduleDir, config.ValidatorsDirectory)
	wallet := &Wallet{
		sp:               sp,
		validatorManager: validator.NewValidatorManager(validatorPath),
	}

	err := wallet.Reload()
	if err != nil {
		return nil, fmt.Errorf("error loading wallet: %w", err)
	}
	return wallet, nil
}

// Reload the wallet data from disk
func (w *Wallet) Reload() error {
	// Check if the wallet data exists
	moduleDir := w.sp.GetModuleDir()
	dataPath := filepath.Join(moduleDir, walletDataFilename)
	_, err := os.Stat(dataPath)
	if errors.Is(err, fs.ErrNotExist) {
		// No data yet, so make some
		w.data = constellationWalletData{
			NextAccount: 0,
		}

		// Save it
		err = w.saveData()
		if err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("error checking status of wallet file [%s]: %w", dataPath, err)
	} else {
		// Read it
		bytes, err := os.ReadFile(dataPath)
		if err != nil {
			return fmt.Errorf("error loading wallet data: %w", err)
		}
		var data constellationWalletData
		err = json.Unmarshal(bytes, &data)
		if err != nil {
			return fmt.Errorf("error deserializing wallet data: %w", err)
		}
		w.data = data
	}

	// Make the Constellation keystore manager
	constellationKeystoreMgr, err := newConstellationKeystoreManager(moduleDir)
	if err != nil {
		return fmt.Errorf("error creating Constellation keystore manager: %w", err)
	}
	w.constellationKeystoreManager = constellationKeystoreMgr
	return nil
}

// Write the wallet data to disk
func (w *Wallet) saveData() error {
	// Serialize it
	dataPath := filepath.Join(w.sp.GetModuleDir(), walletDataFilename)
	bytes, err := json.Marshal(w.data)
	if err != nil {
		return fmt.Errorf("error serializing wallet data: %w", err)
	}

	// Save it
	err = os.WriteFile(dataPath, bytes, fileMode)
	if err != nil {
		return fmt.Errorf("error saving wallet data: %w", err)
	}
	return nil
}

// Generate a new validator key and save it
func (w *Wallet) GenerateNewValidatorKey() (*eth2types.BLSPrivateKey, error) {
	// Get the path for the next validator key
	path := fmt.Sprintf(shared.StakeWiseValidatorPath, w.data.NextAccount)

	// Ask the HD daemon to generate the key
	client := w.sp.GetHyperdriveClient()
	response, err := client.Wallet.GenerateValidatorKey(path)
	if err != nil {
		return nil, fmt.Errorf("error generating validator key for path [%s]: %w", path, err)
	}

	// Increment the next account index first for safety
	w.data.NextAccount++
	err = w.saveData()
	if err != nil {
		return nil, err
	}

	// Save the key to the VC stores
	key, err := eth2types.BLSPrivateKeyFromBytes(response.Data.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("error converting BLS private key for path %s: %w", path, err)
	}
	err = w.validatorManager.StoreKey(key, path)
	if err != nil {
		return nil, fmt.Errorf("error saving validator key: %w", err)
	}

	// Save the key to the Constellation folder
	err = w.constellationKeystoreManager.StoreValidatorKey(key, path)
	if err != nil {
		return nil, fmt.Errorf("error saving validator key to the Constellation store: %w", err)
	}
	return key, nil
}
