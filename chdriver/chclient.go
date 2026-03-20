package chdriver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/hashicorp/go-hclog"
)

type CloudHypervisorClientConfig struct {
	CloudHypervisorSocket string
}

func NewCloudHypervisorClientConfig(socket string) *CloudHypervisorClientConfig {
	return &CloudHypervisorClientConfig{
		CloudHypervisorSocket: socket,
	}
}

type CloudHypervisorClient struct {
	config *CloudHypervisorClientConfig
	client *http.Client
	logger hclog.Logger
}

func NewCloudHypervisorClient(config *CloudHypervisorClientConfig, logger hclog.Logger) *CloudHypervisorClient {
	sock := config.CloudHypervisorSocket

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	return &CloudHypervisorClient{
		config: config,
		client: client,
		logger: logger,
	}
}

// doRequest executes an HTTP request against the cloud-hypervisor API.
// If payload is non-nil it is JSON-encoded and sent as the request body.
// The raw response body and status code are returned so callers can decide
// whether to unmarshal; any HTTP status >= 300 is returned as an error.
func (c *CloudHypervisorClient) doRequest(
	ctx context.Context,
	method, path string,
	payload any,
) ([]byte, int, error) {
	var reqBody io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, "http://localhost/api/v1/"+path, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("do request over unix socket: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	c.logger.Info("cloud-hypervisor response",
		"method", method,
		"path", path,
		"status", resp.StatusCode,
		"body", string(body),
	)

	if resp.StatusCode >= 300 {
		return nil, resp.StatusCode, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	return body, resp.StatusCode, nil
}

// PingCloudHypervisor checks API server availability and returns VMM information.
func (c *CloudHypervisorClient) PingCloudHypervisor(ctx context.Context) (VmmPingResponse, error) {
	result := VmmPingResponse{}

	body, _, err := c.doRequest(ctx, http.MethodGet, "vmm.ping", nil)
	if err != nil {
		return result, err
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return result, fmt.Errorf("unmarshal response: %w", err)
	}

	return result, nil
}

// ShutdownVMM shuts down the cloud-hypervisor VMM process entirely.
func (c *CloudHypervisorClient) ShutdownVMM(ctx context.Context) error {
	_, _, err := c.doRequest(ctx, http.MethodPut, "vmm.shutdown", nil)
	return err
}

// InjectNMI injects a Non-Maskable Interrupt into the VM.
func (c *CloudHypervisorClient) InjectNMI(ctx context.Context) error {
	_, _, err := c.doRequest(ctx, http.MethodPut, "vmm.nmi", nil)
	return err
}

// GetVMInfo returns general information about the VM instance.
func (c *CloudHypervisorClient) GetVMInfo(ctx context.Context) (VmInfo, error) {
	result := VmInfo{}

	body, _, err := c.doRequest(ctx, http.MethodGet, "vm.info", nil)
	if err != nil {
		return result, err
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return result, fmt.Errorf("unmarshal response: %w", err)
	}

	return result, nil
}

// GetVMCounters returns the current device counters for the VM instance.
func (c *CloudHypervisorClient) GetVMCounters(ctx context.Context) (VmCounters, error) {
	result := VmCounters{}

	body, _, err := c.doRequest(ctx, http.MethodGet, "vm.counters", nil)
	if err != nil {
		return result, err
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return result, fmt.Errorf("unmarshal response: %w", err)
	}

	return result, nil
}

// CreateVM creates a new VM instance from the provided configuration.
// The VM is only created, not booted; call BootVM to start it.
func (c *CloudHypervisorClient) CreateVM(ctx context.Context, cfg VmConfig) error {
	_, _, err := c.doRequest(ctx, http.MethodPut, "vm.create", cfg)
	return err
}

// DeleteVM deletes the current VM instance.
func (c *CloudHypervisorClient) DeleteVM(ctx context.Context) error {
	_, _, err := c.doRequest(ctx, http.MethodPut, "vm.delete", nil)
	return err
}

// BootVM boots a previously created VM instance.
func (c *CloudHypervisorClient) BootVM(ctx context.Context) error {
	_, _, err := c.doRequest(ctx, http.MethodPut, "vm.boot", nil)
	return err
}

// PauseVM pauses a running VM instance.
func (c *CloudHypervisorClient) PauseVM(ctx context.Context) error {
	_, _, err := c.doRequest(ctx, http.MethodPut, "vm.pause", nil)
	return err
}

// ResumeVM resumes a previously paused VM instance.
func (c *CloudHypervisorClient) ResumeVM(ctx context.Context) error {
	_, _, err := c.doRequest(ctx, http.MethodPut, "vm.resume", nil)
	return err
}

// ShutdownVM shuts down the VM instance.
func (c *CloudHypervisorClient) ShutdownVM(ctx context.Context) error {
	_, _, err := c.doRequest(ctx, http.MethodPut, "vm.shutdown", nil)
	return err
}

// RebootVM reboots the VM instance.
func (c *CloudHypervisorClient) RebootVM(ctx context.Context) error {
	_, _, err := c.doRequest(ctx, http.MethodPut, "vm.reboot", nil)
	return err
}

// PowerButtonVM triggers the power button on the VM instance.
func (c *CloudHypervisorClient) PowerButtonVM(ctx context.Context) error {
	_, _, err := c.doRequest(ctx, http.MethodPut, "vm.power-button", nil)
	return err
}

// ResizeVM resizes the vCPUs and/or memory of the VM instance.
func (c *CloudHypervisorClient) ResizeVM(ctx context.Context, resize VmResize) error {
	_, _, err := c.doRequest(ctx, http.MethodPut, "vm.resize", resize)
	return err
}

// ResizeDisk resizes a disk attached to the VM instance.
func (c *CloudHypervisorClient) ResizeDisk(ctx context.Context, resize VmResizeDisk) error {
	_, _, err := c.doRequest(ctx, http.MethodPut, "vm.resize-disk", resize)
	return err
}

// ResizeMemoryZone resizes a memory zone of the VM instance.
func (c *CloudHypervisorClient) ResizeMemoryZone(ctx context.Context, resize VmResizeZone) error {
	_, _, err := c.doRequest(ctx, http.MethodPut, "vm.resize-zone", resize)
	return err
}

// AddDevice adds a host device to the VM instance.
// Returns PciDeviceInfo when the device is hot-added (200); nil when cold-added (204).
func (c *CloudHypervisorClient) AddDevice(ctx context.Context, device DeviceConfig) (*PciDeviceInfo, error) {
	body, status, err := c.doRequest(ctx, http.MethodPut, "vm.add-device", device)
	if err != nil {
		return nil, err
	}

	if status == http.StatusOK {
		result := &PciDeviceInfo{}
		if err := json.Unmarshal(body, result); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}
		return result, nil
	}

	return nil, nil
}

