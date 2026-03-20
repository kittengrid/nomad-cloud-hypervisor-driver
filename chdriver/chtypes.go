package chdriver

// VmmPingResponse holds the response from the /vmm.ping endpoint.
type VmmPingResponse struct {
	BuildVersion string   `json:"build_version"`
	Version      string   `json:"version"`
	Pid          int      `json:"pid"`
	Features     []string `json:"features"`
}

// VmState represents the current state of a VM instance.
type VmState string

const (
	VmStateCreated  VmState = "Created"
	VmStateRunning  VmState = "Running"
	VmStateShutdown VmState = "Shutdown"
	VmStatePaused   VmState = "Paused"
)

// VmInfo holds general information about a VM instance.
type VmInfo struct {
	Config            VmConfig               `json:"config"`
	State             VmState                `json:"state"`
	MemoryActualSize  *int64                 `json:"memory_actual_size,omitempty"`
	DeviceTree        map[string]DeviceNode  `json:"device_tree,omitempty"`
}

// DeviceNode represents a node in the VM device tree.
type DeviceNode struct {
	ID       string   `json:"id,omitempty"`
	Children []string `json:"children,omitempty"`
	PciBdf   string   `json:"pci_bdf,omitempty"`
}

// VmCounters holds per-device counters keyed by device name and counter name.
type VmCounters map[string]map[string]int64

// PciDeviceInfo holds identifying information about a PCI device.
type PciDeviceInfo struct {
	ID  string `json:"id"`
	Bdf string `json:"bdf"`
}

// PayloadConfig describes the bootable payload of a VM.
type PayloadConfig struct {
	Firmware  string `json:"firmware,omitempty"`
	Kernel    string `json:"kernel,omitempty"`
	Cmdline   string `json:"cmdline,omitempty"`
	Initramfs string `json:"initramfs,omitempty"`
	Igvm      string `json:"igvm,omitempty"`
	HostData  string `json:"host_data,omitempty"`
}

// CpuTopology describes the CPU topology exposed to the guest.
type CpuTopology struct {
	ThreadsPerCore  *int `json:"threads_per_core,omitempty"`
	CoresPerDie     *int `json:"cores_per_die,omitempty"`
	DiesPerPackage  *int `json:"dies_per_package,omitempty"`
	Packages        *int `json:"packages,omitempty"`
}

// CpuAffinity pins a vCPU to a set of host CPUs.
type CpuAffinity struct {
	Vcpu     int   `json:"vcpu"`
	HostCpus []int `json:"host_cpus"`
}

// CpuFeatures enables optional CPU feature flags.
type CpuFeatures struct {
	Amx *bool `json:"amx,omitempty"`
}

// CoreSchedulingMode controls the core scheduling granularity.
type CoreSchedulingMode string

const (
	CoreSchedulingModeVm   CoreSchedulingMode = "Vm"
	CoreSchedulingModeVcpu CoreSchedulingMode = "Vcpu"
	CoreSchedulingModeOff  CoreSchedulingMode = "Off"
)

// CpusConfig describes the CPU configuration of a VM.
type CpusConfig struct {
	BootVcpus      int                 `json:"boot_vcpus"`
	MaxVcpus       int                 `json:"max_vcpus"`
	Topology       *CpuTopology        `json:"topology,omitempty"`
	KvmHyperv      *bool               `json:"kvm_hyperv,omitempty"`
	MaxPhysBits    *int                `json:"max_phys_bits,omitempty"`
	Nested         *bool               `json:"nested,omitempty"`
	Affinity       []CpuAffinity       `json:"affinity,omitempty"`
	Features       *CpuFeatures        `json:"features,omitempty"`
	CoreScheduling *CoreSchedulingMode `json:"core_scheduling,omitempty"`
}

// MemoryZoneConfig describes a single memory zone.
type MemoryZoneConfig struct {
	ID             string `json:"id"`
	Size           int64  `json:"size"`
	File           string `json:"file,omitempty"`
	Mergeable      *bool  `json:"mergeable,omitempty"`
	Shared         *bool  `json:"shared,omitempty"`
	Hugepages      *bool  `json:"hugepages,omitempty"`
	HugepageSize   *int64 `json:"hugepage_size,omitempty"`
	HostNumaNode   *int32 `json:"host_numa_node,omitempty"`
	HotplugSize    *int64 `json:"hotplug_size,omitempty"`
	HotpluggedSize *int64 `json:"hotplugged_size,omitempty"`
	Prefault       *bool  `json:"prefault,omitempty"`
}

