package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/perm"
)

func TestToolLabel(t *testing.T) {
	tests := []struct {
		name string
		tool config.TargetTool
		want string
	}{
		{name: "auto", tool: config.TargetTool{Name: "Cursor", Auto: true}, want: "Cursor (Autoconfiguración)"},
		{name: "manual", tool: config.TargetTool{Name: "VS Code", Auto: false}, want: "VS Code (Instrucciones manuales)"},
	}
	for _, test := range tests {
		if got := toolLabel(test.tool); got != test.want {
			t.Errorf("toolLabel(%s) = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestPlatformIDsAndParse(t *testing.T) {
	ids := platformIDs()
	if len(ids) != len(perm.SupportedPlatforms) {
		t.Fatalf("platformIDs() = %d entries, want %d", len(ids), len(perm.SupportedPlatforms))
	}
	for _, p := range perm.SupportedPlatforms {
		if got, err := parsePlatform(string(p)); err != nil || got != p {
			t.Errorf("parsePlatform(%q) = %v, %v; want %v", p, got, err, p)
		}
	}
	if _, err := parsePlatform("unsupported"); err == nil {
		t.Error("parsePlatform(unsupported) expected an error")
	}
}

func TestUpdateAssetName(t *testing.T) {
	name := updateAssetName()
	if !strings.HasPrefix(name, "sv-memory_") {
		t.Errorf("updateAssetName() = %q, want sv-memory_ prefix", name)
	}
	if strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".tar.gz") {
		// valid extension for the current platform
	} else {
		t.Errorf("updateAssetName() = %q, want .zip or .tar.gz", name)
	}
}

func TestSHA256File(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(path, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}
	hash, err := sha256File(path)
	if err != nil {
		t.Fatalf("sha256File() error = %v", err)
	}
	// sha256("hello world") = b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9
	if hash != "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9" {
		t.Errorf("sha256File() = %q, want known hash", hash)
	}
	if _, err := sha256File(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("sha256File(missing) expected an error")
	}
}

func TestExtractTarBinary(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "bundle.tar.gz")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	payload := []byte("#!/bin/sh\necho hi\n")
	if err := tw.WriteHeader(&tar.Header{Name: "sv-memory", Mode: 0o755, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "out")
	if err := extractTarBinary(archive, dest); err != nil {
		t.Fatalf("extractTarBinary() error = %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, payload) {
		t.Errorf("extracted = %q, %v; want %q", got, err, payload)
	}

	if err := extractTarBinary(filepath.Join(t.TempDir(), "empty.tar.gz"), dest); err == nil {
		t.Error("extractTarBinary(no regular file) expected an error")
	}
}

func TestExtractZipBinary(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "bundle.zip")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	payload := []byte("MZ fake binary")
	w, err := zw.Create("sv-memory.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, writeErr := w.Write(payload); writeErr != nil {
		t.Fatal(writeErr)
	}
	if closeErr := zw.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if writeErr := os.WriteFile(archive, buf.Bytes(), 0644); writeErr != nil {
		t.Fatal(writeErr)
	}

	dest := filepath.Join(t.TempDir(), "out.exe")
	if extractErr := extractZipBinary(archive, dest); extractErr != nil {
		t.Fatalf("extractZipBinary() error = %v", extractErr)
	}
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, payload) {
		t.Errorf("extracted = %q, %v; want %q", got, err, payload)
	}
}

func TestCurrentExecutablePath(t *testing.T) {
	exe, err := currentExecutablePath()
	if err != nil {
		t.Fatalf("currentExecutablePath() error = %v", err)
	}
	if exe == "" {
		t.Error("currentExecutablePath() returned empty path")
	}
}

func TestConfigureThemeAndKeyMap(t *testing.T) {
	if configureTheme() == nil {
		t.Error("configureTheme() returned nil")
	}
	if configureKeyMap() == nil {
		t.Error("configureKeyMap() returned nil")
	}
}

func TestPlatformNames(t *testing.T) {
	tools := []config.TargetTool{
		{Name: "Cursor", Auto: true},
		{Name: "Claude Code", Auto: true},
	}
	if got := platformNames(tools); got != "Cursor, Claude Code" {
		t.Errorf("platformNames() = %q, want Cursor, Claude Code", got)
	}
	if got := platformNames(nil); got != "" {
		t.Errorf("platformNames(nil) = %q, want empty", got)
	}
}

func TestPrintGrantResult(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printGrantResult(&perm.Result{
		Skipped:    true,
		SkippedMsg: "open code uses interactive approval",
	})
	printGrantResult(&perm.Result{
		ConfigPath: "/tmp/settings.json",
		Added:      []string{"sv_mem_search"},
		Present:    []string{"sv_mem_save"},
		DryRun:     true,
	})
	printGrantResult(&perm.Result{
		ConfigPath: "/tmp/settings.json",
		Added:      []string{"sv_mem_search", "sv_mem_get"},
	})

	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	text := string(out)
	for _, want := range []string{"open code uses interactive approval", "[dry-run]", "Added 2"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}
}
