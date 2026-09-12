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

const (
	stateVersion      = 1
	stateRelativePath = "codex-worker-orchestrator/install-state.json"
)

func statePath(codexDir string) string {
	return filepath.Join(codexDir, filepath.FromSlash(stateRelativePath))
}

func loadState(codexDir string) (installState, bool, error) {
	if err := validateManagedPathAncestors(codexDir, stateRelativePath); err != nil {
		return installState{}, false, err
	}
	path := statePath(codexDir)
	data, exists, err := readOptionalRegularStateFile(path)
	if err != nil {
		return installState{}, false, err
	}
	if !exists {
		return installState{Version: stateVersion, Config: map[string]managedConfigRecord{}}, false, nil
	}
	state, err := decodeInstallState(data)
	if err != nil {
		return installState{}, false, err
	}
	return state, true, nil
}

func readOptionalRegularStateFile(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("stat Codex install state: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("codex install state is not a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("read Codex install state: %w", err)
	}
	return data, true, nil
}

func decodeInstallState(data []byte) (installState, error) {
	var state installState
	if err := json.Unmarshal(data, &state); err != nil {
		return installState{}, fmt.Errorf("decode Codex install state: %w", err)
	}
	if err := validateInstallState(&state); err != nil {
		return installState{}, err
	}
	return state, nil
}

func validateInstallState(state *installState) error {
	if state.Version != stateVersion {
		return fmt.Errorf("unsupported Codex install state version %d", state.Version)
	}
	if state.Config == nil {
		state.Config = map[string]managedConfigRecord{}
	}
	if err := validateInstallStateFiles(state.Files); err != nil {
		return err
	}
	return validateInstallStateConfig(state.Config)
}

func validateInstallStateFiles(records []managedFileRecord) error {
	seen := map[string]bool{}
	for _, record := range records {
		if err := validateManagedRelativePath(record.Path); err != nil || record.SHA256 == "" || seen[record.Path] {
			return fmt.Errorf("invalid Codex install state file record %q", record.Path)
		}
		seen[record.Path] = true
	}
	return nil
}

func validateInstallStateConfig(records map[string]managedConfigRecord) error {
	for key, record := range records {
		if key != managedConfigKey || record.Value == "" || record.LineSHA256 == "" {
			return fmt.Errorf("invalid Codex install state config record %q", key)
		}
	}
	return nil
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

func stateFileMap(state installState) map[string]managedFileRecord {
	records := make(map[string]managedFileRecord, len(state.Files))
	for _, record := range state.Files {
		records[record.Path] = record
	}
	return records
}

func validateManagedRelativePath(path string) error {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, "\\") {
		return fmt.Errorf("invalid managed Codex relative path")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean == "." || clean != path || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("invalid managed Codex relative path")
	}
	return nil
}

func validateManagedPathAncestors(codexDir, relativePath string) error {
	if err := validateManagedRelativePath(relativePath); err != nil {
		return err
	}
	parts := strings.Split(filepath.FromSlash(relativePath), string(filepath.Separator))
	current := codexDir
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("stat managed Codex path ancestor %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("managed Codex path ancestor is not a real directory: %s", current)
		}
	}
	return nil
}
