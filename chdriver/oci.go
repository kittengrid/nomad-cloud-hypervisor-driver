package chdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/go-hclog"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
	"oras.land/oras-go/v2/registry/remote/retry"
)

type PullOptions struct {
	Reference string // e.g. ghcr.io/myorg/vm-images/ubuntu22.04:2026-04-02
	CacheDir  string // absolute base cache dir
}

type PulledArtifact struct {
	WorkDir string
}

func PullIntoCache(ctx context.Context, opts PullOptions, logger hclog.Logger) (*PulledArtifact, error) {
	repo, err := remote.NewRepository(opts.Reference)
	if err != nil {
		return nil, fmt.Errorf("create repository: %w", err)
	}
	repo.PlainHTTP = isLocalRegistry(repo.Reference.Registry)

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

	if err := materializeOCIImage(ctx, repo, repo.Reference.Reference, workDir, logger); err == nil {
		return &PulledArtifact{WorkDir: workDir}, nil
	} else {
		return nil, fmt.Errorf("materialize OCI image: %w", err)
	}
}

func materializeOCIImage(ctx context.Context, repo *remote.Repository, ref string, workDir string, logger hclog.Logger) error {

	manifestDesc, err := repo.Resolve(ctx, ref)
	if err != nil {
		return fmt.Errorf("resolve manifest %q: %w", ref, err)
	}

	rc, err := repo.Fetch(ctx, manifestDesc)
	if err != nil {
		return fmt.Errorf("fetch manifest %s: %w", manifestDesc.Digest, err)
	}
	defer rc.Close()

	manifestBytes, err := content.ReadAll(rc, manifestDesc)
	if err != nil {
		return fmt.Errorf("read manifest %s: %w", manifestDesc.Digest, err)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("decode manifest: %w", err)
	}

	if manifest.Config.Digest != "" {
		rc, err := repo.Fetch(ctx, manifest.Config)
		if err != nil {
			return fmt.Errorf("fetch config blob %s: %w", manifest.Config.Digest, err)
		}
		configBytes, err := content.ReadAll(rc, manifest.Config)
		rc.Close()
		if err != nil {
			return fmt.Errorf("read config blob %s: %w", manifest.Config.Digest, err)
		}
		if err := os.WriteFile(filepath.Join(workDir, "metadata.json"), configBytes, 0o644); err != nil {
			return fmt.Errorf("write metadata.json: %w", err)
		}
	}

	for _, layer := range manifest.Layers {
		name := layer.Annotations[ocispec.AnnotationTitle]
		if name == "" {
			continue
		}

		rc, err := repo.Fetch(ctx, layer)
		if err != nil {
			return fmt.Errorf("fetch layer %s (%s): %w", name, layer.Digest, err)
		}
		data, err := content.ReadAll(rc, layer)
		rc.Close()
		if err != nil {
			return fmt.Errorf("read layer %s (%s): %w", name, layer.Digest, err)
		}

		if err := os.WriteFile(filepath.Join(workDir, name), data, 0o644); err != nil {
			return fmt.Errorf("write layer %s: %w", name, err)
		}
	}

	return nil
}
