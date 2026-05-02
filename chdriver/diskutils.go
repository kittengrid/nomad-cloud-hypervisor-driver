package chdriver

import (
	"fmt"
	"syscall"
)

// xfsMagic is the filesystem type identifier for XFS as returned by statfs(2).
const xfsMagic = 0x58465342

// isSameXFSPartition reports whether path1 and path2 live on the same XFS
// partition. path1 and path2 must both already exist (pass a directory for
// paths whose final component has not been created yet).
func isSameXFSPartition(path1, path2 string) (bool, error) {
	var st1, st2 syscall.Stat_t
	if err := syscall.Stat(path1, &st1); err != nil {
		return false, fmt.Errorf("stat %s: %w", path1, err)
	}
	if err := syscall.Stat(path2, &st2); err != nil {
		return false, fmt.Errorf("stat %s: %w", path2, err)
	}
	if st1.Dev != st2.Dev {
		return false, nil
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path1, &fs); err != nil {
		return false, fmt.Errorf("statfs %s: %w", path1, err)
	}
	return fs.Type == xfsMagic, nil
}

type OverlayDisk struct {
	baseImagePath string
	baseImageType string
	overlayPath   string
	useReflink    bool
}

func NewOverlayDiskFromDiskConfig(diskConfig TaskDiskConfig, overlayPath string, useReflink bool) *OverlayDisk {
	baseImageType := diskConfig.ImageType
	if baseImageType == "" {
		baseImageType = "raw"
	}

	return &OverlayDisk{
		baseImagePath: diskConfig.Path,
		overlayPath:   overlayPath,
		baseImageType: baseImageType,
		useReflink:    useReflink,
	}
}

func (o *OverlayDisk) Create() error {
	if o.useReflink {
		return run("cp", "--reflink=always", o.baseImagePath, o.overlayPath)
	}
	return run("qemu-img", "create",
		"-F", o.baseImageType,
		"-f", "qcow2",
		"-b", o.baseImagePath,
		o.overlayPath,
	)
}

func (o *OverlayDisk) BaseImagePath() string {
	return o.baseImagePath
}

func (o *OverlayDisk) OverlayPath() string {
	return o.overlayPath
}
