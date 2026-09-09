// Package GoPak handles creating and extracting .pak asset archives (zip format with /Asset root).
package GoPak

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// PackAssetDir compresses the given assetDir into a pakFilePath (.pak archive).
// Inside the archive, all files are stored under the "Asset/" top-level folder prefix.
func PackAssetDir(assetDir string, pakFilePath string) (int, error) {
	if _, err := os.Stat(assetDir); os.IsNotExist(err) {
		return 0, fmt.Errorf("asset directory does not exist: %s", assetDir)
	}

	_ = os.MkdirAll(filepath.Dir(pakFilePath), 0755)
	outFile, err := os.Create(pakFilePath)
	if err != nil {
		return 0, fmt.Errorf("failed to create pak file: %w", err)
	}
	defer outFile.Close()

	zw := zip.NewWriter(outFile)
	defer zw.Close()

	count := 0
	err = filepath.Walk(assetDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(assetDir, path)
		if err != nil {
			return err
		}
		// Normalize to forward slashes under "Asset/"
		entryName := "Asset/" + filepath.ToSlash(rel)

		hdr := &zip.FileHeader{
			Name:   entryName,
			Method: zip.Deflate,
		}
		hdr.Modified = info.ModTime()

		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(w, f)
		if err != nil {
			return err
		}
		count++
		return nil
	})

	if err != nil {
		return 0, err
	}
	return count, nil
}

// ExtractPak extracts all contents of pakFilePath into destDir.
// If overwrite is false, existing files will be skipped.
func ExtractPak(pakFilePath string, destDir string, overwrite bool) (int, error) {
	r, err := zip.OpenReader(pakFilePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open pak file: %w", err)
	}
	defer r.Close()

	count := 0
	for _, f := range r.File {
		targetPath := filepath.Join(destDir, filepath.FromSlash(f.Name))
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(targetPath, 0755)
			continue
		}

		if !overwrite {
			if _, err := os.Stat(targetPath); err == nil {
				continue
			}
		}

		_ = os.MkdirAll(filepath.Dir(targetPath), 0755)
		outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			continue
		}
		rc, err := f.Open()
		if err == nil {
			_, _ = io.Copy(outFile, rc)
			_ = rc.Close()
			count++
		}
		_ = outFile.Close()
	}
	return count, nil
}

// ExtractPakBytes extracts zip payload bytes into destDir.
func ExtractPakBytes(zipData []byte, destDir string, overwrite bool) (int, error) {
	r, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return 0, fmt.Errorf("failed to read zip payload: %w", err)
	}

	count := 0
	for _, f := range r.File {
		targetPath := filepath.Join(destDir, filepath.FromSlash(f.Name))
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(targetPath, 0755)
			continue
		}

		if !overwrite {
			if _, err := os.Stat(targetPath); err == nil {
				continue
			}
		}

		_ = os.MkdirAll(filepath.Dir(targetPath), 0755)
		outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			continue
		}
		rc, err := f.Open()
		if err == nil {
			_, _ = io.Copy(outFile, rc)
			_ = rc.Close()
			count++
		}
		_ = outFile.Close()
	}
	return count, nil
}