// MemoryConfig describes the memory configuration of a VM.
type MemoryConfig struct {
	Size           int64              `json:"size"`
	HotplugSize    *int64             `json:"hotplug_size,omitempty"`
	HotpluggedSize *int64             `json:"hotplugged_size,omitempty"`
	Mergeable      *bool              `json:"mergeable,omitempty"`
	HotplugMethod  string             `json:"hotplug_method,omitempty"`
	Shared         *bool              `json:"shared,omitempty"`
	Hugepages      *bool              `json:"hugepages,omitempty"`
	HugepageSize   *int64             `json:"hugepage_size,omitempty"`
	Prefault       *bool              `json:"prefault,omitempty"`
	Thp            *bool              `json:"thp,omitempty"`
	Zones          []MemoryZoneConfig `json:"zones,omitempty"`
}

// TokenBucket defines a rate-limiting token bucket.
type TokenBucket struct {
	Size         int64  `json:"size"`
	OneTimeBurst *int64 `json:"one_time_burst,omitempty"`
	RefillTime   int64  `json:"refill_time"`
}

// RateLimiterConfig configures independent bandwidth and ops rate limits.
type RateLimiterConfig struct {
	Bandwidth *TokenBucket `json:"bandwidth,omitempty"`
	Ops       *TokenBucket `json:"ops,omitempty"`
}

// RateLimitGroupConfig associates a named rate limit group with a limiter config.
type RateLimitGroupConfig struct {
	ID                string            `json:"id"`
	RateLimiterConfig RateLimiterConfig `json:"rate_limiter_config"`
}

// VirtQueueAffinity pins a virtqueue to a set of host CPUs.
type VirtQueueAffinity struct {
	QueueIndex int   `json:"queue_index"`
	HostCpus   []int `json:"host_cpus"`
}

// ImageType identifies the disk image format.
type ImageType string

const (
	ImageTypeFixedVhd ImageType = "FixedVhd"
	ImageTypeQcow2    ImageType = "Qcow2"
	ImageTypeRaw      ImageType = "Raw"
	ImageTypeVhdx     ImageType = "Vhdx"
	ImageTypeUnknown  ImageType = "Unknown"
)

// LockGranularity controls disk locking granularity.
type LockGranularity string

const (
	LockGranularityByteRange LockGranularity = "ByteRange"
	LockGranularityFull      LockGranularity = "Full"
)

// DiskConfig describes a disk device attached to a VM.
type DiskConfig struct {
	Path              string             `json:"path,omitempty"`
	Readonly          *bool              `json:"readonly,omitempty"`
	Direct            *bool              `json:"direct,omitempty"`
	Iommu             *bool              `json:"iommu,omitempty"`
	NumQueues         *int               `json:"num_queues,omitempty"`
	QueueSize         *int               `json:"queue_size,omitempty"`
	VhostUser         *bool              `json:"vhost_user,omitempty"`
	VhostSocket       string             `json:"vhost_socket,omitempty"`
	RateLimiterConfig *RateLimiterConfig `json:"rate_limiter_config,omitempty"`
	PciSegment        *int16             `json:"pci_segment,omitempty"`
	ID                string             `json:"id,omitempty"`
	Serial            string             `json:"serial,omitempty"`
	RateLimitGroup    string             `json:"rate_limit_group,omitempty"`
	QueueAffinity     []VirtQueueAffinity `json:"queue_affinity,omitempty"`
	BackingFiles      *bool              `json:"backing_files,omitempty"`
	Sparse            *bool              `json:"sparse,omitempty"`
	ImageType         *ImageType         `json:"image_type,omitempty"`
	LockGranularity   *LockGranularity   `json:"lock_granularity,omitempty"`
}

// NetConfig describes a network device attached to a VM.
type NetConfig struct {
	Tap               string             `json:"tap,omitempty"`
	IP                string             `json:"ip,omitempty"`
	Mask              string             `json:"mask,omitempty"`
	Mac               string             `json:"mac,omitempty"`
	HostMac           string             `json:"host_mac,omitempty"`
	Mtu               *int               `json:"mtu,omitempty"`
	Iommu             *bool              `json:"iommu,omitempty"`
	NumQueues         *int               `json:"num_queues,omitempty"`
	QueueSize         *int               `json:"queue_size,omitempty"`
	VhostUser         *bool              `json:"vhost_user,omitempty"`
	VhostSocket       string             `json:"vhost_socket,omitempty"`
	VhostMode         string             `json:"vhost_mode,omitempty"`
	ID                string             `json:"id,omitempty"`
	PciSegment        *int16             `json:"pci_segment,omitempty"`
	RateLimiterConfig *RateLimiterConfig `json:"rate_limiter_config,omitempty"`
	OffloadTso        *bool              `json:"offload_tso,omitempty"`
	OffloadUfo        *bool              `json:"offload_ufo,omitempty"`
	OffloadCsum       *bool              `json:"offload_csum,omitempty"`
}

