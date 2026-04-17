// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

package chdriver

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/hashicorp/consul-template/signals"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/drivers/shared/eventer"
	"github.com/hashicorp/nomad/drivers/shared/executor"
	"github.com/hashicorp/nomad/plugins/base"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/hashicorp/nomad/plugins/shared/hclspec"
	"github.com/hashicorp/nomad/plugins/shared/structs"
	"github.com/kittengrid/nomad-cloud-hypervisor-driver/internal/cloudinit"
	"github.com/kittengrid/nomad-cloud-hypervisor-driver/internal/oci"
)

const (
	// pluginName is the name of the plugin
	// this is used for logging and (along with the version) for uniquely
	// identifying plugin binaries fingerprinted by the client
	pluginName = "cloud-hypervisor"

	// pluginVersion allows the client to identify and use newer versions of
	// an installed plugin
	pluginVersion = "v0.0.1"

	// fingerprintPeriod is the interval at which the plugin will send
	// fingerprint responses
	fingerprintPeriod = 30 * time.Second

	// taskHandleVersion is the version of task handle which this plugin sets
	// and understands how to decode
	// this is used to allow modification and migration of the task schema
	// used by the plugin
	taskHandleVersion = 1
)

var (
	// pluginInfo describes the plugin
	pluginInfo = &base.PluginInfoResponse{
		Type:              base.PluginTypeDriver,
		PluginApiVersions: []string{drivers.ApiVersion010},
		PluginVersion:     pluginVersion,
		Name:              pluginName,
	}

	// configSpec is the specification of the plugin's configuration
	// this is used to validate the configuration specified for the plugin
	// on the client.
	// this is not global, but can be specified on a per-client basis.
	configSpec = hclspec.NewObject(map[string]*hclspec.Spec{
		// TODO: define plugin's agent configuration schema.
		//
		// The schema should be defined using HCL specs and it will be used to
		// validate the agent configuration provided by the user in the
		// `plugin` stanza (https://www.nomadproject.io/docs/configuration/plugin.html).
		//
		// For example, for the schema below a valid configuration would be:
		//
		//   plugin "cloud-hypervisor" {
		//     config {
		//       shell = "fish"
		//     }
		//   }
		"cloud-hypervisor-binary-path": hclspec.NewDefault(
			hclspec.NewAttr("cloud-hypervisor-binary-path", "string", false),
			hclspec.NewLiteral(`"/usr/bin/cloud-hypervisor"`),
		),
		"cloud-hypervisor-socket-dir": hclspec.NewDefault(
			hclspec.NewAttr("cloud-hypervisor-socket-dir", "string", false),
			hclspec.NewLiteral(`"/run/nomad-ch-driver"`),
		),
		"cache-dir": hclspec.NewDefault(
			hclspec.NewAttr("cache-dir", "string", false),
			hclspec.NewLiteral(`"/run/nomad-ch-driver"`),
		),
	})

	// taskConfigSpec is the specification of the plugin's configuration for
	// a task
	// this is used to validated the configuration specified for the plugin
	// when a job is submitted.
	taskConfigSpec = hclspec.NewObject(map[string]*hclspec.Spec{
		"payload": hclspec.NewBlock("payload", false, hclspec.NewObject(map[string]*hclspec.Spec{
			"kernel":    hclspec.NewAttr("kernel", "string", false),
			"initramfs": hclspec.NewAttr("initramfs", "string", false),
			"cmdline":   hclspec.NewAttr("cmdline", "string", false),
		})),
		"oci_image": hclspec.NewAttr("oci_image", "string", false),
		"disk": hclspec.NewBlockList("disk", hclspec.NewObject(map[string]*hclspec.Spec{
			"path":              hclspec.NewAttr("path", "string", false),
			"image_type":        hclspec.NewAttr("image_type", "string", false),
			"readonly":          hclspec.NewAttr("readonly", "bool", false),
			"ephemeral_overlay": hclspec.NewAttr("ephemeral_overlay", "bool", false),
			"oci_image":         hclspec.NewAttr("oci_image", "string", false),
		})),
		"console": hclspec.NewBlock("console", false, hclspec.NewObject(map[string]*hclspec.Spec{
			"mode": hclspec.NewAttr("mode", "string", false),
		})),
		"network": hclspec.NewBlockList("network", hclspec.NewObject(map[string]*hclspec.Spec{
			"mac":                hclspec.NewAttr("mac", "string", false),
			"tap":                hclspec.NewAttr("tap", "string", false),
			"auto-tuntap":        hclspec.NewAttr("auto-tuntap", "bool", false),
			"auto-tuntap-bridge": hclspec.NewAttr("auto-tuntap-bridge", "string", false),
		})),
		// cloud_init is an optional inline cloud-config string.  When set the
		// driver writes the content to a file, generates a NoCloud seed ISO
		// image and attaches it as a read-only disk to the VM.
		"cloud-init": hclspec.NewBlock("cloud-init", false, hclspec.NewObject(map[string]*hclspec.Spec{
			"user-data": hclspec.NewAttr("user-data", "string", true),
			"meta-data": hclspec.NewAttr("meta-data", "string", false),
		})),
		"serial": hclspec.NewAttr("serial", "string", false),
	})

	// capabilities indicates what optional features this driver supports
	// this should be set according to the target run time.
	capabilities = &drivers.Capabilities{
		// TODO: set plugin's capabilities
		//
		// The plugin's capabilities signal Nomad which extra functionalities
		// are supported. For a list of available options check the docs page:
		// https://godoc.org/github.com/hashicorp/nomad/plugins/drivers#Capabilities
		SendSignals: true,
		Exec:        false,
	}
)

