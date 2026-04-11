package chdriver

type OverlayDisk struct {
	baseImagePath string
	baseImageType string
	overlayPath   string
}

func NewOverlayDiskFromDiskConfig(diskConfig TaskDiskConfig, overLayPath string) *OverlayDisk {
	baseImageType := diskConfig.ImageType
	if baseImageType == "" {
		baseImageType = "raw"
	}

	return &OverlayDisk{
		baseImagePath: diskConfig.Path,
		overlayPath:   overLayPath,
		baseImageType: baseImageType,
	}
}

func (o *OverlayDisk) Create() error {
	// Use qemu-img to create a new overlay image with the base image as the backing file.
	cmd := []string{
		"qemu-img",
		"create",
		"-F", o.baseImageType,
		"-f", "qcow2",
		"-b", o.baseImagePath,
		o.overlayPath,
	}

	return run(cmd[0], cmd[1:]...)
}

func (o *OverlayDisk) BaseImagePath() string {
	return o.baseImagePath
}

func (o *OverlayDisk) OverlayPath() string {
	return o.overlayPath
}
