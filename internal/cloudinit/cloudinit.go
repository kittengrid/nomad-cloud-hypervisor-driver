// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

package cloudinit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hashicorp/go-hclog"
)

// CreateISO writes a NoCloud seed ISO image at isoPath containing the given
// user-data and meta-data content.  An empty meta-data is valid and satisfies
// cloud-init's NoCloud datasource minimum requirements.
//
// Generation is attempted with the following tools in order of preference:
//  1. cloud-localds  – ships with the cloud-image-utils / cloud-init packages
//  2. genisoimage    – traditional ISO 9660 creator
//  3. mkisofs        – alias / replacement for genisoimage on many distros
//  4. xorrisofs      – modern ISO creator bundled with xorriso
//
// The function returns an error if none of the tools are available or if
// image creation fails.
func CreateISO(userData, metaData, isoPath string, logger hclog.Logger) error {
	tmpDir, err := os.MkdirTemp("", "cloud-init-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	userDataPath := filepath.Join(tmpDir, "user-data")
	if err := os.WriteFile(userDataPath, []byte(userData), 0o600); err != nil {
		return fmt.Errorf("write user-data: %w", err)
	}

	metaDataPath := filepath.Join(tmpDir, "meta-data")
	if err := os.WriteFile(metaDataPath, []byte(metaData), 0o600); err != nil {
		return fmt.Errorf("write meta-data: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(isoPath), 0o755); err != nil {
		return fmt.Errorf("create iso dir: %w", err)
	}

	if path, err := exec.LookPath("cloud-localds"); err == nil {
		logger.Debug("generating cloud-init ISO with cloud-localds", "tool", path)
		return runCloudLocalds(path, isoPath, userDataPath, metaDataPath)
	}

	for _, tool := range []string{"genisoimage", "mkisofs", "xorrisofs"} {
		if path, err := exec.LookPath(tool); err == nil {
			logger.Debug("generating cloud-init ISO", "tool", path)
			return runISOTool(path, isoPath, tmpDir)
		}
	}

	return fmt.Errorf(
		"no ISO generation tool found; install one of: cloud-localds, genisoimage, mkisofs, xorrisofs",
	)
}

func runCloudLocalds(toolPath, isoPath, userDataPath, metaDataPath string) error {
	cmd := exec.Command(toolPath, isoPath, userDataPath, metaDataPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cloud-localds failed: %w\noutput: %s", err, out)
	}
	return nil
}

func runISOTool(toolPath, isoPath, srcDir string) error {
	cmd := exec.Command(toolPath,
		"-output", isoPath,
		"-volid", "cidata",
		"-joliet",
		"-rock",
		"-input-charset", "utf-8",
		srcDir,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed: %w\noutput: %s", filepath.Base(toolPath), err, out)
	}
	return nil
}