// Config contains configuration information for the plugin
type Config struct {
	// TODO: create decoded plugin configuration struct
	//
	// This struct is the decoded version of the schema defined in the
	// configSpec variable above. It's used to convert the HCL configuration
	// passed by the Nomad agent into Go contructs.
	CloudHypervisorBinaryPath string `codec:"cloud-hypervisor-binary-path"`
	CloudHypervisorSocketDir  string `codec:"cloud-hypervisor-socket-dir"`
	CacheDir                  string `codec:"cache-dir"`
}

// TaskPayloadConfig corresponds to PayloadConfig in chtypes.
type TaskPayloadConfig struct {
	Kernel    string `codec:"kernel"    json:"kernel,omitempty"`
	Initramfs string `codec:"initramfs" json:"initramfs,omitempty"`
	Cmdline   string `codec:"cmdline"   json:"cmdline,omitempty"`
}

// TaskDiskConfig corresponds to DiskConfig in chtypes.
type TaskDiskConfig struct {
	Path             string `codec:"path"              json:"path,omitempty"`
	ImageType        string `codec:"image_type"        json:"image_type,omitempty"`
	Readonly         bool   `codec:"readonly"          json:"readonly"`
	EphemeralOverlay bool   `codec:"ephemeral_overlay" json:"ephemeral_overlay"`
	OCIImage         string `codec:"oci_image"         json:"oci_image,omitempty"`
}

// TaskConsoleConfig corresponds to ConsoleConfig in chtypes (required fields only).
type TaskConsoleConfig struct {
	Mode string `codec:"mode" json:"mode,omitempty"`
}

type TaskNetworkConfig struct {
	Mac              string `codec:"mac"                json:"mac,omitempty"`
	Tap              string `codec:"tap"                json:"tap,omitempty"`
	AutoTuntap       bool   `codec:"auto-tuntap"        json:"auto-tuntap"`
	AutoTuntapBridge string `codec:"auto-tuntap-bridge" json:"auto-tuntap-bridge,omitempty"`
}

type CloudInit struct {
	UserData string `codec:"user-data" json:"user-data,omitempty"`
	MetaData string `codec:"meta-data" json:"meta-data,omitempty"`
}

// TaskConfig contains configuration information for a task that runs with
// this plugin
type TaskConfig struct {
	Payload   TaskPayloadConfig   `codec:"payload"`
	Disk      []TaskDiskConfig    `codec:"disk"`
	Console   TaskConsoleConfig   `codec:"console"`
	Network   []TaskNetworkConfig `codec:"network"`
	CloudInit *CloudInit          `codec:"cloud-init"`
	OCIImage  string              `codec:"oci_image"`
	Serial    string              `codec:"serial"`
}

