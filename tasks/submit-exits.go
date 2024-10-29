package cstasks

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	"github.com/nodeset-org/hyperdrive-constellation/shared/keys"
	nscommon "github.com/nodeset-org/nodeset-client-go/common"
	"github.com/rocket-pool/node-manager-core/beacon"
	"github.com/rocket-pool/node-manager-core/log"
	"github.com/rocket-pool/node-manager-core/node/validator"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
	eth2types "github.com/wealdtech/go-eth2-types/v2"
)

// Submit signed exits task
type SubmitSignedExitsTask struct {
	sp                cscommon.IConstellationServiceProvider
	logger            *slog.Logger
	ctx               context.Context
	cfg               *csconfig.ConstellationConfig
	res               *csconfig.MergedResources
	w                 *cscommon.Wallet
	csMgr             *cscommon.ConstellationManager
	rpMgr             *cscommon.RocketPoolManager
	bc                beacon.IBeaconClient
	beaconCfg         *beacon.Eth2Config
	initialized       bool
	registeredAddress *common.Address

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
	hd := t.sp.GetHyperdriveClient()
	if !t.initialized {
		validatorsResponse, err := hd.NodeSet_Constellation.GetValidators(t.res.DeploymentName)
		if err != nil {
			return fmt.Errorf("error getting validators from NodeSet: %w", err)
		}
		if validatorsResponse.Data.NotRegistered {
			t.logger.Debug("Node is not registered with NodeSet, can't send signed exits")
			return nil
		}
		if validatorsResponse.Data.NotWhitelisted {
			t.logger.Debug("User doesn't have a node registered with Constellation yet, can't send signed exits")
			return nil
		}
		if validatorsResponse.Data.IncorrectNodeAddress {
			t.logger.Warn("Your user account has a different node whitelisted for Constellation, can't send signed exits")
			return nil
		}
		if validatorsResponse.Data.InvalidPermissions {
			t.logger.Warn("Your user account does not have the correct permissions for Constellation, can't send signed exits")
			return nil
		}
		for _, validator := range validatorsResponse.Data.Validators {
			if !validator.RequiresExitMessage {
				t.signedExitsSent[beacon.ValidatorPubkey(validator.Pubkey)] = true
			}
		}
		t.initialized = true
	}

	// Get the registered address from the server
	if t.registeredAddress == nil {
		response, err := hd.NodeSet_Constellation.GetRegisteredAddress(t.res.DeploymentName)
		if err != nil {
			return fmt.Errorf("error getting registered address from NodeSet: %w", err)
		}
		if response.Data.NotRegisteredWithNodeSet {
			t.logger.Debug("Node is not registered with NodeSet, can't send signed exits")
			return nil
		}
		if response.Data.NotRegisteredWithConstellation {
			t.logger.Debug("User doesn't have a node registered with Constellation yet, can't send signed exits")
			return nil
		}
		if response.Data.InvalidPermissions {
			t.logger.Warn("User account does not have the correct permissions for Constellation, can't send signed exits")
			return nil
		}
		t.registeredAddress = &response.Data.RegisteredAddress
	}

	// Make sure it matches the Constellation node registeredAddress
	registeredAddress := *t.registeredAddress
	if registeredAddress != snapshot.ConstellationNode.NodeAddress {
		t.logger.Info("NodeSet registered address doesn't match Constellation node address, can't send signed exits",
			slog.String("registeredAddress", registeredAddress.Hex()),
			slog.String("nodeAddress", snapshot.ConstellationNode.NodeAddress.Hex()),
		)
		return nil
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
	eligibleMinipools, exitMessages, err := t.getSignedExits(snapshot, requiredMinipools)
	if err != nil {
		return fmt.Errorf("error getting signed exits: %w", err)
	}
	if len(exitMessages) == 0 {
		return nil
	}

	// Upload signed exits to NodeSet
	err = t.uploadSignedExits(eligibleMinipools, exitMessages)
	if err != nil {
		return fmt.Errorf("error uploading signed exits: %w", err)
	}

	// Return
	return nil
}

// Get minipools that are eligible for signed exit submission
func (t *SubmitSignedExitsTask) getSignedExits(snapshot *NetworkSnapshot, minipools []minipool.IMinipool) ([]minipool.IMinipool, []nscommon.EncryptedExitData, error) {
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
		return nil, nil, fmt.Errorf("error getting validator statuses: %w", err)
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
		return nil, nil, nil
	}

	// Get Beacon details for exiting
	head, err := t.bc.GetBeaconHead(t.ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("error getting beacon head: %w", err)
	}
	epoch := head.FinalizedEpoch // Use the latest finalized epoch for the exit
	signatureDomain, err := t.bc.GetDomainData(t.ctx, eth2types.DomainVoluntaryExit[:], epoch, false)
	if err != nil {
		return nil, nil, fmt.Errorf("error getting domain data: %w", err)
	}

	// Get the signed exits
	exitData := []nscommon.EncryptedExitData{}
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
		exitMessage := nscommon.ExitMessage{
			Message: nscommon.ExitMessageDetails{
				Epoch:          strconv.FormatUint(epoch, 10),
				ValidatorIndex: index,
			},
			Signature: signature.HexWithPrefix(),
		}

		// Encrypt it
		encryptedMessage, err := nscommon.EncryptSignedExitMessage(exitMessage, t.res.EncryptionPubkey)
		if err != nil {
			t.logger.Warn("Error encrypting signed exit message",
				slog.String("minipool", mp.Common().Address.Hex()),
				slog.String("pubkey", pubkey.HexWithPrefix()),
				log.Err(err),
			)
			continue
		}

		exitData = append(exitData, nscommon.EncryptedExitData{
			Pubkey:      pubkey.HexWithPrefix(),
			ExitMessage: encryptedMessage,
		})
		t.logger.Debug("Signed exit message created",
			slog.String("minipool", mp.Common().Address.Hex()),
			slog.String("pubkey", pubkey.HexWithPrefix()),
		)
	}

	return eligibleMinipools, exitData, nil
}

