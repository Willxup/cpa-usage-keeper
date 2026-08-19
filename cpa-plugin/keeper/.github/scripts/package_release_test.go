package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageLibraryWritesRootLibraryEntryAndChecksum(t *testing.T) {
	dir := t.TempDir()
	libraryPath := filepath.Join(dir, "keeper.so")
	archivePath := filepath.Join(dir, "keeper_0.1.0_linux_amd64.zip")
	checksumPath := archivePath + ".sha256"

	if err := os.WriteFile(libraryPath, []byte("plugin-binary"), 0o644); err != nil {
		t.Fatalf("write library: %v", err)
	}

	archiveData, err := packageLibrary(libraryPath, archivePath)
	if err != nil {
		t.Fatalf("packageLibrary() error = %v", err)
	}
	if err := writeChecksum(checksumPath, archivePath, archiveData); err != nil {
		t.Fatalf("writeChecksum() error = %v", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(archiveData), int64(len(archiveData)))
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	if len(reader.File) != 1 {
		t.Fatalf("zip entry count = %d, want 1", len(reader.File))
	}
	entry := reader.File[0]
	if entry.Name != "keeper.so" {
		t.Fatalf("zip entry name = %q, want keeper.so", entry.Name)
	}
	if entry.FileInfo().Mode().Perm() != 0o755 {
		t.Fatalf("zip entry mode = %v, want 0755", entry.FileInfo().Mode().Perm())
	}

	checksumRaw, err := os.ReadFile(checksumPath)
	if err != nil {
		t.Fatalf("read checksum: %v", err)
	}
	sum := sha256.Sum256(archiveData)
	wantLine := hex.EncodeToString(sum[:]) + "  keeper_0.1.0_linux_amd64.zip\n"
	if string(checksumRaw) != wantLine {
		t.Fatalf("checksum line = %q, want %q", string(checksumRaw), wantLine)
	}
	if strings.Contains(string(checksumRaw), string(filepath.Separator)+"keeper_0.1.0_linux_amd64.zip") {
		t.Fatalf("checksum line includes a path: %q", string(checksumRaw))
	}
}