// TaskState is the runtime state which is encoded in the handle returned to
// Nomad client.
// This information is needed to rebuild the task state and handler during
// recovery.
type TaskState struct {
	TaskConfig     *drivers.TaskConfig
	StartedAt      time.Time
	ReattachConfig *structs.ReattachConfig
	Pid            int
	SocketPath     string
}

// CloudHypervisorDriverPlugin is an example driver plugin. When provisioned in a job,
// the taks will output a greet specified by the user.
type CloudHypervisorDriverPlugin struct {
	// eventer is used to handle multiplexing of TaskEvents calls such that an
	// event can be broadcast to all callers
	eventer *eventer.Eventer

	// config is the plugin configuration set by the SetConfig RPC
	config *Config

	// nomadConfig is the client config from Nomad
	nomadConfig *base.ClientDriverConfig

	// tasks is the in memory datastore mapping taskIDs to driver handles
	tasks *taskStore

	// ctx is the context for the driver. It is passed to other subsystems to
	// coordinate shutdown
	ctx context.Context

	// signalShutdown is called when the driver is shutting down and cancels
	// the ctx passed to any subsystems
	signalShutdown context.CancelFunc

	// logger will log to the Nomad agent
	logger hclog.Logger

	cmd *exec.Cmd
}

// NewPlugin returns a new cloud-hypervisor driver plugin
func NewPlugin(logger hclog.Logger) drivers.DriverPlugin {
	ctx, cancel := context.WithCancel(context.Background())
	logger = logger.Named(pluginName)

	return &CloudHypervisorDriverPlugin{
		eventer:        eventer.NewEventer(ctx, logger),
		config:         &Config{},
		tasks:          newTaskStore(),
		ctx:            ctx,
		signalShutdown: cancel,
		logger:         logger,
	}
}

// PluginInfo returns information describing the plugin.
func (d *CloudHypervisorDriverPlugin) PluginInfo() (*base.PluginInfoResponse, error) {
	return pluginInfo, nil
}

// ConfigSchema returns the plugin configuration schema.
func (d *CloudHypervisorDriverPlugin) ConfigSchema() (*hclspec.Spec, error) {
	return configSpec, nil
}

// SetConfig is called by the client to pass the configuration for the plugin.
func (d *CloudHypervisorDriverPlugin) SetConfig(cfg *base.Config) error {
	var config Config
	if len(cfg.PluginConfig) != 0 {
		if err := base.MsgPackDecode(cfg.PluginConfig, &config); err != nil {
			return err
		}
	}

	// Save the configuration to the plugin
	d.config = &config
	d.nomadConfig = cfg.AgentConfig.Driver

	binary := d.config.CloudHypervisorBinaryPath
	// We check that the binary path exists and it is executable, using go fileutils for stat
	info, err := os.Stat(binary)
	if err != nil {
		return fmt.Errorf("binary %s not found: %v", binary, err)
	}

	// We check if it is executable by checking the executable bit in the file mode
	if info.Mode()&0111 == 0 {
		return fmt.Errorf("binary %s is not executable", binary)
	}

	return nil
}

// TaskConfigSchema returns the HCL schema for the configuration of a task.
func (d *CloudHypervisorDriverPlugin) TaskConfigSchema() (*hclspec.Spec, error) {
	return taskConfigSpec, nil
}

// Capabilities returns the features supported by the driver.
func (d *CloudHypervisorDriverPlugin) Capabilities() (*drivers.Capabilities, error) {
	return capabilities, nil
}

// Fingerprint returns a channel that will be used to send health information
// and other driver specific node attributes.
func (d *CloudHypervisorDriverPlugin) Fingerprint(ctx context.Context) (<-chan *drivers.Fingerprint, error) {
	ch := make(chan *drivers.Fingerprint)
	go d.handleFingerprint(ctx, ch)
	return ch, nil
}

// handleFingerprint manages the channel and the flow of fingerprint data.
func (d *CloudHypervisorDriverPlugin) handleFingerprint(ctx context.Context, ch chan<- *drivers.Fingerprint) {
	defer close(ch)

	// Nomad expects the initial fingerprint to be sent immediately
	ticker := time.NewTimer(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			// after the initial fingerprint we can set the proper fingerprint
			// period
			ticker.Reset(fingerprintPeriod)
			ch <- d.buildFingerprint()
		}
	}
}

