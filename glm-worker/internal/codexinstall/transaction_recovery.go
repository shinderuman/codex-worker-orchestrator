package codexinstall

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type installTransactionJournal struct {
	Version     int                         `json:"version"`
	Destination string                      `json:"destination"`
	Surfaces    []installTransactionSurface `json:"surfaces"`
}

type installTransactionSurface struct {
	Path string                   `json:"path"`
	Pre  installTransactionImage `json:"pre"`
	Post installTransactionImage `json:"post"`
}

type installTransactionImage struct {
	Exists  bool        `json:"exists"`
	Content []byte      `json:"content,omitempty"`
	Mode    os.FileMode `json:"mode,omitempty"`
}

const (
	installTransactionVersion      = 1
	installTransactionRelativePath = "codex-worker-orchestrator/install-transaction.json"
)

func installTransactionPath(codexDir string) string {
	return filepath.Join(codexDir, filepath.FromSlash(installTransactionRelativePath))
}

func plannedInstallState(preparation installPreparation) installState {
	files := make([]managedFileRecord, 0, len(preparation.filePlan.Desired))
	for _, file := range preparation.filePlan.Desired {
		files = append(files, managedFileRecord{Path: file.Path, SHA256: file.SHA256})
	}
	next := installState{Version: stateVersion, Files: files, Config: map[string]managedConfigRecord{}}
	if preparation.configPlan.Record != nil {
		next.Config[managedConfigKey] = *preparation.configPlan.Record
	}
	return next
}

func newInstallTransactionJournal(preparation installPreparation, backups []installBackup, next installState) (installTransactionJournal, error) {
	destination, err := filepath.Abs(preparation.codexDir)
	if err != nil {
		return installTransactionJournal{}, fmt.Errorf("resolve codex install transaction destination: %w", err)
	}
	post, err := plannedInstallTransactionPostimages(preparation, next)
	if err != nil {
		return installTransactionJournal{}, err
	}
	return buildInstallTransactionJournal(preparation.codexDir, filepath.Clean(destination), backups, post)
}

func plannedInstallTransactionPostimages(preparation installPreparation, next installState) (map[string]installTransactionImage, error) {
	stateData, err := encodeInstallState(next)
	if err != nil {
		return nil, err
	}
	post := map[string]installTransactionImage{
		filepath.Clean(statePath(preparation.codexDir)): {
			Exists:  true,
			Content: stateData,
			Mode:    0o644,
		},
	}
	for _, file := range preparation.filePlan.Desired {
		post[filepath.Clean(filepath.Join(preparation.codexDir, filepath.FromSlash(file.Path)))] = installTransactionImage{
			Exists:  true,
			Content: append([]byte(nil), file.Content...),
			Mode:    file.Mode.Perm(),
		}
	}
	for _, relative := range preparation.filePlan.Remove {
		post[filepath.Clean(filepath.Join(preparation.codexDir, filepath.FromSlash(relative)))] = installTransactionImage{}
	}
	if preparation.configPlan.Changed {
		post[filepath.Clean(preparation.configPlan.Path)] = installTransactionImage{
			Exists:  true,
			Content: append([]byte(nil), preparation.configPlan.Next...),
			Mode:    preparation.configPlan.Mode.Perm(),
		}
	}
	return post, nil
}

func buildInstallTransactionJournal(codexDir, destination string, backups []installBackup, post map[string]installTransactionImage) (installTransactionJournal, error) {
	journal := installTransactionJournal{Version: installTransactionVersion, Destination: destination}
	seen := make(map[string]bool, len(backups))
	for _, backup := range backups {
		path := filepath.Clean(backup.Path)
		if seen[path] {
			return installTransactionJournal{}, fmt.Errorf("duplicate codex install transaction surface %s", path)
		}
		postImage, ok := post[path]
		if !ok {
			return installTransactionJournal{}, fmt.Errorf("missing codex install transaction postimage for %s", path)
		}
		relative, err := filepath.Rel(codexDir, path)
		if err != nil {
			return installTransactionJournal{}, fmt.Errorf("resolve codex install transaction surface %s: %w", path, err)
		}
		relative = filepath.ToSlash(relative)
		if !validInstallTransactionSurfacePath(relative) {
			return installTransactionJournal{}, fmt.Errorf("invalid codex install transaction surface %s", relative)
		}
		seen[path] = true
		journal.Surfaces = append(journal.Surfaces, installTransactionSurface{
			Path: relative,
			Pre: installTransactionImage{
				Exists:  backup.Exists,
				Content: append([]byte(nil), backup.Content...),
				Mode:    backup.Mode.Perm(),
			},
			Post: postImage,
		})
	}
	if len(seen) != len(post) {
		return installTransactionJournal{}, fmt.Errorf("codex install transaction postimage set does not match mutation surfaces")
	}
	sort.Slice(journal.Surfaces, func(i, j int) bool { return journal.Surfaces[i].Path < journal.Surfaces[j].Path })
	if err := validateInstallTransactionJournal(codexDir, journal); err != nil {
		return installTransactionJournal{}, err
	}
	return journal, nil
}

