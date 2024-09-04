package cstasks

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	"github.com/nodeset-org/hyperdrive-constellation/shared/keys"
	nscommon "github.com/nodeset-org/nodeset-client-go/common"
	"github.com/rocket-pool/node-manager-core/beacon"
	"github.com/rocket-pool/node-manager-core/log"
	"github.com/rocket-pool/node-manager-core/node/validator"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
	"github.com/rocket-pool/rocketpool-go/v2/rocketpool"
	eth2types "github.com/wealdtech/go-eth2-types/v2"
)

// Submit signed exits task
type SubmitSignedExitsTask struct {
	sp          cscommon.IConstellationServiceProvider
	logger      *slog.Logger
	ctx         context.Context
	cfg         *csconfig.ConstellationConfig
	res         *csconfig.MergedResources
	w           *cscommon.Wallet
	csMgr       *cscommon.ConstellationManager
	rpMgr       *cscommon.RocketPoolManager
	rp          *rocketpool.RocketPool
	bc          beacon.IBeaconClient
	beaconCfg   *beacon.Eth2Config
	initialized bool

	// Cache of minipools that have had signed exits sent to NodeSet
	signedExitsSent map[beacon.ValidatorPubkey]bool
}

// Create a submit signed exits task
func NewSubmitSignedExitsTask(ctx context.Context, sp cscommon.IConstellationServiceProvider, logger *log.Logger) *SubmitSignedExitsTask {
	log := logger.With(slog.String(keys.TaskKey, "Submit Signed Exits"))
	return &SubmitSignedExitsTask{
		ctx:             ctx,
		sp:              sp,
		logger:          log,
		cfg:             sp.GetConfig(),
		res:             sp.GetResources(),
		w:               sp.GetWallet(),
		csMgr:           sp.GetConstellationManager(),
		rpMgr:           sp.GetRocketPoolManager(),
		bc:              sp.GetBeaconClient(),
		signedExitsSent: make(map[beacon.ValidatorPubkey]bool),
	}
}

// Submit signed exits
func (t *SubmitSignedExitsTask) Run(snapshot *NetworkSnapshot) error {
	// Log
	t.logger.Info("Checking for required signed exit submissions...")

	// Get the Beacon config
	if t.beaconCfg == nil {
		cfg, err := t.bc.GetEth2Config(t.ctx)
		if err != nil {
			return fmt.Errorf("error getting Beacon config: %w", err)
		}
		t.beaconCfg = &cfg
	}

	// Initialize the signed exits cache
	if !t.initialized {
		hd := t.sp.GetHyperdriveClient()
		validatorsResponse, err := hd.NodeSet_Constellation.GetValidators()
		if err != nil {
			return fmt.Errorf("error getting validators from NodeSet: %w", err)
		}
		for _, validator := range validatorsResponse.Data.Validators {
			if validator.ExitMessageUploaded {
				t.signedExitsSent[beacon.ValidatorPubkey(validator.Pubkey)] = true
			}
		}
		t.initialized = true
	}

	// Get minipools that haven't had exits submitted yet
	requiredMinipools := []minipool.IMinipool{}
	for _, mp := range snapshot.ConstellationNode.Minipools {
		pubkey := mp.Common().Pubkey.Get()
		_, exists := t.signedExitsSent[pubkey]
		if !exists {
			requiredMinipools = append(requiredMinipools, mp)
		}
	}
	if len(requiredMinipools) == 0 {
		return nil
	}

	// Get signed exits for eligible minipools
	exitMessages, err := t.getSignedExits(snapshot, requiredMinipools)
	if err != nil {
		return fmt.Errorf("error getting signed exits: %w", err)
	}
	if len(exitMessages) == 0 {
		return nil
	}

	// Upload signed exits to NodeSet
	err = t.uploadSignedExits(exitMessages)
	if err != nil {
		return fmt.Errorf("error uploading signed exits: %w", err)
	}

	// Return
	return nil
}

