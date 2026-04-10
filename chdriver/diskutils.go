package chdriver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
	"oras.land/oras-go/v2/registry/remote/retry"
)

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

type PullOptions struct {
	Reference string // e.g. ghcr.io/myorg/vm-images/ubuntu22.04:2026-04-02
	CacheDir  string // absolute base cache dir
}

type PulledArtifact struct {
	WorkDir string
}

func PullIntoCache(ctx context.Context, opts PullOptions) (*PulledArtifact, error) {
	repo, err := remote.NewRepository(opts.Reference)
	if err != nil {
		return nil, fmt.Errorf("create repository: %w", err)
	}

	store, err := credentials.NewStoreFromDocker(credentials.StoreOptions{})
	if err != nil {
		return nil, fmt.Errorf("new credentials store: %w", err)
	}

	repo.Client = &auth.Client{
		Client:     retry.DefaultClient,
		Cache:      auth.NewCache(),
		Credential: credentials.Credential(store),
	}

	// Resolve the tag/digest first so we get the immutable manifest descriptor.
	desc, err := repo.Resolve(ctx, repo.Reference.Reference)
	if err != nil {
		return nil, fmt.Errorf("resolve reference: %w", err)
	}

	workDir := filepath.Join(opts.CacheDir, "sha256-"+desc.Digest.Encoded())
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir cache dir: %w", err)
	}

	// file.New expects a filesystem directory store.
	fs, err := file.New(workDir)
	if err != nil {
		return nil, fmt.Errorf("open file store: %w", err)
	}
	defer fs.Close()

	// Copy the artifact graph rooted at the resolved reference into the file store.
	_, err = oras.Copy(ctx, repo, repo.Reference.Reference, fs, "", oras.DefaultCopyOptions)
	if err != nil {
		return nil, fmt.Errorf("copy artifact to cache: %w", err)
	}

	return &PulledArtifact{WorkDir: workDir}, nil
}