// RngConfig describes the virtual RNG device.
type RngConfig struct {
	Src   string `json:"src"`
	Iommu *bool  `json:"iommu,omitempty"`
}

// BalloonConfig describes the virtio balloon device.
type BalloonConfig struct {
	Size              int64 `json:"size"`
	DeflateOnOom      *bool `json:"deflate_on_oom,omitempty"`
	FreePageReporting *bool `json:"free_page_reporting,omitempty"`
}

// FsConfig describes a virtio-fs device.
type FsConfig struct {
	Tag        string `json:"tag"`
	Socket     string `json:"socket"`
	NumQueues  int    `json:"num_queues"`
	QueueSize  int    `json:"queue_size"`
	PciSegment *int16 `json:"pci_segment,omitempty"`
	ID         string `json:"id,omitempty"`
}

// PmemConfig describes a persistent memory device.
type PmemConfig struct {
	File          string `json:"file"`
	Size          *int64 `json:"size,omitempty"`
	Iommu         *bool  `json:"iommu,omitempty"`
	DiscardWrites *bool  `json:"discard_writes,omitempty"`
	PciSegment    *int16 `json:"pci_segment,omitempty"`
	ID            string `json:"id,omitempty"`
}

// ConsoleMode controls how a console device is exposed.
type ConsoleMode string

const (
	ConsoleModeOff    ConsoleMode = "Off"
	ConsoleModePty    ConsoleMode = "Pty"
	ConsoleModeTty    ConsoleMode = "Tty"
	ConsoleModeFile   ConsoleMode = "File"
	ConsoleModeSocket ConsoleMode = "Socket"
	ConsoleModeNull   ConsoleMode = "Null"
)

// ConsoleConfig describes a serial or virtio console device.
type ConsoleConfig struct {
	File   string      `json:"file,omitempty"`
	Socket string      `json:"socket,omitempty"`
	Mode   ConsoleMode `json:"mode"`
	Iommu  *bool       `json:"iommu,omitempty"`
}

// DebugConsoleConfig describes the debug console device.
type DebugConsoleConfig struct {
	File   string      `json:"file,omitempty"`
	Mode   ConsoleMode `json:"mode"`
	Iobase *int        `json:"iobase,omitempty"`
}

// DeviceConfig describes a host device passed through to the VM.
type DeviceConfig struct {
	Path              string `json:"path"`
	Iommu             *bool  `json:"iommu,omitempty"`
	PciSegment        *int16 `json:"pci_segment,omitempty"`
	ID                string `json:"id,omitempty"`
	XNvGpudirectClique *int8 `json:"x_nv_gpudirect_clique,omitempty"`
}

// VdpaConfig describes a vDPA device.
type VdpaConfig struct {
	Path       string `json:"path"`
	NumQueues  int    `json:"num_queues"`
	Iommu      *bool  `json:"iommu,omitempty"`
	PciSegment *int16 `json:"pci_segment,omitempty"`
	ID         string `json:"id,omitempty"`
}

// VsockConfig describes a virtio vsock device.
type VsockConfig struct {
	Cid        int64  `json:"cid"`
	Socket     string `json:"socket"`
	Iommu      *bool  `json:"iommu,omitempty"`
	PciSegment *int16 `json:"pci_segment,omitempty"`
	ID         string `json:"id,omitempty"`
}

// NumaDistance describes the NUMA distance between two nodes.
type NumaDistance struct {
	Destination int32 `json:"destination"`
	Distance    int32 `json:"distance"`
}

// NumaConfig describes a NUMA node.
type NumaConfig struct {
	GuestNumaID  int32          `json:"guest_numa_id"`
	Cpus         []int32        `json:"cpus,omitempty"`
	Distances    []NumaDistance `json:"distances,omitempty"`
	MemoryZones  []string       `json:"memory_zones,omitempty"`
	PciSegments  []int32        `json:"pci_segments,omitempty"`
	DeviceID     string         `json:"device_id,omitempty"`
}

// PciSegmentConfig describes a PCI segment.
type PciSegmentConfig struct {
	PciSegment          int16  `json:"pci_segment"`
	Mmio32ApertureWeight *int32 `json:"mmio32_aperture_weight,omitempty"`
	Mmio64ApertureWeight *int32 `json:"mmio64_aperture_weight,omitempty"`
}

// PlatformConfig describes platform-level VM configuration.
type PlatformConfig struct {
	NumPciSegments    *int16   `json:"num_pci_segments,omitempty"`
	IommuSegments     []int16  `json:"iommu_segments,omitempty"`
	IommuAddressWidth *uint8   `json:"iommu_address_width,omitempty"`
	SerialNumber      string   `json:"serial_number,omitempty"`
	UUID              string   `json:"uuid,omitempty"`
	OemStrings        []string `json:"oem_strings,omitempty"`
	Tdx               *bool    `json:"tdx,omitempty"`
	SevSnp            *bool    `json:"sev_snp,omitempty"`
}

