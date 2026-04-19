package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type IndexManifest struct {
	BlobDigest string `json:"blobDigest"`
	ConfigHash string `json:"configHash"`
}

const manifestFile = "manifest.json"

func Save(dir string, m IndexManifest) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating manifest dir: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, manifestFile), data, 0o644)
}

func Load(dir string) (IndexManifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, manifestFile))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return IndexManifest{}, err
		}
		return IndexManifest{}, fmt.Errorf("reading manifest: %w", err)
	}
	var m IndexManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return IndexManifest{}, fmt.Errorf("parsing manifest: %w", err)
	}
	return m, nil
}
