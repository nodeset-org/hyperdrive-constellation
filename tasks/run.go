package cstasks

import (
	"context"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/rocket-pool/node-manager-core/log"
	"github.com/rocket-pool/node-manager-core/utils"

	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
)

// Config
const (
	tasksInterval time.Duration = time.Minute * 5
	taskCooldown  time.Duration = time.Second * 10

	ErrorColor             = color.FgRed
	WarningColor           = color.FgYellow
	UpdateDepositDataColor = color.FgHiWhite
	SendExitDataColor      = color.FgGreen
)

type TaskLoop struct {
	ctx    context.Context
	logger *log.Logger
	sp     *cscommon.ConstellationServiceProvider
	wg     *sync.WaitGroup
}

func NewTaskLoop(sp *cscommon.ConstellationServiceProvider, wg *sync.WaitGroup) *TaskLoop {
	taskLoop := &TaskLoop{
		sp:     sp,
		logger: sp.ServiceProvider.GetTasksLogger(),
		wg:     wg,
	}
	taskLoop.ctx = taskLoop.logger.CreateContextWithLogger(sp.ServiceProvider.GetBaseContext())
	return taskLoop
}

// Run daemon
func (t *TaskLoop) Run() error {
	// Initialize tasks

	// Run the loop
	t.wg.Add(1)
	go func() {

		for {
			err := t.sp.ServiceProvider.WaitEthClientSynced(t.ctx, false) // Force refresh the primary / fallback EC status
			if err != nil {
				t.logger.Error(err.Error())
				if utils.SleepWithCancel(t.ctx, taskCooldown) {
					break
				}
				continue
			}

			// Check the BC status
			err = t.sp.ServiceProvider.WaitBeaconClientSynced(t.ctx, false) // Force refresh the primary / fallback BC status
			if err != nil {
				t.logger.Error(err.Error())
				if utils.SleepWithCancel(t.ctx, taskCooldown) {
					break
				}
				continue
			}

			// Tasks start here

			// Tasks end here

			if utils.SleepWithCancel(t.ctx, tasksInterval) {
				break
			}
		}

		t.wg.Done()
	}()

	return nil
}
