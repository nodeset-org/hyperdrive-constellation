package csapi

import csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"

type ServiceGetResourcesData struct {
	Resources *csconfig.MergedResources `json:"resources"`
}

type ServiceGetNetworkSettingsData struct {
	Settings *csconfig.ConstellationSettings `json:"settings"`
}

type ServiceGetConfigData struct {
	Config map[string]any `json:"config"`
}

type ServiceVersionData struct {
	Version string `json:"version"`
}
