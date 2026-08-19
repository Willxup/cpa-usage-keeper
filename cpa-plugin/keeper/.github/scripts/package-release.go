package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	libraryPath := flag.String("library", "", "path to the compiled plugin library")
	archivePath := flag.String("archive", "", "path to the output zip archive")
	checksumPath := flag.String("checksum", "", "path to the output checksum file")
	flag.Parse()

	if *libraryPath == "" || *archivePath == "" || *checksumPath == "" {
		fatalf("library, archive, and checksum are required")
	}
	archiveData, errPackage := packageLibrary(*libraryPath, *archivePath)
	if errPackage != nil {
		fatalf("%v", errPackage)
	}
	if errChecksum := writeChecksum(*checksumPath, *archivePath, archiveData); errChecksum != nil {
		fatalf("%v", errChecksum)
	}
}

func packageLibrary(libraryPath, archivePath string) ([]byte, error) {
	library, errOpen := os.Open(libraryPath)
	if errOpen != nil {
		return nil, fmt.Errorf("open library: %w", errOpen)
	}
	defer func() {
		if errClose := library.Close(); errClose != nil {
			fmt.Fprintf(os.Stderr, "close library: %v\n", errClose)
		}
	}()

	info, errStat := library.Stat()
	if errStat != nil {
		return nil, fmt.Errorf("stat library: %w", errStat)
	}
	if errMkdir := os.MkdirAll(filepath.Dir(archivePath), 0o755); errMkdir != nil {
		return nil, fmt.Errorf("create archive directory: %w", errMkdir)
	}
	archive, errCreate := os.Create(archivePath)
	if errCreate != nil {
		return nil, fmt.Errorf("create archive: %w", errCreate)
	}
	archiveClosed := false
	defer func() {
		if !archiveClosed {
			if errClose := archive.Close(); errClose != nil {
				fmt.Fprintf(os.Stderr, "close archive: %v\n", errClose)
			}
		}
	}()

	writer := zip.NewWriter(archive)
	header, errHeader := zip.FileInfoHeader(info)
	if errHeader != nil {
		return nil, fmt.Errorf("create zip header: %w", errHeader)
	}
	header.Name = filepath.Base(libraryPath)
	header.Method = zip.Deflate
	header.SetMode(0o755)
	entry, errEntry := writer.CreateHeader(header)
	if errEntry != nil {
		return nil, fmt.Errorf("create zip entry: %w", errEntry)
	}
	if _, errCopy := io.Copy(entry, library); errCopy != nil {
		return nil, fmt.Errorf("copy library: %w", errCopy)
	}
	if errClose := writer.Close(); errClose != nil {
		return nil, fmt.Errorf("close zip writer: %w", errClose)
	}
	if errClose := archive.Close(); errClose != nil {
		return nil, fmt.Errorf("close archive: %w", errClose)
	}
	archiveClosed = true

	data, errRead := os.ReadFile(archivePath)
	if errRead != nil {
		return nil, fmt.Errorf("read archive: %w", errRead)
	}
	return data, nil
}

func writeChecksum(checksumPath, archivePath string, archiveData []byte) error {
	if errMkdir := os.MkdirAll(filepath.Dir(checksumPath), 0o755); errMkdir != nil {
		return fmt.Errorf("create checksum directory: %w", errMkdir)
	}
	checksum := sha256.Sum256(archiveData)
	line := fmt.Sprintf("%s  %s\n", hex.EncodeToString(checksum[:]), filepath.Base(archivePath))
	if errWrite := os.WriteFile(checksumPath, []byte(line), 0o644); errWrite != nil {
		return fmt.Errorf("write checksum: %w", errWrite)
	}
	return nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
