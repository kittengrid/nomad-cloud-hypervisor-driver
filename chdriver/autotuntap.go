package chdriver

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/hashicorp/go-hclog"
)

type AutoTunTapIface struct {
	name   string
	bridge string
	logger hclog.Logger
}

func TaskIDToTapIfaceName(taskID string) string {
	// taskID has the form:
	// tap-6ca4e8fe-e8fb-0491-d383-2caaf2537d6c/kittenvisor/5a9094d9
	// Use the last path component as the tap suffix, giving tap-5a9094d9.
	ifaceSuffix := taskID[strings.LastIndex(taskID, "/")+1:]
	return fmt.Sprintf("tap-%s", ifaceSuffix)
}

func NewAutoTunTapIfaceFromNetConfig(networkConfig *TaskNetworkConfig, taskID string, logger hclog.Logger) *AutoTunTapIface {
	return NewAutoTunTapIface(TaskIDToTapIfaceName(taskID), networkConfig.AutoTuntapBridge, logger)
}

func NewAutoTunTapIface(iface string, bridge string, logger hclog.Logger) *AutoTunTapIface {
	return &AutoTunTapIface{
		name:   iface,
		bridge: bridge,
		logger: logger,
	}
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v failed: %w: %s", name, args, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (a *AutoTunTapIface) Up() error {
	logger := a.logger.With("device", a.name).With("bridge", a.bridge)
	if err := run("ip", "tuntap", "add", "dev", a.name, "mode", "tap"); err != nil {
		logger.Error("Failed to create tap device", "error", err)
		return fmt.Errorf("create tap device: %w", err)
	}

	if err := run("ip", "link", "set", a.name, "master", a.bridge); err != nil {
		logger.Error("Failed to set tap device master", "error", err)
		return fmt.Errorf("set tap device master: %w", err)
	}

	if err := run("ip", "link", "set", a.name, "up"); err != nil {
		logger.Error("Failed to set tap device up", "error", err)
		return fmt.Errorf("set tap device up: %w", err)
	}

	return nil
}

func (a *AutoTunTapIface) Down() error {
	logger := a.logger.With("device", a.name).With("bridge", a.bridge)
	if err := run("ip", "link", "set", "dev", a.name, "nomaster"); err != nil {
		logger.Error("Failed to set tap device nomaster", "error", err)
		return fmt.Errorf("set tap device nomaster: %w", err)
	}

	if err := run("ip", "link", "set", "dev", a.name, "down"); err != nil {
		logger.Error("Failed to set tap device down", "error", err)
		return fmt.Errorf("set tap device down: %w", err)
	}

	if err := run("ip", "tuntap", "del", "dev", a.name, "mode", "tap"); err != nil {
		logger.Error("Failed to delete tap device", "error", err)
		return fmt.Errorf("delete tap device: %w", err)
	}

	return nil
}