// RemoveDevice removes a device from the VM instance by its identifier.
func (c *CloudHypervisorClient) RemoveDevice(ctx context.Context, device VmRemoveDevice) error {
	_, _, err := c.doRequest(ctx, http.MethodPut, "vm.remove-device", device)
	return err
}

// AddDisk adds a disk to the VM instance.
// Returns PciDeviceInfo when the disk is hot-added (200); nil when cold-added (204).
func (c *CloudHypervisorClient) AddDisk(ctx context.Context, disk DiskConfig) (*PciDeviceInfo, error) {
	body, status, err := c.doRequest(ctx, http.MethodPut, "vm.add-disk", disk)
	if err != nil {
		return nil, err
	}

	if status == http.StatusOK {
		result := &PciDeviceInfo{}
		if err := json.Unmarshal(body, result); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}
		return result, nil
	}

	return nil, nil
}

// AddFs adds a virtio-fs device to the VM instance.
// Returns PciDeviceInfo when the device is hot-added (200); nil when cold-added (204).
func (c *CloudHypervisorClient) AddFs(ctx context.Context, fs FsConfig) (*PciDeviceInfo, error) {
	body, status, err := c.doRequest(ctx, http.MethodPut, "vm.add-fs", fs)
	if err != nil {
		return nil, err
	}

	if status == http.StatusOK {
		result := &PciDeviceInfo{}
		if err := json.Unmarshal(body, result); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}
		return result, nil
	}

	return nil, nil
}