// buildFingerprint returns the driver's fingerprint data
func (d *CloudHypervisorDriverPlugin) buildFingerprint() *drivers.Fingerprint {
	fp := &drivers.Fingerprint{
		Attributes:        map[string]*structs.Attribute{},
		Health:            drivers.HealthStateHealthy,
		HealthDescription: drivers.DriverHealthy,
	}

	binary := d.config.CloudHypervisorBinaryPath

	cmd := exec.Command("which", binary)
	if err := cmd.Run(); err != nil {
		return &drivers.Fingerprint{
			Health:            drivers.HealthStateUndetected,
			HealthDescription: fmt.Sprintf("cloud-hypervisor binary not found in path %s", binary),
		}
	}

	// We just run the cloud hypervisor and regexp the version
	cmd = exec.Command(binary, "--version")

	output, err := cmd.Output()
	if err != nil {
		return &drivers.Fingerprint{
			Health:            drivers.HealthStateUnhealthy,
			HealthDescription: fmt.Sprintf("cloud-hypervisor binary at path %s is not executable: %v", binary, err),
		}
	}
	// \d+\.\d+(\.\d+)?
	date := regexp.MustCompile(`\d+\.\d+(\.\d+)?`).FindString(string(output))
	if date == "" {
		return &drivers.Fingerprint{
			Health:            drivers.HealthStateUnhealthy,
			HealthDescription: fmt.Sprintf("cloud-hypervisor binary at path %s did not return a version string", binary),
		}
	}

	fp.Attributes["driver.cloud-hypervisor.cloud-hypervisor_version"] = structs.NewStringAttribute(date)
	fp.Attributes["driver.cloud-hypervisor.cloud-hypervisor_path"] = structs.NewStringAttribute(binary)
	fp.Attributes["driver.cloud-hypervisor.cloud-hypervisor_socket_dir"] = structs.NewStringAttribute(d.config.CloudHypervisorSocketDir)
	fp.Attributes["driver.cloud-hypervisor.cache-dir"] = structs.NewStringAttribute(d.config.CacheDir)

	return fp
}