// TpmConfig describes a TPM device.
type TpmConfig struct {
	Socket string `json:"socket"`
}

// LandlockConfig describes a Landlock filesystem rule.
type LandlockConfig struct {
	Path   string `json:"path"`
	Access string `json:"access"`
}

// VmConfig is the top-level configuration used to create a VM instance.
type VmConfig struct {
	Cpus             *CpusConfig            `json:"cpus,omitempty"`
	Memory           *MemoryConfig          `json:"memory,omitempty"`
	Payload          PayloadConfig          `json:"payload"`
	RateLimitGroups  []RateLimitGroupConfig  `json:"rate_limit_groups,omitempty"`
	Disks            []DiskConfig           `json:"disks,omitempty"`
	Net              []NetConfig            `json:"net,omitempty"`
	Rng              *RngConfig             `json:"rng,omitempty"`
	Balloon          *BalloonConfig         `json:"balloon,omitempty"`
	Fs               []FsConfig             `json:"fs,omitempty"`
	Pmem             []PmemConfig           `json:"pmem,omitempty"`
	Serial           *ConsoleConfig         `json:"serial,omitempty"`
	Console          *ConsoleConfig         `json:"console,omitempty"`
	DebugConsole     *DebugConsoleConfig    `json:"debug_console,omitempty"`
	Devices          []DeviceConfig         `json:"devices,omitempty"`
	Vdpa             []VdpaConfig           `json:"vdpa,omitempty"`
	Vsock            *VsockConfig           `json:"vsock,omitempty"`
	Numa             []NumaConfig           `json:"numa,omitempty"`
	Iommu            *bool                  `json:"iommu,omitempty"`
	Watchdog         *bool                  `json:"watchdog,omitempty"`
	Pvpanic          *bool                  `json:"pvpanic,omitempty"`
	PciSegments      []PciSegmentConfig     `json:"pci_segments,omitempty"`
	Platform         *PlatformConfig        `json:"platform,omitempty"`
	Tpm              *TpmConfig             `json:"tpm,omitempty"`
	LandlockEnable   *bool                  `json:"landlock_enable,omitempty"`
	LandlockRules    []LandlockConfig       `json:"landlock_rules,omitempty"`
}

// VmResize describes a request to resize the VM's vCPUs or memory.
type VmResize struct {
	DesiredVcpus   *int   `json:"desired_vcpus,omitempty"`
	DesiredRam     *int64 `json:"desired_ram,omitempty"`
	DesiredBalloon *int64 `json:"desired_balloon,omitempty"`
}

// VmResizeDisk describes a request to resize an attached disk.
type VmResizeDisk struct {
	ID          string `json:"id,omitempty"`
	DesiredSize int64  `json:"desired_size,omitempty"`
}

// VmResizeZone describes a request to resize a memory zone.
type VmResizeZone struct {
	ID         string `json:"id,omitempty"`
	DesiredRam int64  `json:"desired_ram,omitempty"`
}

// VmRemoveDevice identifies a device to be removed from a running VM.
type VmRemoveDevice struct {
	ID string `json:"id,omitempty"`
}

// VmSnapshotConfig describes the destination for a VM snapshot.
type VmSnapshotConfig struct {
	DestinationURL string `json:"destination_url,omitempty"`
}

// VmCoredumpData describes the destination for a VM coredump.
type VmCoredumpData struct {
	DestinationURL string `json:"destination_url,omitempty"`
}

// MemoryRestoreMode controls how memory is restored from a snapshot.
type MemoryRestoreMode string

const (
	MemoryRestoreModeCopy     MemoryRestoreMode = "Copy"
	MemoryRestoreModeOnDemand MemoryRestoreMode = "OnDemand"
)

// RestoreConfig describes how to restore a VM from a snapshot.
type RestoreConfig struct {
	SourceURL          string             `json:"source_url"`
	Prefault           *bool              `json:"prefault,omitempty"`
	MemoryRestoreMode  *MemoryRestoreMode `json:"memory_restore_mode,omitempty"`
	Resume             *bool              `json:"resume,omitempty"`
}

// ReceiveMigrationData describes the local endpoint for an incoming migration.
type ReceiveMigrationData struct {
	ReceiverURL string `json:"receiver_url"`
}

// SendMigrationData describes the remote endpoint for an outgoing migration.
type SendMigrationData struct {
	DestinationURL string `json:"destination_url"`
	Local          *bool  `json:"local,omitempty"`
}

// VmAddUserDevice describes a userspace device to be added to a running VM.
type VmAddUserDevice struct {
	Socket string `json:"socket"`
}
