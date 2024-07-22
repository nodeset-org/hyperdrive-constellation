package csconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
	hdconfig "github.com/nodeset-org/hyperdrive-daemon/shared/config"
	"github.com/rocket-pool/node-manager-core/config"
	"gopkg.in/yaml.v2"
)

var (
	// Mainnet resources for reference in testing
	MainnetResourcesReference *ConstellationResources = &ConstellationResources{
		Directory:     nil,
		RocketStorage: config.HexToAddressPtr("0x1d8f8f00cfa6758d7bE78336684788Fb0ee0Fa46"),
		FeeRecipient:  config.HexToAddressPtr("0xd4E96eF8eee8678dBFf4d535E033Ed1a4F7605b7"),
	}

	// Holesky resources for reference in testing
	HoleskyResourcesReference *ConstellationResources = &ConstellationResources{
		Directory:     nil,
		RocketStorage: config.HexToAddressPtr("0x594Fb75D3dc2DFa0150Ad03F99F97817747dd4E1"),
		FeeRecipient:  config.HexToAddressPtr("0xA347C391bc8f740CAbA37672157c8aAcD08Ac567"),
	}
)

// Network settings with a field for Constellation-specific settings
type ConstellationSettings struct {
	// The unique key used to identify the network in the configuration
	Key config.Network `yaml:"key" json:"key"`

	// Hyperdrive resources for the network
	ConstellationResources *ConstellationResources `yaml:"constellationResources" json:"constellationResources"`

	// A collection of default configuration settings to use for the network, which will override
	// the standard "general-purpose" default value for the setting
	DefaultConfigSettings map[string]any `yaml:"defaultConfigSettings,omitempty" json:"defaultConfigSettings,omitempty"`
}

// A collection of network-specific resources and getters for them
type ConstellationResources struct {
	*hdconfig.HyperdriveResources

	// The Constellation directory contract address, which houses all of the other contract addresses
	Directory *common.Address

	// Address of the RocketStorage contract, the master contract for all of Rocket Pool
	RocketStorage *common.Address

	// The fee recipient to use for the Constellation VC. This must ALWAYS be set to the Rocket Pool Smoothing Pool contract address.
	// Technically this should come from Directory (or RocketStorage within Directory) but it needs to be set here for templating to use it.
	FeeRecipient *common.Address
}

// A merged set of general resources and Constellation-specific resources for the selected network
type MergedResources struct {
	// General resources
	*hdconfig.MergedResources

	// Constellation-specific resources
	*ConstellationResources
}

// Load network settings from a folder
func LoadSettingsFiles(sourceDir string) ([]*ConstellationSettings, error) {
	// Make sure the folder exists
	_, err := os.Stat(sourceDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("network settings folder [%s] does not exist", sourceDir)
	}

	// Enumerate the dir
	files, err := os.ReadDir(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("error enumerating network settings source folder: %w", err)
	}

	settingsList := []*ConstellationSettings{}
	for _, file := range files {
		// Ignore dirs and nonstandard files
		if file.IsDir() || !file.Type().IsRegular() {
			continue
		}

		// Load the file
		filename := file.Name()
		ext := filepath.Ext(filename)
		if ext != ".yaml" && ext != ".yml" {
			// Only load YAML files
			continue
		}
		settingsFilePath := filepath.Join(sourceDir, filename)
		bytes, err := os.ReadFile(settingsFilePath)
		if err != nil {
			return nil, fmt.Errorf("error reading network settings file [%s]: %w", settingsFilePath, err)
		}

		// Unmarshal the settings
		settings := new(ConstellationSettings)
		err = yaml.Unmarshal(bytes, settings)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling network settings file [%s]: %w", settingsFilePath, err)
		}
		settingsList = append(settingsList, settings)
	}
	return settingsList, nil
}
