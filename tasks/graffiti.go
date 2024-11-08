package cstasks

import (
	"fmt"
	"os"

	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
)

const (
	GraffitiFileMode os.FileMode = 0644
)

// Updates the graffiti file with the current graffiti
func UpdateGraffitiFile(sp cscommon.IConstellationServiceProvider) error {
	// Get the path of the graffiti file
	moduleDir := sp.GetModuleDir()
	cfg := sp.GetConfig()
	graffitiPath := cfg.GetFullGraffitiPath(moduleDir)

	// Get the graffiti
	graffiti := cfg.Graffiti()

	// Write the graffiti to the file
	err := os.WriteFile(graffitiPath, []byte(graffiti), GraffitiFileMode)
	if err != nil {
		return fmt.Errorf("Failed to write graffiti to file [%s]: %w", graffitiPath, err)
	}
	return nil
}
