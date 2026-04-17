package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// ProgressFunc is called with a human-readable status message during artifact
// download so callers can surface progress to users (e.g. via Nomad task events).
type ProgressFunc func(msg string)

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
// deterministic cache directory based on the manifest digest. progress, if
// non-nil, is called with status messages during the download.
func PullIntoCache(ctx context.Context, opts PullOptions, logger hclog.Logger, progress ProgressFunc) (*PulledArtifact, error) {
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

	if err := MaterializeImage(ctx, repo, repo.Reference.Reference, workDir, progress); err != nil {
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
// workDir using the driver-expected filenames. Blobs are streamed directly to
// disk without buffering their content in memory. progress, if non-nil, is
// called with status messages as each layer is fetched.
func MaterializeImage(ctx context.Context, repo *remote.Repository, ref, workDir string, progress ProgressFunc) error {
	manifest, err := fetchManifest(ctx, repo, ref)
	if err != nil {
		return err
	}

	if manifest.Config.Digest != "" {
		if err := fetchBlobToFile(ctx, repo, manifest.Config, filepath.Join(workDir, "metadata.json"), nil); err != nil {
			return fmt.Errorf("write metadata.json: %w", err)
		}
	}

	for _, layer := range manifest.Layers {
		name := layer.Annotations[ocispec.AnnotationTitle]
		if name == "" {
			continue
		}
		if progress != nil {
			progress(fmt.Sprintf("Pulling %s", name))
		}
		var onPct func(int)
		if progress != nil && layer.Size > 0 {
			onPct = func(pct int) {
				progress(fmt.Sprintf("Pulling %s: %d%%", name, pct))
			}
		}
		if err := fetchBlobToFile(ctx, repo, layer, filepath.Join(workDir, name), onPct); err != nil {
			return fmt.Errorf("write layer %s: %w", name, err)
		}
		if progress != nil {
			progress(fmt.Sprintf("Pulled %s", name))
		}
	}

	return nil
}

// fetchBlobToFile streams a blob from the repository directly into a local
// file without buffering the content in memory. onPct, if non-nil, is called
// with the download percentage each time progress crosses a 5% boundary.
func fetchBlobToFile(ctx context.Context, repo *remote.Repository, desc ocispec.Descriptor, path string, onPct func(int)) error {
	rc, err := repo.Fetch(ctx, desc)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", desc.Digest, err)
	}
	defer rc.Close()

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()

	var r io.Reader = rc
	if onPct != nil {
		r = &pctReader{r: rc, total: desc.Size, onPct: onPct}
	}
	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("stream to %s: %w", path, err)
	}
	return nil
}

// pctReader wraps an io.Reader and calls onPct each time the download crosses
// a 5-percentage-point boundary, giving callers cheap progress reporting
// without flooding them with per-read callbacks.
type pctReader struct {
	r       io.Reader
	total   int64
	read    int64
	lastPct int
	onPct   func(int)
}

func (p *pctReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.read += int64(n)
		pct := int(p.read * 100 / p.total)
		if pct >= p.lastPct+5 {
			p.lastPct = pct
			p.onPct(pct)
		}
	}
	return n, err
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