// Get minipools that are eligible for signed exit submission
func (t *SubmitSignedExitsTask) getSignedExits(snapshot *NetworkSnapshot, minipools []minipool.IMinipool) ([]nscommon.ExitData, error) {
	// Get the slot to check on Beacon
	blockTimeUnix := snapshot.ExecutionBlockHeader.Time
	slotSeconds := blockTimeUnix - t.beaconCfg.GenesisTime
	slot := slotSeconds / t.beaconCfg.SecondsPerSlot

	// Check the minipool status on Beacon
	opts := &beacon.ValidatorStatusOptions{
		Slot: &slot,
	}
	pubkeys := make([]beacon.ValidatorPubkey, len(minipools))
	for i, mp := range minipools {
		pubkeys[i] = mp.Common().Pubkey.Get()
	}
	statuses, err := t.bc.GetValidatorStatuses(t.ctx, pubkeys, opts)
	if err != nil {
		return nil, fmt.Errorf("error getting validator statuses: %w", err)
	}

	// Filter on beacon status
	eligibleMinipools := []minipool.IMinipool{}
	for _, mp := range minipools {
		// Ignore minipools that aren't on Beacon yet
		pubkey := mp.Common().Pubkey.Get()
		status, exists := statuses[pubkey]
		if !exists {
			t.logger.Debug("Validator hasn't been seen by Beacon yet",
				slog.String("minipool", mp.Common().Address.Hex()),
				slog.String("pubkey", pubkey.HexWithPrefix()),
			)
			continue
		}

		// Ignore minipools that haven't been assigned an index yet
		if status.Index == "" {
			t.logger.Debug("Validator doesn't have an index yet",
				slog.String("minipool", mp.Common().Address.Hex()),
				slog.String("pubkey", pubkey.HexWithPrefix()),
			)
			continue
		}
		t.logger.Info("Validator is eligible for signed exit submission",
			slog.String("minipool", mp.Common().Address.Hex()),
			slog.String("pubkey", pubkey.HexWithPrefix()),
		)
		eligibleMinipools = append(eligibleMinipools, mp)
	}
	if len(eligibleMinipools) == 0 {
		return nil, nil
	}

	// Get Beacon details for exiting
	head, err := t.bc.GetBeaconHead(t.ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting beacon head: %w", err)
	}
	epoch := head.FinalizedEpoch // Use the latest finalized epoch for the exit
	signatureDomain, err := t.bc.GetDomainData(t.ctx, eth2types.DomainVoluntaryExit[:], epoch, false)
	if err != nil {
		return nil, fmt.Errorf("error getting domain data: %w", err)
	}

	// Get the signed exits
	exitData := []nscommon.ExitData{}
	for _, mp := range eligibleMinipools {
		pubkey := mp.Common().Pubkey.Get()
		index := statuses[pubkey].Index

		// Load the private key
		key, err := t.w.LoadValidatorKey(pubkey)
		if err != nil {
			t.logger.Warn("Error getting validator private key",
				slog.String("minipool", mp.Common().Address.Hex()),
				slog.String("pubkey", pubkey.HexWithPrefix()),
				log.Err(err),
			)
			continue
		}
		if key == nil {
			t.logger.Warn("Validator private key not found on disk",
				slog.String("minipool", mp.Common().Address.Hex()),
				slog.String("pubkey", pubkey.HexWithPrefix()),
			)
			continue
		}

		// Make a signed exit
		signature, err := validator.GetSignedExitMessage(key, index, epoch, signatureDomain)
		if err != nil {
			t.logger.Warn("Error getting signed exit message",
				slog.String("minipool", mp.Common().Address.Hex()),
				slog.String("pubkey", pubkey.HexWithPrefix()),
				log.Err(err),
			)
			continue
		}
		exitData = append(exitData, nscommon.ExitData{
			Pubkey: pubkey.HexWithPrefix(),
			ExitMessage: nscommon.ExitMessage{
				Message: nscommon.ExitMessageDetails{
					Epoch:          strconv.FormatUint(epoch, 10),
					ValidatorIndex: index,
				},
				Signature: signature.HexWithPrefix(),
			},
		})
		t.logger.Debug("Signed exit message created",
			slog.String("minipool", mp.Common().Address.Hex()),
			slog.String("pubkey", pubkey.HexWithPrefix()),
		)
	}

	return exitData, nil
}

// Upload signed exits to NodeSet
func (t *SubmitSignedExitsTask) uploadSignedExits(exitMessages []nscommon.ExitData) error {
	hd := t.sp.GetHyperdriveClient()
	uploadResponse, err := hd.NodeSet_Constellation.UploadSignedExits(exitMessages)
	if err != nil {
		return fmt.Errorf("error uploading signed exits: %w", err)
	}

	if uploadResponse.Data.NotRegistered {
		return fmt.Errorf("node is not registered with nodeset, can't send signed exits")
	}
	if uploadResponse.Data.NotAuthorized {
		return fmt.Errorf("node is not authorized for constellation usage, can't send signed exits")
	}
	t.logger.Debug("Signed exits uploaded to NodeSet")

	// Get the validators to make sure they're marked as submitted
	validatorsResponse, err := hd.NodeSet_Constellation.GetValidators()
	if err != nil {
		return fmt.Errorf("error getting validators from NodeSet: %w", err)
	}
	for _, validator := range validatorsResponse.Data.Validators {
		// Find it in the exit messages
		found := false
		pubkey := validator.Pubkey
		for _, exitMessage := range exitMessages {
			if exitMessage.Pubkey == pubkey.HexWithPrefix() {
				found = true
				break
			}
		}
		if !found {
			t.logger.Warn("Validator exit message still missing according to NodeSet and wasn't submitted in this round",
				slog.String("pubkey", pubkey.HexWithPrefix()),
			)
			continue
		}

		if !validator.ExitMessageUploaded {
			t.logger.Warn("Validator exit message was submitted to NodeSet but wasn't stored on the server",
				slog.String("pubkey", pubkey.HexWithPrefix()),
			)
			continue
		}

		t.signedExitsSent[pubkey] = true
		t.logger.Info("Validator exit message successfully submitted and stored on the NodeSet server",
			slog.String("pubkey", pubkey.HexWithPrefix()),
		)
	}
	return nil
}
