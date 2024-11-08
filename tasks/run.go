package cstasks

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	"github.com/nodeset-org/hyperdrive-constellation/shared/keys"
	"github.com/nodeset-org/hyperdrive-daemon/shared/types/api"
	"github.com/rocket-pool/node-manager-core/beacon"
	"github.com/rocket-pool/node-manager-core/log"
	"github.com/rocket-pool/node-manager-core/utils"
	"github.com/rocket-pool/node-manager-core/wallet"
)

// Config
const (
	// Time to wait after finishing tasks before starting the next iteration
	tasksInterval time.Duration = time.Minute * 5

	// Time between individual tasks
	taskCooldown time.Duration = time.Second

	// Time to wait if the tasks loop isn't ready before checking again
	notReadySleepTime time.Duration = time.Second * 15

	ErrorColor             = color.FgRed
	WarningColor           = color.FgYellow
	UpdateDepositDataColor = color.FgHiWhite
	SendExitDataColor      = color.FgGreen
)

type waitUntilReadyResult int

const (
	waitUntilReadyExit waitUntilReadyResult = iota
	waitUntilReadyContinue
	waitUntilReadySuccess
)

type TaskLoop struct {
	// Services
	ctx     context.Context
	logger  *log.Logger
	sp      cscommon.IConstellationServiceProvider
	wg      *sync.WaitGroup
	csMgr   *cscommon.ConstellationManager
	rpMgr   *cscommon.RocketPoolManager
	grafMgr *cscommon.GraffitiManager

	// Tasks
	createNetworkSnapshot *NetworkSnapshotTask
	stakeMinipools        *StakeMinipoolsTask
	sendExitData          *SubmitSignedExitsTask

	// Internal
	wasExecutionClientSynced bool
	wasBeaconClientSynced    bool
	checkedGraffiti          bool
}

func NewTaskLoop(sp cscommon.IConstellationServiceProvider, wg *sync.WaitGroup) *TaskLoop {
	logger := sp.GetTasksLogger()
	ctx := logger.CreateContextWithLogger(sp.GetBaseContext())
	taskLoop := &TaskLoop{
		sp:                    sp,
		logger:                logger,
		ctx:                   ctx,
		wg:                    wg,
		csMgr:                 sp.GetConstellationManager(),
		rpMgr:                 sp.GetRocketPoolManager(),
		grafMgr:               sp.GetGraffitiManager(),
		createNetworkSnapshot: NewNetworkSnapshotTask(ctx, sp, logger),
		stakeMinipools:        NewStakeMinipoolsTask(ctx, sp, logger),
		sendExitData:          NewSubmitSignedExitsTask(ctx, sp, logger),

		wasExecutionClientSynced: true,
		wasBeaconClientSynced:    true,
	}
	return taskLoop
}

// Run daemon
func (t *TaskLoop) Run() error {
	// Write the graffiti to the file
	err := t.grafMgr.UpdateGraffitiFile(t.logger.Logger)
	if err != nil {
		return fmt.Errorf("error updating graffiti file: %w", err)
	}

	// Wait until the HD daemon has tried logging into the NodeSet server to check registration status
	t.getNodeSetRegistrationStatus()

	// Run task loop
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()

		for {
			// Update the graffiti in the VC key manager
			err := t.updateGraffitiInVc()
			if err != nil {
				t.logger.Warn("Updating graffiti in VC failed", log.Err(err))
			}

			// Make sure all of the resources are ready for task processing
			walletStatus, readyResult := t.waitUntilReady()
			switch readyResult {
			case waitUntilReadyExit:
				return
			case waitUntilReadyContinue:
				continue
			}

			// === Task execution ===
			if t.runTasks(walletStatus) {
				return
			}
		}
	}()

	/*
		// Run metrics loop
		go func() {
			err := runMetricsServer(sp, log.NewColorLogger(MetricsColor), stateLocker)
			if err != nil {
				errorLog.Println(err)
			}
			wg.Done()
		}()
	*/
	return nil
}

// Get thee NodeSet server registration status
func (t *TaskLoop) getNodeSetRegistrationStatus() {
	hd := t.sp.GetHyperdriveClient()
	attempts := 3
	for i := 0; i < attempts; i++ {
		response, err := hd.NodeSet.GetRegistrationStatus()
		if err != nil {
			// Error was because of a comms failure, so try again after 1 second
			t.logger.Warn(
				"Getting node registration status during NodeSet login attempt failed",
				slog.String(log.ErrorKey, err.Error()),
				slog.Int(keys.AttemptKey, i+1),
			)
			if utils.SleepWithCancel(t.ctx, time.Second) {
				return
			}
			continue
		}

		switch response.Data.Status {
		case api.NodeSetRegistrationStatus_Registered:
			// Successful login
			return
		case api.NodeSetRegistrationStatus_NoWallet:
			// Error was because the wallet isn't ready yet, so just return since logging in won't work yet
			t.logger.Info("Can't log into NodeSet, node doesn't have a wallet yet")
			return
		case api.NodeSetRegistrationStatus_Unregistered:
			// Node's not registered yet, this isn't an actual error to report
			t.logger.Info("Node is not registered with NodeSet yet")
			return
		default:
			// Error occurred on the remote side, so try again after 1 second
			t.logger.Warn(
				"NodeSet registration status is unknown",
				slog.String(log.ErrorKey, response.Data.ErrorMessage),
				slog.Int(keys.AttemptKey, i+1),
			)
			if utils.SleepWithCancel(t.ctx, time.Second) {
				return
			}
		}
	}
	t.logger.Error("Max login attempts reached")
}

