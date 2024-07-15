package cscommon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	"github.com/nodeset-org/hyperdrive-daemon/shared/config"
	"github.com/rocket-pool/node-manager-core/beacon"
	"github.com/rocket-pool/node-manager-core/utils"
	eth2types "github.com/wealdtech/go-eth2-types/v2"
	eth2ks "github.com/wealdtech/go-eth2-wallet-encryptor-keystorev4"
)

const (
	keystorePrefix string = "keystore-"
	keystoreSuffix string = ".json"
)

// Keystore manager for the Constellation operator
type constellationKeystoreManager struct {
	encryptor   *eth2ks.Encryptor
	keystoreDir string
	password    string
}

// Create new Constellation keystore manager
func newConstellationKeystoreManager(moduleDir string) (*constellationKeystoreManager, error) {
	keystoreDir := filepath.Join(moduleDir, config.ValidatorsDirectory, csconfig.ModuleName)
	passwordPath := filepath.Join(keystoreDir, csconfig.KeystorePasswordFile)

	// Read the password file
	var password string
	_, err := os.Stat(passwordPath)
	if errors.Is(err, fs.ErrNotExist) {
		// Make a new one
		password, err = initializeKeystorePassword(passwordPath)
		if err != nil {
			return nil, fmt.Errorf("error generating initial random validator keystore password: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("error reading keystore password from [%s]: %w", passwordPath, err)
	} else {
		bytes, err := os.ReadFile(passwordPath)
		if err != nil {
			return nil, fmt.Errorf("error reading keystore password from [%s]: %w", passwordPath, err)
		}
		password = string(bytes)
	}

	return &constellationKeystoreManager{
		encryptor:   eth2ks.New(eth2ks.WithCipher("scrypt")),
		keystoreDir: keystoreDir,
		password:    password,
	}, nil
}

// Store a validator key
func (ks *constellationKeystoreManager) StoreValidatorKey(key *eth2types.BLSPrivateKey, derivationPath string) error {
	// Get validator pubkey
	pubkey := beacon.ValidatorPubkey(key.PublicKey().Marshal())

	// Encrypt key
	encryptedKey, err := ks.encryptor.Encrypt(key.Marshal(), ks.password)
	if err != nil {
		return fmt.Errorf("could not encrypt validator key: %w", err)
	}

	// Create key store
	keyStore := beacon.ValidatorKeystore{
		Crypto:  encryptedKey,
		Version: ks.encryptor.Version(),
		UUID:    uuid.New(),
		Path:    derivationPath,
		Pubkey:  pubkey,
	}

	// Encode key store
	keyStoreBytes, err := json.Marshal(keyStore)
	if err != nil {
		return fmt.Errorf("could not encode validator key: %w", err)
	}

	// Get key file path
	keyFilePath := filepath.Join(ks.keystoreDir, keystorePrefix+pubkey.HexWithPrefix()+keystoreSuffix)

	// Write key store to disk
	if err := os.WriteFile(keyFilePath, keyStoreBytes, fileMode); err != nil {
		return fmt.Errorf("could not write validator key to disk: %w", err)
	}

	// Return
	return nil
}

// Initializes the Constellation keystore directory and saves a random password to it
func initializeKeystorePassword(passwordPath string) (string, error) {
	// Make a password
	password, err := utils.GenerateRandomPassword()
	if err != nil {
		return "", err
	}

	// Make the keystore dir
	keystoreDir := filepath.Dir(passwordPath)
	err = os.MkdirAll(keystoreDir, dirMode)
	if err != nil {
		return "", fmt.Errorf("error creating keystore directory [%s]: %w", keystoreDir, err)
	}

	err = os.WriteFile(passwordPath, []byte(password), fileMode)
	if err != nil {
		return "", fmt.Errorf("error saving password to file [%s]: %w", passwordPath, err)
	}
	return password, nil
}
