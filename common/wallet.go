package cscommon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
	"github.com/nodeset-org/hyperdrive-daemon/shared"

	"github.com/rocket-pool/node-manager-core/beacon"
	"github.com/rocket-pool/node-manager-core/node/validator"
	"github.com/rocket-pool/node-manager-core/node/validator/keymanager"

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

// A validator private/public key pair
type ValidatorKey struct {
	PublicKey      beacon.ValidatorPubkey
	PrivateKey     *eth2types.BLSPrivateKey
	DerivationPath string
	WalletIndex    uint64
}

// Wallet manager for the Constellation daemon
type Wallet struct {
	validatorManager *validator.ValidatorManager
	data             constellationWalletData
	sp               services.IModuleServiceProvider
}

// Create a new wallet
func NewWallet(sp services.IModuleServiceProvider, validatorPath string, keyManager keymanager.IKeyManagerClient) (*Wallet, error) {
	wallet := &Wallet{
		sp:               sp,
		validatorManager: validator.NewValidatorManager(validatorPath, keyManager),
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

// Get all keys stored on disk and/or VC
func (w *Wallet) GetStoredAndLoadedKeys(ctx context.Context, logger *slog.Logger, includeVc bool) (map[beacon.ValidatorPubkey]validator.StoredValidatorKeyInfo, error) {
	return w.validatorManager.GetAllKeys(ctx, logger, includeVc)
}

// Get the next validator key without saving it.
// You are responsible for saving it before using it for actual validation duties.
func (w *Wallet) GetNextValidatorKey() (*ValidatorKey, error) {
	// Get the path for the next validator key
	index := w.data.NextAccount
	path := fmt.Sprintf(shared.ConstellationValidatorPath, index)

	// Ask the HD daemon to generate the key
	client := w.sp.GetHyperdriveClient()
	response, err := client.Wallet.GenerateValidatorKey(path)
	if err != nil {
		return nil, fmt.Errorf("error generating validator key for path [%s]: %w", path, err)
	}

	// Decode the key
	privateKey, err := eth2types.BLSPrivateKeyFromBytes(response.Data.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("error converting BLS private key for path %s: %w", path, err)
	}
	pubkey := beacon.ValidatorPubkey(privateKey.PublicKey().Marshal())
	return &ValidatorKey{
		PublicKey:      pubkey,
		PrivateKey:     privateKey,
		DerivationPath: path,
		WalletIndex:    index,
	}, nil
}

// Save a validator key
func (w *Wallet) SaveValidatorKey(ctx context.Context, logger *slog.Logger, key *ValidatorKey) error {
	// Save the key to the VC stores
	err := w.validatorManager.StoreKey(ctx, logger, key.PrivateKey, key.DerivationPath, nil, false)
	if err != nil {
		return fmt.Errorf("error saving validator key: %w", err)
	}

	// Update the wallet data
	nextIndex := key.WalletIndex + 1
	if nextIndex > w.data.NextAccount {
		w.data.NextAccount = nextIndex
	}
	err = w.saveData()
	if err != nil {
		return fmt.Errorf("error saving wallet data: %w", err)
	}
	return nil
}

// Get the private validator key with the corresponding pubkey
func (w *Wallet) LoadValidatorKey(pubkey beacon.ValidatorPubkey) (*eth2types.BLSPrivateKey, error) {
	return w.validatorManager.LoadKey(pubkey)
}

// Recover a validator key by public key
func (w *Wallet) RecoverValidatorKey(ctx context.Context, logger *slog.Logger, pubkey beacon.ValidatorPubkey, startIndex uint64, maxAttempts uint64, slashingProtection *beacon.SlashingProtectionData, loadIntoVc bool) (uint64, error) {
	client := w.sp.GetHyperdriveClient()

	// Find matching validator key
	var index uint64
	var validatorKey *eth2types.BLSPrivateKey
	var derivationPath string
	for index = 0; index < maxAttempts; index++ {
		// Get the key from the HD daemon
		path := fmt.Sprintf(shared.ConstellationValidatorPath, index+startIndex)
		response, err := client.Wallet.GenerateValidatorKey(path)
		if err != nil {
			return 0, fmt.Errorf("error generating validator key for path [%s]: %w", path, err)
		}

		// Decode the key
		key, err := eth2types.BLSPrivateKeyFromBytes(response.Data.PrivateKey)
		if err != nil {
			return 0, fmt.Errorf("error converting BLS private key for path %s: %w", path, err)
		}

		if bytes.Equal(pubkey[:], key.PublicKey().Marshal()) {
			validatorKey = key
			derivationPath = path
			break
		}
	}

	// Check validator key
	if validatorKey == nil {
		return 0, fmt.Errorf("validator %s key not found", pubkey.Hex())
	}

	// Update account index
	nextIndex := index + startIndex + 1
	if nextIndex > w.data.NextAccount {
		w.data.NextAccount = nextIndex
	}

	// Update keystores
	err := w.validatorManager.StoreKey(ctx, logger, validatorKey, derivationPath, slashingProtection, loadIntoVc)
	if err != nil {
		return 0, fmt.Errorf("error storing validator %s key: %w", pubkey.HexWithPrefix(), err)
	}

	// Save the wallet data
	err = w.saveData()
	if err != nil {
		return 0, fmt.Errorf("error storing wallet data: %w", err)
	}

	// Return
	return index + startIndex, nil
}

// Delete a validator key from disk and unload it from the VC
func (w *Wallet) DeleteValidatorKey(ctx context.Context, logger *slog.Logger, pubkey beacon.ValidatorPubkey, deleteFromVc bool) (*beacon.SlashingProtectionData, error) {
	return w.validatorManager.DeleteKey(ctx, logger, pubkey, deleteFromVc)
}