func saveInstallTransactionJournal(path string, journal installTransactionJournal) error {
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return fmt.Errorf("encode codex install transaction journal: %w", err)
	}
	if err := writeAtomic(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("persist codex install transaction journal: %w", err)
	}
	return nil
}

func loadInstallTransactionJournal(codexDir string) (installTransactionJournal, error) {
	if err := validateManagedPathAncestors(codexDir, installTransactionRelativePath); err != nil {
		return installTransactionJournal{}, fmt.Errorf("validate codex install transaction journal path: %w", err)
	}
	path := installTransactionPath(codexDir)
	info, err := os.Lstat(path)
	if err != nil {
		return installTransactionJournal{}, err
	}
	if !info.Mode().IsRegular() {
		return installTransactionJournal{}, fmt.Errorf("codex install transaction journal is not a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return installTransactionJournal{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var journal installTransactionJournal
	if err := decoder.Decode(&journal); err != nil {
		return installTransactionJournal{}, fmt.Errorf("failed to decode Codex install transaction journal: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("unexpected trailing JSON value")
		}
		return installTransactionJournal{}, fmt.Errorf("failed to decode Codex install transaction journal: %w", err)
	}
	if err := validateInstallTransactionJournal(codexDir, journal); err != nil {
		return installTransactionJournal{}, err
	}
	return journal, nil
}

func validateInstallTransactionJournal(codexDir string, journal installTransactionJournal) error {
	if journal.Version != installTransactionVersion {
		return fmt.Errorf("unsupported codex install transaction journal version %d", journal.Version)
	}
	destination, err := filepath.Abs(codexDir)
	if err != nil {
		return fmt.Errorf("resolve codex install transaction destination: %w", err)
	}
	if filepath.Clean(journal.Destination) != filepath.Clean(destination) {
		return fmt.Errorf("codex install transaction destination mismatch")
	}
	if len(journal.Surfaces) == 0 {
		return fmt.Errorf("codex install transaction journal has no surfaces")
	}
	seen := make(map[string]bool, len(journal.Surfaces))
	stateCount := 0
	for _, surface := range journal.Surfaces {
		isState, err := validateInstallTransactionSurface(codexDir, surface, seen)
		if err != nil {
			return err
		}
		if isState {
			stateCount++
		}
	}
	if stateCount != 1 {
		return fmt.Errorf("codex install transaction journal must contain exactly one state surface")
	}
	return nil
}

func validateInstallTransactionSurface(codexDir string, surface installTransactionSurface, seen map[string]bool) (bool, error) {
	if !validInstallTransactionSurfacePath(surface.Path) || seen[surface.Path] {
		return false, fmt.Errorf("invalid codex install transaction surface %q", surface.Path)
	}
	if err := validateManagedPathAncestors(codexDir, surface.Path); err != nil {
		return false, fmt.Errorf("invalid codex install transaction surface %s: %w", surface.Path, err)
	}
	if err := validateInstallTransactionImage(surface.Pre); err != nil {
		return false, fmt.Errorf("invalid codex install transaction preimage for %s: %w", surface.Path, err)
	}
	if err := validateInstallTransactionImage(surface.Post); err != nil {
		return false, fmt.Errorf("invalid codex install transaction postimage for %s: %w", surface.Path, err)
	}
	seen[surface.Path] = true
	return surface.Path == stateRelativePath, nil
}

func validateInstallTransactionImage(image installTransactionImage) error {
	if !image.Exists {
		if len(image.Content) != 0 || image.Mode != 0 {
			return fmt.Errorf("absent image carries content or mode")
		}
		return nil
	}
	if image.Mode&^os.ModePerm != 0 {
		return fmt.Errorf("image mode contains non-permission bits")
	}
	return nil
}

func validInstallTransactionSurfacePath(path string) bool {
	if err := validateManagedRelativePath(path); err != nil {
		return false
	}
	if path == stateRelativePath || path == "config.toml" || path == "instructions/codex-worker-orchestrator.md" || path == "rules/glm-worker.rules" {
		return true
	}
	return strings.HasPrefix(path, "instructions/") || strings.HasPrefix(path, "glm-worker/prompts/")
}

func installTransactionPreimagesMatch(codexDir string) (bool, error) {
	journal, err := loadInstallTransactionJournal(codexDir)
	if err != nil {
		return false, err
	}
	for _, surface := range journal.Surfaces {
		current, err := captureInstallBackup(filepath.Join(codexDir, filepath.FromSlash(surface.Path)))
		if err != nil {
			return false, err
		}
		if !installTransactionImageMatches(current, surface.Pre) {
			return false, nil
		}
	}
	return true, nil
}

func recoverInterruptedInstall(codexDir string) error {
	journal, err := loadInstallTransactionJournal(codexDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	current, stateIndex, allPost, err := inspectInstallTransaction(codexDir, journal)
	if err != nil {
		return err
	}
	if allPost {
		return removeInstallTransactionJournal(codexDir, "finalize committed Codex install transaction")
	}
	if err := validateInstallTransactionRecoveryState(journal, current, stateIndex); err != nil {
		return err
	}
	if err := restoreInstallTransactionPreimages(codexDir, journal, current); err != nil {
		return err
	}
	return removeInstallTransactionJournal(codexDir, "remove recovered Codex install transaction journal")
}

func inspectInstallTransaction(codexDir string, journal installTransactionJournal) ([]installBackup, int, bool, error) {
	current := make([]installBackup, len(journal.Surfaces))
	stateIndex := -1
	allPost := true
	for i, surface := range journal.Surfaces {
		path := filepath.Join(codexDir, filepath.FromSlash(surface.Path))
		backup, err := captureInstallBackup(path)
		if err != nil {
			return nil, -1, false, err
		}
		current[i] = backup
		preMatch := installTransactionImageMatches(backup, surface.Pre)
		postMatch := installTransactionImageMatches(backup, surface.Post)
		if !preMatch && !postMatch {
			return nil, -1, false, fmt.Errorf("codex install transaction surface changed after interruption: %s", surface.Path)
		}
		if !postMatch {
			allPost = false
		}
		if surface.Path == stateRelativePath {
			stateIndex = i
		}
	}
	if stateIndex < 0 {
		return nil, -1, false, fmt.Errorf("codex install transaction journal is missing state surface")
	}
	return current, stateIndex, allPost, nil
}

func validateInstallTransactionRecoveryState(journal installTransactionJournal, current []installBackup, stateIndex int) error {
	stateSurface := journal.Surfaces[stateIndex]
	statePre := installTransactionImageMatches(current[stateIndex], stateSurface.Pre)
	statePost := installTransactionImageMatches(current[stateIndex], stateSurface.Post)
	if statePost && !statePre {
		return fmt.Errorf("codex install transaction state was committed but transaction surfaces are incomplete")
	}
	if !statePre {
		return fmt.Errorf("codex install transaction state does not match its preimage")
	}
	return nil
}

func restoreInstallTransactionPreimages(codexDir string, journal installTransactionJournal, current []installBackup) error {
	for i := len(journal.Surfaces) - 1; i >= 0; i-- {
		surface := journal.Surfaces[i]
		if installTransactionImageMatches(current[i], surface.Pre) {
			continue
		}
		if !installTransactionImageMatches(current[i], surface.Post) {
			return fmt.Errorf("codex install transaction surface cannot be recovered safely: %s", surface.Path)
		}
		backup := installTransactionImageBackup(codexDir, surface.Path, surface.Pre)
		if err := restoreInstallBackup(backup); err != nil {
			return fmt.Errorf("recover interrupted codex install surface %s: %w", surface.Path, err)
		}
	}
	return nil
}

func installTransactionImageMatches(current installBackup, expected installTransactionImage) bool {
	return current.Exists == expected.Exists && current.Mode.Perm() == expected.Mode.Perm() && bytes.Equal(current.Content, expected.Content)
}

func installTransactionImageBackup(codexDir, relative string, image installTransactionImage) installBackup {
	return installBackup{
		Path:    filepath.Join(codexDir, filepath.FromSlash(relative)),
		Exists:  image.Exists,
		Content: append([]byte(nil), image.Content...),
		Mode:    image.Mode.Perm(),
	}
}

func removeInstallTransactionJournal(codexDir, operation string) error {
	if err := os.Remove(installTransactionPath(codexDir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}