// AddPmem adds a persistent memory device to the VM instance.
// Returns PciDeviceInfo when the device is hot-added (200); nil when cold-added (204).
func (c *CloudHypervisorClient) AddPmem(ctx context.Context, pmem PmemConfig) (*PciDeviceInfo, error) {
	body, status, err := c.doRequest(ctx, http.MethodPut, "vm.add-pmem", pmem)
	if err != nil {
		return nil, err
	}

	if status == http.StatusOK {
		result := &PciDeviceInfo{}
		if err := json.Unmarshal(body, result); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}
		return result, nil
	}

	return nil, nil
}

// AddNet adds a network device to the VM instance.
// Returns PciDeviceInfo when the device is hot-added (200); nil when cold-added (204).
func (c *CloudHypervisorClient) AddNet(ctx context.Context, net NetConfig) (*PciDeviceInfo, error) {
	body, status, err := c.doRequest(ctx, http.MethodPut, "vm.add-net", net)
	if err != nil {
		return nil, err
	}

	if status == http.StatusOK {
		result := &PciDeviceInfo{}
		if err := json.Unmarshal(body, result); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}
		return result, nil
	}

	return nil, nil
}

// AddVsock adds a vsock device to the VM instance.
// Returns PciDeviceInfo when the device is hot-added (200); nil when cold-added (204).
func (c *CloudHypervisorClient) AddVsock(ctx context.Context, vsock VsockConfig) (*PciDeviceInfo, error) {
	body, status, err := c.doRequest(ctx, http.MethodPut, "vm.add-vsock", vsock)
	if err != nil {
		return nil, err
	}

	if status == http.StatusOK {
		result := &PciDeviceInfo{}
		if err := json.Unmarshal(body, result); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}
		return result, nil
	}

	return nil, nil
}

// AddVdpa adds a vDPA device to the VM instance.
// Returns PciDeviceInfo when the device is hot-added (200); nil when cold-added (204).
func (c *CloudHypervisorClient) AddVdpa(ctx context.Context, vdpa VdpaConfig) (*PciDeviceInfo, error) {
	body, status, err := c.doRequest(ctx, http.MethodPut, "vm.add-vdpa", vdpa)
	if err != nil {
		return nil, err
	}

	if status == http.StatusOK {
		result := &PciDeviceInfo{}
		if err := json.Unmarshal(body, result); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}
		return result, nil
	}

	return nil, nil
}

// AddUserDevice adds a userspace device to the VM instance.
// Returns PciDeviceInfo when the device is hot-added (200); nil when cold-added (204).
func (c *CloudHypervisorClient) AddUserDevice(ctx context.Context, device VmAddUserDevice) (*PciDeviceInfo, error) {
	body, status, err := c.doRequest(ctx, http.MethodPut, "vm.add-user-device", device)
	if err != nil {
		return nil, err
	}

	if status == http.StatusOK {
		result := &PciDeviceInfo{}
		if err := json.Unmarshal(body, result); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}
		return result, nil
	}

	return nil, nil
}

// SnapshotVM takes a snapshot of the VM instance to the given destination URL.
func (c *CloudHypervisorClient) SnapshotVM(ctx context.Context, snapshot VmSnapshotConfig) error {
	_, _, err := c.doRequest(ctx, http.MethodPut, "vm.snapshot", snapshot)
	return err
}

// CoredumpVM takes a coredump of the VM instance to the given destination URL.
func (c *CloudHypervisorClient) CoredumpVM(ctx context.Context, coredump VmCoredumpData) error {
	_, _, err := c.doRequest(ctx, http.MethodPut, "vm.coredump", coredump)
	return err
}

// RestoreVM restores the VM instance from a previously taken snapshot.
func (c *CloudHypervisorClient) RestoreVM(ctx context.Context, restore RestoreConfig) error {
	_, _, err := c.doRequest(ctx, http.MethodPut, "vm.restore", restore)
	return err
}

// ReceiveMigration prepares the VMM to receive an incoming VM migration.
func (c *CloudHypervisorClient) ReceiveMigration(ctx context.Context, data ReceiveMigrationData) error {
	_, _, err := c.doRequest(ctx, http.MethodPut, "vm.receive-migration", data)
	return err
}

// SendMigration migrates the VM instance to a remote destination.
func (c *CloudHypervisorClient) SendMigration(ctx context.Context, data SendMigrationData) error {
	_, _, err := c.doRequest(ctx, http.MethodPut, "vm.send-migration", data)
	return err
}