// StartTask returns a task handle and a driver network if necessary.
func (d *CloudHypervisorDriverPlugin) StartTask(cfg *drivers.TaskConfig) (*drivers.TaskHandle, *drivers.DriverNetwork, error) {
	if _, ok := d.tasks.Get(cfg.ID); ok {
		return nil, nil, fmt.Errorf("task with ID %q already started", cfg.ID)
	}

	var driverConfig TaskConfig
	if err := cfg.DecodeDriverConfig(&driverConfig); err != nil {
		return nil, nil, fmt.Errorf("failed to decode driver config: %v", err)
	}

	if err := resolveOCIPayload(d.ctx, &driverConfig, d.config.CacheDir, d.logger); err != nil {
		return nil, nil, fmt.Errorf("resolve OCI payload: %v", err)
	}

	d.logger.Info("starting task", "driver_cfg", hclog.Fmt("%+v", driverConfig))
	handle := drivers.NewTaskHandle(taskHandleVersion)
	handle.Config = cfg

	pluginLogFile := filepath.Join(cfg.TaskDir().Dir, "executor.out")
	executorConfig := &executor.ExecutorConfig{
		LogFile:  pluginLogFile,
		LogLevel: "debug",
		Compute:  d.nomadConfig.Topology.Compute(),
	}
	logger := d.logger.With("task_name", handle.Config.Name, "alloc_id", handle.Config.AllocID)

	exec, pluginClient, err := executor.CreateExecutor(logger, d.nomadConfig, executorConfig)

	logger.Info("executor created for task", "task_id", cfg.ID, "log_file", pluginLogFile)

	if err != nil {
		return nil, nil, fmt.Errorf("failed to create executor: %v", err)
	}

	// If an inline cloud-init payload was provided, materialise it as a
	// NoCloud seed ISO inside the task's local directory so it survives
	// across driver restarts together with the rest of the allocation data.
	if driverConfig.CloudInit != nil && driverConfig.CloudInit.UserData != "" {
		isoPath := filepath.Join(cfg.TaskDir().LocalDir, "cloud-init-seed.iso")
		if err := cloudinit.CreateISO(driverConfig.CloudInit.UserData, driverConfig.CloudInit.MetaData, isoPath, d.logger); err != nil {
			return nil, nil, fmt.Errorf("failed to create cloud-init ISO: %v", err)
		}
		// Append the generated ISO as a read-only disk so the task config
		// carries the resolved path into startCloudHypervisor.
		driverConfig.Disk = append(driverConfig.Disk, TaskDiskConfig{
			Path:     isoPath,
			Readonly: true,
		})
	}

	// We have to set up the net devices if the config specifies AutoTuntap
	for _, net := range driverConfig.Network {
		if net.AutoTuntap {
			if err := NewAutoTunTapIfaceFromNetConfig(&net, cfg.ID, d.logger).Up(); err != nil {
				return nil, nil, fmt.Errorf("set up auto-tuntap: %w", err)
			}
		}
	}

	// Now the disks, in case any of them are ephemeral overlays that need to be generated before starting
	for i, disk := range driverConfig.Disk {
		if disk.OCIImage != "" {
			if err := os.MkdirAll(d.config.CacheDir, 0o755); err != nil {
				return nil, nil, fmt.Errorf("create socket dir: %w", err)
			}

			// If the disk is specified as an OCI image, we need to pull it and convert it to a qcow2 image before starting the VM.
			pullOptions := oci.PullOptions{
				Reference: disk.OCIImage,
				CacheDir:  d.config.CacheDir,
			}

			ociImagePath, err := oci.PullIntoCache(context.Background(), pullOptions, d.logger)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to pull OCI image: %v", err)
			}

			// Update the disk path to point to the pulled image, which is what startCloudHypervisor will use.
			driverConfig.Disk[i].Path = ociImagePath.WorkDir + "/rootfs.qcow2"
			driverConfig.Disk[i].ImageType = "qcow2"
		}

		if disk.EphemeralOverlay {
			overlayPath := filepath.Join(cfg.TaskDir().LocalDir, fmt.Sprintf("overlay-%d.img", i))
			if err := NewOverlayDiskFromDiskConfig(disk, overlayPath).Create(); err != nil {
				return nil, nil, fmt.Errorf("failed to create ephemeral overlay: %v", err)
			}
			// Update the disk path to point to the overlay, which is what startCloudHypervisor will use.
			driverConfig.Disk[i].Path = overlayPath
			driverConfig.Disk[i].ImageType = "qcow2"
		}
	}

	proc, err := startCloudHypervisor(
		d.config.CloudHypervisorBinaryPath,
		d.config.CloudHypervisorSocketDir,
		cfg,
		driverConfig,
		exec,
		d.logger,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start cloud-hypervisor: %v", err)
	}

	h := &taskHandle{
		pid:          proc.Pid,
		exec:         proc.exec,
		socketPath:   proc.SocketBasePath,
		taskConfig:   cfg,
		procState:    drivers.TaskStateRunning,
		startedAt:    time.Now().Round(time.Millisecond),
		logger:       d.logger,
		doneCh:       make(chan struct{}),
		client:       NewCloudHypervisorClient(NewCloudHypervisorClientConfig(proc.SocketBasePath), d.logger),
		driverConfig: driverConfig,
	}

	driverState := TaskState{
		Pid:            proc.Pid,
		SocketPath:     proc.SocketBasePath,
		ReattachConfig: structs.ReattachConfigFromGoPlugin(pluginClient.ReattachConfig()),
		TaskConfig:     cfg,
		StartedAt:      h.startedAt,
	}

	if err := handle.SetDriverState(&driverState); err != nil {
		return nil, nil, fmt.Errorf("failed to set driver state: %v", err)
	}

	d.tasks.Set(cfg.ID, h)
	go h.run()
	return handle, nil, nil
}

