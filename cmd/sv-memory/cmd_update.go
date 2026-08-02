package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	updateAPI         = "https://api.github.com/repos/svtech-code/sv-memory/releases/latest"
	updateDownloadURL = "https://github.com/svtech-code/sv-memory/releases/download"
	httpTimeout       = 30 * time.Second
	downloadTimeout   = 90 * time.Second
	updateUserAgent   = "sv-memory-updater"
)

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type releaseInfo struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for and apply sv-memory updates",
	RunE:  runUpdate,
}

// updateAssetName returns the release asset filename for the current platform.
func updateAssetName() string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("sv-memory_%s_%s.zip", runtime.GOOS, runtime.GOARCH)
	}
	return fmt.Sprintf("sv-memory_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
}

func fetchLatestRelease() (*releaseInfo, error) {
	client := &http.Client{Timeout: httpTimeout}
	req, err := http.NewRequest(http.MethodGet, updateAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", updateUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}
	var rel releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// downloadFile downloads url into dest with a user agent header.
func downloadFile(url, dest string) error {
	client := &http.Client{Timeout: downloadTimeout}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", updateUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// fetchExpectedChecksum downloads checksums.txt for the release and returns the
// expected SHA-256 of the given asset.
func fetchExpectedChecksum(tag, asset string) (string, error) {
	url := fmt.Sprintf("%s/%s/checksums.txt", updateDownloadURL, tag)
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksums.txt returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("asset %s not found in checksums.txt", asset)
}

// extractTarBinary copies the single regular file of a tarball to dest.
func extractTarBinary(archivePath, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeReg {
			out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
	}
	return fmt.Errorf("no regular file found in tarball")
}

// extractZipBinary copies the single regular file of a zip to dest.
func extractZipBinary(archivePath, dest string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		out.Close()
		rc.Close()
		return nil
	}
	return fmt.Errorf("no regular file found in zip")
}

// currentExecutablePath returns the resolved path of the running binary.
func currentExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return filepath.Abs(exe)
}

func runUpdate(cmd *cobra.Command, args []string) error {
	// The auto-updater only makes sense for release binaries. Local dev builds
	// report "dev" and cannot compare against release tags.
	if version == "dev" || version == "" {
		fmt.Println("Current version: dev (built from source)")
		fmt.Println()
		fmt.Println("The auto-updater works with release binaries installed via install.sh / install.ps1.")
		fmt.Println("To update a source build, use:")
		fmt.Println("  go install github.com/svtech-code/sv-memory/cmd/sv-memory@latest")
		return nil
	}

	fmt.Printf("Current version: %s\n", version)

	rel, err := fetchLatestRelease()
	if err != nil {
		fmt.Printf("\nCould not check for updates (%v).\n", err)
		fmt.Println("You can manually check at: https://github.com/svtech-code/sv-memory/releases")
		return nil
	}

	latest := strings.TrimPrefix(rel.TagName, "v")
	current := strings.TrimPrefix(version, "v")
	fmt.Printf("Latest release:  %s\n", rel.TagName)

	if latest == current {
		fmt.Println()
		fmt.Printf("sv-memory is already up to date (%s).\n", version)
		return nil
	}

	asset := updateAssetName()
	fmt.Printf("\nUpdate to %s? (y/N): ", rel.TagName)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		fmt.Println("Update cancelled.")
		return nil
	}

	fmt.Printf("\nDownloading %s...\n", asset)

	tmpDir, err := os.MkdirTemp("", "sv-memory-update-*")
	if err != nil {
		return fmt.Errorf("could not create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Fetch and verify the expected checksum BEFORE installing anything.
	expectedHash, err := fetchExpectedChecksum(rel.TagName, asset)
	if err != nil {
		fmt.Printf("⚠️  Could not verify checksum: %v\n", err)
		fmt.Println("Manual update recommended: https://github.com/svtech-code/sv-memory/releases")
		return nil
	}

	downloadURL := fmt.Sprintf("%s/%s/%s", updateDownloadURL, rel.TagName, asset)
	archivePath := filepath.Join(tmpDir, asset)
	if err := downloadFile(downloadURL, archivePath); err != nil {
		fmt.Printf("⚠️  Download failed: %v\n", err)
		fmt.Println("Manual update recommended: https://github.com/svtech-code/sv-memory/releases")
		return nil
	}

	actualHash, err := sha256File(archivePath)
	if err != nil {
		return fmt.Errorf("could not hash downloaded file: %w", err)
	}
	if actualHash != expectedHash {
		fmt.Println("⚠️  Checksum mismatch!")
		fmt.Printf("  expected: %s\n", expectedHash)
		fmt.Printf("  got:      %s\n", actualHash)
		fmt.Println("Update aborted to protect your system. Manual update recommended:")
		fmt.Println("  https://github.com/svtech-code/sv-memory/releases")
		return nil
	}
	fmt.Println("Checksum verified ✓")

	// Extract the new binary.
	newBin := filepath.Join(tmpDir, "sv-memory")
	if runtime.GOOS == "windows" {
		newBin += ".exe"
	}
	if runtime.GOOS == "windows" {
		if err := extractZipBinary(archivePath, newBin); err != nil {
			return fmt.Errorf("could not extract archive: %w", err)
		}
	} else {
		if err := extractTarBinary(archivePath, newBin); err != nil {
			return fmt.Errorf("could not extract archive: %w", err)
		}
	}

	exe, err := currentExecutablePath()
	if err != nil {
		return fmt.Errorf("could not locate current executable: %w", err)
	}

	// Windows cannot overwrite a running .exe; hand the user a copy command.
	if runtime.GOOS == "windows" {
		fmt.Println()
		fmt.Println("Windows cannot replace a running executable. Copy the new binary manually:")
		fmt.Printf("  copy /Y \"%s\" \"%s\"\n", newBin, exe)
		return nil
	}

	// Atomic replace on Unix: the running process keeps the old inode; new
	// invocations pick up the replacement.
	if err := os.Rename(newBin, exe); err != nil {
		fmt.Printf("⚠️  Could not replace the binary at %s: %v\n", exe, err)
		fmt.Println("You may need write permission. Try copying it manually:")
		fmt.Printf("  cp \"%s\" \"%s\"\n", newBin, exe)
		return nil
	}

	fmt.Printf("\n✓ Updated to %s!\n", rel.TagName)
	fmt.Println("Run 'sv-memory version' to confirm.")
	return nil
}
