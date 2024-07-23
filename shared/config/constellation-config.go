package csconfig

import (
	"fmt"

	"github.com/nodeset-org/hyperdrive-constellation/shared"
	"github.com/nodeset-org/hyperdrive-constellation/shared/config/ids"
	hdconfig "github.com/nodeset-org/hyperdrive-daemon/shared/config"
	hdids "github.com/nodeset-org/hyperdrive-daemon/shared/config/ids"
	"github.com/rocket-pool/node-manager-core/config"
)

const (
	// Tags
	daemonTag string = "nodeset/hyperdrive-constellation:v" + shared.ConstellationVersion
)

// Configuration for Constellation
type ConstellationConfig struct {
	// Toggle for enabling the module
	Enabled config.Parameter[bool]

	// Port to run the Constellation API server on
	ApiPort config.Parameter[uint16]

	// The Docker Hub tag for the Constellation daemon
	DaemonContainerTag config.Parameter[string]

	// Validator client configs
	VcCommon   *config.ValidatorClientCommonConfig
	Lighthouse *config.LighthouseVcConfig
	Lodestar   *config.LodestarVcConfig
	Nimbus     *config.NimbusVcConfig
	Prysm      *config.PrysmVcConfig
	Teku       *config.TekuVcConfig

	// Internal fields
	Version         string
	hdCfg           *hdconfig.HyperdriveConfig
	networkSettings []*ConstellationSettings
}

// Generates a new Constellation config
func NewConstellationConfig(hdCfg *hdconfig.HyperdriveConfig, networks []*ConstellationSettings) (*ConstellationConfig, error) {
	cfg := &ConstellationConfig{
		hdCfg:           hdCfg,
		networkSettings: networks,

		Enabled: config.Parameter[bool]{
			ParameterCommon: &config.ParameterCommon{
				ID:                 ids.ConstellationEnableID,
				Name:               "Enable",
				Description:        "Enable support for Constellation (see more at https://docs.nodeset.io).",
				AffectsContainers:  []config.ContainerID{ContainerID_ConstellationDaemon, ContainerID_ConstellationValidator},
				CanBeBlank:         false,
				OverwriteOnUpgrade: false,
			},
			Default: map[config.Network]bool{
				config.Network_All: false,
			},
		},

		ApiPort: config.Parameter[uint16]{
			ParameterCommon: &config.ParameterCommon{
				ID:                 ids.ApiPortID,
				Name:               "Daemon API Port",
				Description:        "The port that the Constellation daemon's API server should run on. Note this is bound to the local machine only; it cannot be accessed by other machines.",
				AffectsContainers:  []config.ContainerID{ContainerID_ConstellationDaemon},
				CanBeBlank:         false,
				OverwriteOnUpgrade: false,
			},
			Default: map[config.Network]uint16{
				config.Network_All: DefaultApiPort,
			},
		},

		DaemonContainerTag: config.Parameter[string]{
			ParameterCommon: &config.ParameterCommon{
				ID:                 ids.DaemonContainerTagID,
				Name:               "Daemon Container Tag",
				Description:        "The tag name of Hyperdrive's Constellation Daemon image to use.",
				AffectsContainers:  []config.ContainerID{ContainerID_ConstellationDaemon},
				CanBeBlank:         false,
				OverwriteOnUpgrade: true,
			},
			Default: map[config.Network]string{
				config.Network_All: daemonTag,
			},
		},
	}

	cfg.VcCommon = config.NewValidatorClientCommonConfig()
	cfg.Lighthouse = config.NewLighthouseVcConfig()
	cfg.Lodestar = config.NewLodestarVcConfig()
	cfg.Nimbus = config.NewNimbusVcConfig()
	cfg.Prysm = config.NewPrysmVcConfig()
	cfg.Teku = config.NewTekuVcConfig()

	// Provision the defaults for each network
	for _, network := range networks {
		err := config.SetDefaultsForNetworks(cfg, network.DefaultConfigSettings, network.Key)
		if err != nil {
			return nil, fmt.Errorf("could not set defaults for network %s: %w", network.Key, err)
		}
	}

	// Apply the default values for the current network
	config.ApplyDefaults(cfg, hdCfg.Network.Value)
	return cfg, nil
}

