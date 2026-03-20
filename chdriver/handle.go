// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

package chdriver

import (
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/plugins/drivers"
)

// taskHandle holds the runtime state of a single cloud-hypervisor VM task.
type taskHandle struct {
	// stateLock syncs access to all fields below
	stateLock sync.RWMutex

	logger     hclog.Logger
	pid        int
	socketPath string
	taskConfig *drivers.TaskConfig
	procState  drivers.TaskState
	startedAt  time.Time
	completedAt time.Time
	exitResult  *drivers.ExitResult

	// doneCh is closed when the process exits, unblocking WaitTask callers.
	doneCh chan struct{}
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

// run polls the cloud-hypervisor process until it exits, then updates state and
// closes doneCh. Using syscall.Kill(pid, 0) works both for processes we spawned
// and for processes we re-attached to after a driver restart.
func (h *taskHandle) run() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		if err := syscall.Kill(h.pid, syscall.Signal(0)); err != nil {
			// ESRCH means the process no longer exists.
			h.stateLock.Lock()
			h.procState = drivers.TaskStateExited
			h.exitResult = &drivers.ExitResult{ExitCode: 0}
			h.completedAt = time.Now()
			h.stateLock.Unlock()
			close(h.doneCh)
			return
		}
	}
}