// Wait until the chains and other resources are ready to be queried
// Returns true if the owning loop needs to exit, false if it can continue
func (t *TaskLoop) waitUntilReady() (*wallet.WalletStatus, waitUntilReadyResult) {
	// Check the EC status
	err := t.sp.WaitEthClientSynced(t.ctx, false) // Force refresh the primary / fallback EC status
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "context canceled") {
			return nil, waitUntilReadyExit
		}
		t.wasExecutionClientSynced = false
		t.logger.Error("Execution Client not synced. Waiting for sync...", slog.String(log.ErrorKey, errMsg))
		return nil, t.sleepAndReturnReadyResult()
	}

	if !t.wasExecutionClientSynced {
		t.logger.Info("Execution Client is now synced.")
		t.wasExecutionClientSynced = true
	}

	// Check the BC status
	err = t.sp.WaitBeaconClientSynced(t.ctx, false) // Force refresh the primary / fallback BC status
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "context canceled") {
			return nil, waitUntilReadyExit
		}
		// NOTE: if not synced, it returns an error - so there isn't necessarily an underlying issue
		t.wasBeaconClientSynced = false
		t.logger.Error("Beacon Node not synced. Waiting for sync...", slog.String(log.ErrorKey, errMsg))
		return nil, t.sleepAndReturnReadyResult()
	}

	if !t.wasBeaconClientSynced {
		t.logger.Info("Beacon Node is now synced.")
		t.wasBeaconClientSynced = true
	}

	// Wait for a wallet
	walletStatus, err := t.sp.WaitForWallet(t.ctx)
	if err != nil {
		errMsg := err.Error()
		t.logger.Error("Waiting for node address...", slog.String(log.ErrorKey, errMsg))
		return nil, t.sleepAndReturnReadyResult()
	}
	if walletStatus == nil {
		return nil, waitUntilReadyExit
	}

	// Wait for NodeSet registration
	if t.sp.WaitForNodeSetRegistration(t.ctx) {
		return nil, waitUntilReadyExit
	}

	return walletStatus, waitUntilReadySuccess
}

// Sleep on the context for the not-ready sleep time, and return either exit or continue
// based on whether the context was cancelled.
func (t *TaskLoop) sleepAndReturnReadyResult() waitUntilReadyResult {
	if utils.SleepWithCancel(t.ctx, notReadySleepTime) {
		return waitUntilReadyExit
	} else {
		return waitUntilReadyContinue
	}
}

// Runs an iteration of the node tasks.
// Returns true if the task loop should exit, false if it should continue.
func (t *TaskLoop) runTasks(walletStatus *wallet.WalletStatus) bool {
	// Create a network snapshot
	snapshot, err := t.createNetworkSnapshot.Run(walletStatus)
	if err != nil {
		t.logger.Error(err.Error())
		return utils.SleepWithCancel(t.ctx, tasksInterval)
	}

	// Stake minipools that are ready
	if err := t.stakeMinipools.Run(snapshot); err != nil {
		t.logger.Error(err.Error())
	}
	if utils.SleepWithCancel(t.ctx, taskCooldown) {
		return true
	}

	// Submit missing exit messages to the NodeSet server
	if err := t.sendExitData.Run(snapshot); err != nil {
		t.logger.Error(err.Error())
	}

	return utils.SleepWithCancel(t.ctx, tasksInterval)
}

// Update the graffiti in the VC key manager
func (t *TaskLoop) updateGraffitiInVc() error {
	if t.checkedGraffiti {
		return nil
	}

	// Get the list of keys in the VC
	km := t.sp.GetKeyManagerClient()
	loadedKeys, err := km.GetLoadedKeys(t.ctx, t.logger.Logger)
	if err != nil {
		return fmt.Errorf("error getting keys loaded in VC: %w", err)
	}

	// Update the graffiti for each key
	keys := make([]beacon.ValidatorPubkey, 0, len(loadedKeys))
	for _, loadedKey := range loadedKeys {
		keys = append(keys, loadedKey.Pubkey)
	}
	err = t.grafMgr.UpdateGraffitiInVc(t.ctx, t.logger.Logger, keys)
	if err != nil {
		return fmt.Errorf("error updating graffiti in VC: %w", err)
	}

	t.checkedGraffiti = true
	return nil
}
