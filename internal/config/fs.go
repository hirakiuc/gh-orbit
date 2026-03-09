package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// SecureOpen provides a rooted, sanitized way to open files in XDG directories.
// It structurally satisfies G304 by using fs.Open on a DirFS.
func SecureOpen(baseDir, relPath string) (fs.File, error) {
	// 1. Validate base directory is absolute and cleaned
	baseDir = filepath.Clean(baseDir)
	if !filepath.IsAbs(baseDir) {
		return nil, fmt.Errorf("base directory must be absolute: %s", baseDir)
	}

	// 2. Create rooted FS
	rootFS := os.DirFS(baseDir)

	// 3. Clean relative path and ensure it doesn't try to escape
	relPath = filepath.Clean(relPath)
	if filepath.IsAbs(relPath) {
		// If caller passed an absolute path, try to make it relative to baseDir
		var err error
		relPath, err = filepath.Rel(baseDir, relPath)
		if err != nil {
			return nil, fmt.Errorf("path is outside base directory: %w", err)
		}
	}

	if relPath == ".." || (len(relPath) >= 3 && relPath[:3] == "../") {
		return nil, fmt.Errorf("path escapes base directory: %s", relPath)
	}

	// 4. Open via FS (safe from path injection)
	return rootFS.Open(relPath)
}

// SecureReadFile is a wrapper around SecureOpen for one-shot reads.
func SecureReadFile(baseDir, relPath string) ([]byte, error) {
	f, err := SecureOpen(baseDir, relPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	data := make([]byte, info.Size())
	_, err = f.Read(data)
	return data, err
}
