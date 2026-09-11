package codexinstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	stateVersion       = 1
	stateRelativePath  = "codex-worker-orchestrator/install-state.json"
	legacyManifestName = ".codex-config-managed-files"
)

type managedFileRecord struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type managedConfigRecord struct {
	Value      string `json:"value"`
	LineSHA256 string `json:"line_sha256"`
}

type installState struct {
	Version int                            `json:"version"`
	Files   []managedFileRecord            `json:"files,omitempty"`
	Config  map[string]managedConfigRecord `json:"config,omitempty"`
}

type legacyManifest struct {
	Present bool
	Paths   map[string]bool
}

func statePath(codexDir string) string {
	return filepath.Join(codexDir, filepath.FromSlash(stateRelativePath))
}

func loadState(codexDir string) (installState, bool, error) {
	path := statePath(codexDir)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return installState{Version: stateVersion, Config: map[string]managedConfigRecord{}}, false, nil
	}
	if err != nil {
		return installState{}, false, fmt.Errorf("read Codex install state: %w", err)
	}
	var state installState
	if err := json.Unmarshal(data, &state); err != nil {
		return installState{}, false, fmt.Errorf("decode Codex install state: %w", err)
	}
	if state.Version != stateVersion {
		return installState{}, false, fmt.Errorf("unsupported Codex install state version %d", state.Version)
	}
	if state.Config == nil {
		state.Config = map[string]managedConfigRecord{}
	}
	seen := map[string]bool{}
	for _, record := range state.Files {
		if record.Path == "" || record.SHA256 == "" || seen[record.Path] {
			return installState{}, false, fmt.Errorf("invalid Codex install state file record %q", record.Path)
		}
		seen[record.Path] = true
	}
	return state, true, nil
}

func loadLegacyManifest(codexDir string) (legacyManifest, error) {
	path := filepath.Join(codexDir, legacyManifestName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return legacyManifest{Paths: map[string]bool{}}, nil
	}
	if err != nil {
		return legacyManifest{}, fmt.Errorf("read legacy Codex managed-file manifest: %w", err)
	}
	paths := map[string]bool{}
	for _, raw := range strings.Split(string(data), "\n") {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		if !supportedLegacyManagedPath(path) {
			return legacyManifest{}, fmt.Errorf("legacy Codex managed-file manifest contains unsupported path %q", path)
		}
		paths[path] = true
	}
	return legacyManifest{Present: true, Paths: paths}, nil
}

func writeState(codexDir string, state installState) error {
	state.Version = stateVersion
	sort.Slice(state.Files, func(i, j int) bool { return state.Files[i].Path < state.Files[j].Path })
	if len(state.Config) == 0 {
		state.Config = nil
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Codex install state: %w", err)
	}
	data = append(data, '\n')
	path := statePath(codexDir)
	if err := writeAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf("write Codex install state: %w", err)
	}
	return nil
}

func removeLegacyManifest(codexDir string, present bool) error {
	if !present {
		return nil
	}
	path := filepath.Join(codexDir, legacyManifestName)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove legacy Codex managed-file manifest: %w", err)
	}
	return nil
}

func stateFileMap(state installState) map[string]managedFileRecord {
	records := make(map[string]managedFileRecord, len(state.Files))
	for _, record := range state.Files {
		records[record.Path] = record
	}
	return records
}
