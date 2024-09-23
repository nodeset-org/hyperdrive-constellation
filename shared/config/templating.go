package csconfig

import (
	"fmt"

	"github.com/rocket-pool/node-manager-core/config"
)

func (c *ConstellationConfig) DaemonContainerName() string {
	return string(ContainerID_ConstellationDaemon)
}

func (c *ConstellationConfig) VcContainerName() string {
	return string(ContainerID_ConstellationValidator)
}

// The tag for the daemon container
func (cfg *ConstellationConfig) GetDaemonContainerTag() string {
	return cfg.DaemonContainerTag.Value
}

// Get the container tag of the selected VC
func (cfg *ConstellationConfig) GetVcContainerTag() string {
	bn := cfg.hdCfg.GetSelectedBeaconNode()
	switch bn {
	case config.BeaconNode_Lighthouse:
		return cfg.Lighthouse.ContainerTag.Value
	case config.BeaconNode_Lodestar:
		return cfg.Lodestar.ContainerTag.Value
	case config.BeaconNode_Nimbus:
		return cfg.Nimbus.ContainerTag.Value
	case config.BeaconNode_Prysm:
		return cfg.Prysm.ContainerTag.Value
	case config.BeaconNode_Teku:
		return cfg.Teku.ContainerTag.Value
	default:
		panic(fmt.Sprintf("Unknown Beacon Node %s", bn))
	}
}

// Gets the additional flags of the selected VC
func (cfg *ConstellationConfig) GetVcAdditionalFlags() string {
	bn := cfg.hdCfg.GetSelectedBeaconNode()
	switch bn {
	case config.BeaconNode_Lighthouse:
		return cfg.Lighthouse.AdditionalFlags.Value
	case config.BeaconNode_Lodestar:
		return cfg.Lodestar.AdditionalFlags.Value
	case config.BeaconNode_Nimbus:
		return cfg.Nimbus.AdditionalFlags.Value
	case config.BeaconNode_Prysm:
		return cfg.Prysm.AdditionalFlags.Value
	case config.BeaconNode_Teku:
		return cfg.Teku.AdditionalFlags.Value
	default:
		panic(fmt.Sprintf("Unknown Beacon Node %s", bn))
	}
}

// Check if any of the services have doppelganger detection enabled
// NOTE: update this with each new service that runs a VC!
func (cfg *ConstellationConfig) IsDoppelgangerEnabled() bool {
	return cfg.VcCommon.DoppelgangerDetection.Value
}

// Used by text/template to format validator.yml
func (cfg *ConstellationConfig) Graffiti() (string, error) {
	prefix := cfg.hdCfg.GraffitiPrefix()
	customGraffiti := cfg.VcCommon.Graffiti.Value
	if customGraffiti == "" {
		return prefix, nil
	}
	return fmt.Sprintf("%s (%s)", prefix, customGraffiti), nil
}

func (cfg *ConstellationConfig) IsEnabled() bool {
	return cfg.Enabled.Value
}