// Upload signed exits to NodeSet
func (t *SubmitSignedExitsTask) uploadSignedExits(eligibleMinipools []minipool.IMinipool, exitMessages []nscommon.EncryptedExitData) error {
	hd := t.sp.GetHyperdriveClient()
	uploadResponse, err := hd.NodeSet_Constellation.UploadSignedExits(t.res.DeploymentName, exitMessages)
	if err != nil {
		return fmt.Errorf("error uploading signed exits: %w", err)
	}
	if uploadResponse.Data.NotRegistered {
		return fmt.Errorf("node is not registered with nodeset, can't send signed exits")
	}
	if uploadResponse.Data.NotWhitelisted {
		return fmt.Errorf("node has not been whitelisted for Constellation usage, can't send signed exits")
	}
	if uploadResponse.Data.IncorrectNodeAddress {
		return fmt.Errorf("your user account has a different node registered for Constellation, can't send signed exits")
	}
	if uploadResponse.Data.InvalidValidatorOwner {
		return fmt.Errorf("your node does not own the validator for one of these exit messages, can't send signed exits")
	}
	if uploadResponse.Data.InvalidExitMessage {
		return fmt.Errorf("one of the exit messages is invalid, can't send signed exits")
	}
	if uploadResponse.Data.InvalidPermissions {
		return fmt.Errorf("your user account does not have the correct permissions to upload signed exit messages for Constellation, can't send signed exits")
	}
	if uploadResponse.Data.ExitMessageAlreadyExists {
		t.logger.Warn("One of the validators already has a signed exit message uploaded (probably manually), refreshing the cache...")
		origMessageCount := len(exitMessages)

		// Signed exits were probably submitted manually so update the cache
		validatorsResponse, err := hd.NodeSet_Constellation.GetValidators(t.res.DeploymentName)
		if err != nil {
			return fmt.Errorf("error getting validators from NodeSet: %w", err)
		}
		for _, validator := range validatorsResponse.Data.Validators {
			// Ignore validators that still require an exit message
			if validator.RequiresExitMessage {
				continue
			}

			// Ignore validators that have already been marked as submitted
			_, alreadyMarked := t.signedExitsSent[beacon.ValidatorPubkey(validator.Pubkey)]
			if alreadyMarked {
				continue
			}

			// Mark this as uploaded
			t.logger.Info("New signed exit found", slog.String("pubkey", validator.Pubkey.HexWithPrefix()))
			t.signedExitsSent[beacon.ValidatorPubkey(validator.Pubkey)] = true

			// Remove it from the eligible minipool list
			newEligibleMinipools := []minipool.IMinipool{}
			for _, mp := range eligibleMinipools {
				if mp.Common().Pubkey.Get() != validator.Pubkey {
					newEligibleMinipools = append(newEligibleMinipools, mp)
				}
			}
			eligibleMinipools = newEligibleMinipools

			// Remove it from the exit messages
			newExitMessages := []nscommon.EncryptedExitData{}
			for _, exitMessage := range exitMessages {
				if exitMessage.Pubkey != validator.Pubkey.HexWithPrefix() {
					newExitMessages = append(newExitMessages, exitMessage)
				}
			}
			exitMessages = newExitMessages
		}

		// Check if any signed exits were removed
		if len(exitMessages) == origMessageCount {
			return fmt.Errorf("nodeset reported a signed exit was already uploaded but no signed exits were removed after regenerating the cache")
		}
		if len(exitMessages) == 0 {
			t.logger.Info("All pending signed exits were already uploaded, no new signed exits required")
			return nil
		}

		// Try again
		return t.uploadSignedExits(eligibleMinipools, exitMessages)
	}
	t.logger.Debug("Signed exits uploaded to NodeSet")

	// Get the validators to make sure they're marked as submitted
	for _, mp := range eligibleMinipools {
		// Find it in the exit messages
		found := false
		mpCommon := mp.Common()
		pubkey := mpCommon.Pubkey.Get()
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

		t.signedExitsSent[pubkey] = true
		t.logger.Info("Validator exit message successfully submitted and stored on the NodeSet server",
			slog.String("pubkey", pubkey.HexWithPrefix()),
		)
	}
	return nil
}