// RecoverTask recreates the in-memory state of a task from a TaskHandle.
func (d *CloudHypervisorDriverPlugin) RecoverTask(handle *drivers.TaskHandle) error {
	if handle == nil {
		return errors.New("error: handle cannot be nil")
	}

	if _, ok := d.tasks.Get(handle.Config.ID); ok {
		return nil
	}

	var taskState TaskState
	if err := handle.GetDriverState(&taskState); err != nil {
		return fmt.Errorf("failed to decode task state from handle: %v", err)
	}

	// Recreate the executor client so we can monitor the process and collect stats.
	plugRC, err := structs.ReattachConfigToGoPlugin(taskState.ReattachConfig)
	if err != nil {
		d.logger.Error("failed to build ReattachConfig from task state", "error", err, "task_id", handle.Config.ID)
		return fmt.Errorf("failed to build ReattachConfig from task state: %v", err)
	}

	exec, _, err := executor.ReattachToExecutor(
		plugRC,
		d.logger.With("task_name", handle.Config.Name, "alloc_id", handle.Config.AllocID),
		d.nomadConfig.Topology.Compute(),
	)

	// Check the process is still alive before declaring recovery successful.
	if err := syscall.Kill(taskState.Pid, syscall.Signal(0)); err != nil {
		return fmt.Errorf("cloud-hypervisor pid %d not found: %w", taskState.Pid, err)
	}

	h := &taskHandle{
		pid:        taskState.Pid,
		socketPath: taskState.SocketPath,
		exec:       exec,
		taskConfig: taskState.TaskConfig,
		procState:  drivers.TaskStateRunning,
		startedAt:  taskState.StartedAt,
		exitResult: &drivers.ExitResult{},
		logger:     d.logger,
		client:     NewCloudHypervisorClient(NewCloudHypervisorClientConfig(taskState.SocketPath), d.logger),
		doneCh:     make(chan struct{}),
	}

	d.logger.Info("successfully recovered task", "task_id", handle.Config.ID, "pid", h.pid)
	d.tasks.Set(taskState.TaskConfig.ID, h)

	go h.run()
	return nil
}

// WaitTask returns a channel used to notify Nomad when a task exits.
func (d *CloudHypervisorDriverPlugin) WaitTask(ctx context.Context, taskID string) (<-chan *drivers.ExitResult, error) {
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return nil, drivers.ErrTaskNotFound
	}

	ch := make(chan *drivers.ExitResult)
	go d.handleWait(ctx, handle, ch)
	return ch, nil
}

func (d *CloudHypervisorDriverPlugin) handleWait(ctx context.Context, handle *taskHandle, ch chan *drivers.ExitResult) {
	defer close(ch)

	select {
	case <-handle.doneCh:
		handle.stateLock.RLock()
		result := handle.exitResult
		handle.stateLock.RUnlock()
		ch <- result
	case <-ctx.Done():
	case <-d.ctx.Done():
	}
}

// StopTask stops a running task with the given signal and within the timeout window.
func (d *CloudHypervisorDriverPlugin) StopTask(taskID string, timeout time.Duration, signal string) (err error) {
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return drivers.ErrTaskNotFound
	}

	sig := syscall.SIGTERM
	if s, ok := signals.SignalLookup[signal]; ok {
		sig = s.(syscall.Signal)
	}

	if err := syscall.Kill(handle.pid, sig); err != nil {
		return fmt.Errorf("failed to send signal to pid %d: %w", handle.pid, err)
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if syscall.Kill(handle.pid, syscall.Signal(0)) != nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Grace period elapsed — force kill.
	_ = syscall.Kill(handle.pid, syscall.SIGKILL)

	return nil
}

// DestroyTask cleans up and removes a task that has terminated.
func (d *CloudHypervisorDriverPlugin) DestroyTask(taskID string, force bool) error {
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return drivers.ErrTaskNotFound
	}

	defer func() {
		// We have to delete net devices if the config specifies AutoTuntap
		for _, net := range handle.driverConfig.Network {
			if net.AutoTuntap {
				d.logger.Info("cleaning up auto-tuntap device", "task_id", taskID, "iface", net)

				if err := NewAutoTunTapIfaceFromNetConfig(&net, handle.taskConfig.ID, d.logger).Down(); err != nil {
					err = fmt.Errorf("drop auto-tuntap: %w", err)
				}
			}
		}
	}()

	if handle.IsRunning() && !force {
		return errors.New("cannot destroy running task")
	}

	if handle.IsRunning() {
		_ = syscall.Kill(handle.pid, syscall.SIGKILL)
	}

	// Remove the cloud-init seed ISO if one was generated for this task.
	// The file lives inside the alloc's local dir, which Nomad will eventually
	// garbage-collect anyway, but we remove it explicitly here to be tidy.
	if handle.driverConfig.CloudInit != nil {
		isoPath := filepath.Join(handle.taskConfig.TaskDir().LocalDir, "cloud-init-seed.iso")
		if err := os.Remove(isoPath); err != nil && !os.IsNotExist(err) {
			d.logger.Warn("failed to remove cloud-init ISO", "path", isoPath, "error", err)
		}
	}

	d.tasks.Delete(taskID)
	return nil
}