// The title for the config
func (cfg *ConstellationConfig) GetTitle() string {
	return "Constellation"
}

// Get the parameters for this config
func (cfg *ConstellationConfig) GetParameters() []config.IParameter {
	return []config.IParameter{
		&cfg.Enabled,
		&cfg.ApiPort,
		&cfg.DaemonContainerTag,
	}
}

// Get the sections underneath this one
func (cfg *ConstellationConfig) GetSubconfigs() map[string]config.IConfigSection {
	return map[string]config.IConfigSection{
		ids.VcCommonID:   cfg.VcCommon,
		ids.LighthouseID: cfg.Lighthouse,
		ids.LodestarID:   cfg.Lodestar,
		ids.NimbusID:     cfg.Nimbus,
		ids.PrysmID:      cfg.Prysm,
		ids.TekuID:       cfg.Teku,
	}
}

// Changes the current network, propagating new parameter settings if they are affected
func (cfg *ConstellationConfig) ChangeNetwork(oldNetwork config.Network, newNetwork config.Network) {
	// Run the changes
	config.ChangeNetwork(cfg, oldNetwork, newNetwork)
}

// Creates a copy of the configuration
func (cfg *ConstellationConfig) Clone() hdconfig.IModuleConfig {
	clone, _ := NewConstellationConfig(cfg.hdCfg, cfg.networkSettings)
	config.Clone(cfg, clone, cfg.hdCfg.Network.Value)
	clone.Version = cfg.Version
	return clone
}

// Updates the default parameters based on the current network value
func (cfg *ConstellationConfig) UpdateDefaults(network config.Network) {
	config.UpdateDefaults(cfg, network)
}

// Checks to see if the current configuration is valid; if not, returns a list of errors
func (cfg *ConstellationConfig) Validate() []string {
	errors := []string{}
	return errors
}

// Serialize the module config to a map
func (cfg *ConstellationConfig) Serialize() map[string]any {
	cfgMap := config.Serialize(cfg)
	cfgMap[hdids.VersionID] = cfg.Version
	return cfgMap
}

// Deserialize the module config from a map
func (cfg *ConstellationConfig) Deserialize(configMap map[string]any, network config.Network) error {
	err := config.Deserialize(cfg, configMap, network)
	if err != nil {
		return err
	}
	version, exists := configMap[hdids.VersionID]
	if !exists {
		// Handle pre-version configs
		version = "0.0.1"
	}
	cfg.Version = version.(string)
	return nil
}

// Get the version of the module config
func (cfg *ConstellationConfig) GetVersion() string {
	return cfg.Version
}

// Get all loaded network settings
func (cfg *ConstellationConfig) GetNetworkSettings() []*ConstellationSettings {
	return cfg.networkSettings
}

// ===================
// === Module Info ===
// ===================

func (cfg *ConstellationConfig) GetHdClientLogFileName() string {
	return ClientLogName
}

func (cfg *ConstellationConfig) GetApiLogFileName() string {
	return hdconfig.ApiLogName
}

func (cfg *ConstellationConfig) GetTasksLogFileName() string {
	return hdconfig.TasksLogName
}

func (cfg *ConstellationConfig) GetLogNames() []string {
	return []string{
		cfg.GetHdClientLogFileName(),
		cfg.GetApiLogFileName(),
		cfg.GetTasksLogFileName(),
	}
}

// The module name
func (cfg *ConstellationConfig) GetModuleName() string {
	return ModuleName
}

// The module name
func (cfg *ConstellationConfig) GetShortName() string {
	return ShortModuleName
}

func (cfg *ConstellationConfig) GetValidatorContainerTagInfo() map[config.ContainerID]string {
	return map[config.ContainerID]string{
		ContainerID_ConstellationValidator: cfg.GetVcContainerTag(),
	}
}

func (cfg *ConstellationConfig) GetContainersToDeploy() []config.ContainerID {
	return []config.ContainerID{
		ContainerID_ConstellationDaemon,
		ContainerID_ConstellationValidator,
	}
}
