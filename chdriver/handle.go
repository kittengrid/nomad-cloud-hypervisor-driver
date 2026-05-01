// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

package chdriver

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/drivers/shared/executor"
	"github.com/hashicorp/nomad/plugins/drivers"
)

// taskHandle holds the runtime state of a single cloud-hypervisor VM task.
type taskHandle struct {
	// stateLock syncs access to all fields below
	stateLock sync.RWMutex

	logger      hclog.Logger
	pid         int
	socketPath  string
	taskConfig  *drivers.TaskConfig
	procState   drivers.TaskState
	startedAt   time.Time
	completedAt time.Time
	exitResult  *drivers.ExitResult

	// doneCh is closed when the process exits, unblocking WaitTask callers.
	doneCh       chan struct{}
	client       *CloudHypervisorClient
	exec         executor.Executor
	driverConfig TaskConfig
}

func (h *taskHandle) TaskStatus() *drivers.TaskStatus {
	h.stateLock.RLock()
	defer h.stateLock.RUnlock()

	return &drivers.TaskStatus{
		ID:          h.taskConfig.ID,
		Name:        h.taskConfig.Name,
		State:       h.procState,
		StartedAt:   h.startedAt,
		CompletedAt: h.completedAt,
		ExitResult:  h.exitResult,
		DriverAttributes: map[string]string{
			"pid":         strconv.Itoa(h.pid),
			"socket_path": h.socketPath,
		},
	}
}

func (h *taskHandle) IsRunning() bool {
	h.stateLock.RLock()
	defer h.stateLock.RUnlock()
	return h.procState == drivers.TaskStateRunning
}

// run blocks on the executor until cloud-hypervisor exits, then records the
// real exit code/signal and closes doneCh to unblock WaitTask callers.
func (h *taskHandle) run() {
	ps, err := h.exec.Wait(context.Background())

	h.stateLock.Lock()
	h.procState = drivers.TaskStateExited
	h.completedAt = time.Now()
	if err != nil {
		h.exitResult = &drivers.ExitResult{Err: err}
	} else {
		h.exitResult = &drivers.ExitResult{
			ExitCode:  ps.ExitCode,
			Signal:    ps.Signal,
			OOMKilled: ps.OOMKilled,
		}
	}
	h.stateLock.Unlock()
	close(h.doneCh)
}