// InspectTask returns detailed status information for the referenced taskID.
func (d *CloudHypervisorDriverPlugin) InspectTask(taskID string) (*drivers.TaskStatus, error) {
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return nil, drivers.ErrTaskNotFound
	}

	return handle.TaskStatus(), nil
}

// TaskStats returns a channel which the driver should send stats to at the given interval.
func (d *CloudHypervisorDriverPlugin) TaskStats(
	ctx context.Context,
	taskID string,
	interval time.Duration,
) (<-chan *drivers.TaskResourceUsage, error) {
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return nil, drivers.ErrTaskNotFound
	}

	in, err := handle.exec.Stats(ctx, interval)
	if err != nil {
		return nil, err
	}

	out := make(chan *drivers.TaskResourceUsage)

	go func() {
		defer close(out)

		for {
			select {
			case <-ctx.Done():
				return
			case usage, ok := <-in:
				if !ok {
					return
				}
				info, err := handle.client.GetVMInfo(ctx)
				if err == nil {
					usageStats := drivers.MemoryStats{
						Usage:    u64FromI64(info.MemoryActualSize),
						Measured: []string{"Usage"},
					}
					memoryMax := drivers.MemoryStats{
						MaxUsage: uint64(memoryBytesFromResources(handle.taskConfig.Resources)),
						Measured: []string{"MaxUsage"},
					}
					usage.ResourceUsage.MemoryStats.Add(&usageStats)
					usage.ResourceUsage.MemoryStats.Add(&memoryMax)
					d.logger.Debug("Actual memory usage", "usage", usage.ResourceUsage.MemoryStats.Usage)
				}
				sanitizeTaskResourceUsage(usage)

				out <- usage
			}
		}
	}()

	return out, nil
}

func u64FromI64(ptr *int64) uint64 {
	if ptr == nil {
		return 0
	}
	if *ptr < 0 {
		return 0 // guard against invalid negative values
	}
	return uint64(*ptr)
}

func sanitizeTaskResourceUsage(u *drivers.TaskResourceUsage) {
	if u == nil {
		return
	}

	sanitizeResourceUsage(u.ResourceUsage)

	for _, pidUsage := range u.Pids {
		sanitizeResourceUsage(pidUsage)
	}
}

func sanitizeResourceUsage(r *drivers.ResourceUsage) {
	if r == nil || r.CpuStats == nil {
		return
	}

	r.CpuStats.SystemMode = finite(r.CpuStats.SystemMode)
	r.CpuStats.UserMode = finite(r.CpuStats.UserMode)
	r.CpuStats.TotalTicks = finite(r.CpuStats.TotalTicks)
	r.CpuStats.Percent = finite(r.CpuStats.Percent)
}

func finite(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// TaskEvents returns a channel that the plugin can use to emit task related events.
func (d *CloudHypervisorDriverPlugin) TaskEvents(ctx context.Context) (<-chan *drivers.TaskEvent, error) {
	return d.eventer.TaskEvents(ctx)
}

// SignalTask forwards a signal to a task.
// This is an optional capability.
func (d *CloudHypervisorDriverPlugin) SignalTask(taskID string, signal string) error {
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return drivers.ErrTaskNotFound
	}

	sig := syscall.SIGINT
	if s, ok := signals.SignalLookup[signal]; ok {
		sig = s.(syscall.Signal)
	} else {
		d.logger.Warn("unknown signal to send to task, using SIGINT instead", "signal", signal, "task_id", handle.taskConfig.ID)
	}
	return syscall.Kill(handle.pid, sig)
}

// ExecTask returns the result of executing the given command inside a task.
// This is an optional capability.
func (d *CloudHypervisorDriverPlugin) ExecTask(taskID string, cmd []string, timeout time.Duration) (*drivers.ExecTaskResult, error) {
	// TODO: implement driver specific logic to execute commands in a task.
	return nil, errors.New("This driver does not support exec")
}
