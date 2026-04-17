package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-hclog"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// PullOptions describes how to pull an OCI artifact into a local cache.
type PullOptions struct {
	Reference string // e.g. ghcr.io/myorg/vm-images/ubuntu22.04:2026-04-02
	CacheDir  string // absolute base cache dir
}

// PulledArtifact describes the location of a materialized OCI artifact.
type PulledArtifact struct {
	WorkDir string
}

// PullIntoCache resolves, fetches, and materializes an OCI artifact into a
// deterministic cache directory based on the manifest digest.
func PullIntoCache(ctx context.Context, opts PullOptions, logger hclog.Logger) (*PulledArtifact, error) {
	repo, err := openRepository(opts.Reference)
	if err != nil {
		return nil, err
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

	if err := MaterializeImage(ctx, repo, repo.Reference.Reference, workDir); err != nil {
		return nil, fmt.Errorf("materialize OCI image: %w", err)
	}

	return &PulledArtifact{WorkDir: workDir}, nil
}

// FetchMetadata returns the raw OCI config blob, which the driver stores as metadata.json.
func FetchMetadata(ctx context.Context, reference string) ([]byte, error) {
	repo, err := openRepository(reference)
	if err != nil {
		return nil, err
	}

	manifest, err := fetchManifest(ctx, repo, repo.Reference.Reference)
	if err != nil {
		return nil, err
	}
	if manifest.Config.Digest == "" {
		return nil, fmt.Errorf("manifest for %q has no config blob", reference)
	}

	rc, err := repo.Fetch(ctx, manifest.Config)
	if err != nil {
		return nil, fmt.Errorf("fetch config blob %s: %w", manifest.Config.Digest, err)
	}
	defer rc.Close()

	configBytes, err := content.ReadAll(rc, manifest.Config)
	if err != nil {
		return nil, fmt.Errorf("read config blob %s: %w", manifest.Config.Digest, err)
	}
	return configBytes, nil
}

// MaterializeImage fetches the manifest/config/layers and writes them to
// workDir using the driver-expected filenames.
func MaterializeImage(ctx context.Context, repo *remote.Repository, ref, workDir string) error {
	manifest, err := fetchManifest(ctx, repo, ref)
	if err != nil {
		return err
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

func openRepository(reference string) (*remote.Repository, error) {
	repo, err := remote.NewRepository(reference)
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
	return repo, nil
}

func fetchManifest(ctx context.Context, repo *remote.Repository, ref string) (*ocispec.Manifest, error) {
	manifestDesc, err := repo.Resolve(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve manifest %q: %w", ref, err)
	}

	rc, err := repo.Fetch(ctx, manifestDesc)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest %s: %w", manifestDesc.Digest, err)
	}
	defer rc.Close()

	manifestBytes, err := content.ReadAll(rc, manifestDesc)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", manifestDesc.Digest, err)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	return &manifest, nil
}

func isLocalRegistry(host string) bool {
	host = strings.ToLower(host)
	return host == "localhost" ||
		strings.HasPrefix(host, "localhost:") ||
		strings.HasPrefix(host, "127.0.0.1") ||
		strings.HasPrefix(host, "[::1]")
}
