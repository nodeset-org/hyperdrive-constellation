package cscommon

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/rocket-pool/node-manager-core/beacon"
)

const (
	GraffitiFileMode os.FileMode = 0644
)

// Manages graffiti on the VC and in the graffiti file
type GraffitiManager struct {
	sp IConstellationServiceProvider
}

// Creates a new graffiti manager
func NewGraffitiManager(sp IConstellationServiceProvider) *GraffitiManager {
	return &GraffitiManager{
		sp: sp,
	}
}

// Updates the graffiti file with the current graffiti
func (m *GraffitiManager) UpdateGraffitiFile(logger *slog.Logger) error {
	// Get the path of the graffiti file
	moduleDir := m.sp.GetModuleDir()
	cfg := m.sp.GetConfig()
	graffitiPath := cfg.GetFullGraffitiPath(moduleDir)

	// Get the graffiti
	graffiti := cfg.Graffiti()
	logger.Debug("Updating graffiti file",
		"path", graffitiPath,
		"graffiti", graffiti,
	)

	// Write the graffiti to the file
	err := os.WriteFile(graffitiPath, []byte(graffiti), GraffitiFileMode)
	if err != nil {
		return fmt.Errorf("Failed to write graffiti to file [%s]: %w", graffitiPath, err)
	}
	logger.Debug("Graffiti file updated")
	return nil
}

// Updates the graffiti used in the VC key manager by each of the provided keys
func (m *GraffitiManager) UpdateGraffitiInVc(ctx context.Context, logger *slog.Logger, keys []beacon.ValidatorPubkey) error {
	// Get the graffiti
	graffiti := m.sp.GetConfig().Graffiti()

	// Get the keymanager
	keyManager := m.sp.GetKeyManagerClient()
	logger.Debug("Updating graffiti in VC",
		"graffiti", graffiti,
		"keyCount", len(keys),
	)

	// Update the graffiti for each key
	for _, key := range keys {
		err := keyManager.SetGraffitiForValidator(ctx, logger, key, graffiti)
		if err != nil {
			return fmt.Errorf("Failed to update graffiti for key [%s]: %w", key.HexWithPrefix(), err)
		}
	}
	logger.Debug("Graffiti updated in VC")
	return nil
}
