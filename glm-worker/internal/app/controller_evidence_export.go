package app

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

type controllerEvidenceExportOutput struct {
	Target                       controller.EvidenceExportTarget `json:"target"`
	ArchivePath                  string                          `json:"archive_path"`
	ManifestDigest               string                          `json:"manifest_digest"`
	SealedSection                string                          `json:"sealed_section"`
	LiveSection                  string                          `json:"live_section"`
	Coverage                     string                          `json:"coverage"`
	AuthorityChangedDuringExport bool                            `json:"authority_changed_during_export"`
	ControllerGenerationBefore   uint64                          `json:"controller_generation_before"`
	ControllerGenerationAfter    uint64                          `json:"controller_generation_after"`
}

func executeControllerEvidenceExport(
	cfg config.AppConfig,
	store *controller.Store,
	command controllerEvidenceCommand,
) (controllerEvidenceOutput, error) {
	export, result, err := store.ExportEvidence(controller.EvidenceExportRequest{
		TaskPath:  command.TaskPath,
		AttemptID: command.AttemptID,
	})
	if err != nil {
		return controllerEvidenceOutput{}, err
	}
	archivePath, err := writeEvidenceExportArchive(cfg, export, result.ArchiveName)
	if err != nil {
		return controllerEvidenceOutput{}, err
	}
	return controllerEvidenceOutput{Action: command.Action, Export: &controllerEvidenceExportOutput{
		Target:                       result.Target,
		ArchivePath:                  archivePath,
		ManifestDigest:               result.ManifestDigest,
		SealedSection:                result.SealedStatus,
		LiveSection:                  result.LiveStatus,
		Coverage:                     result.Coverage,
		AuthorityChangedDuringExport: result.AuthorityChangedDuringExport,
		ControllerGenerationBefore:   result.ControllerGenerationBefore,
		ControllerGenerationAfter:    result.ControllerGenerationAfter,
	}}, nil
}

func writeEvidenceExportArchive(cfg config.AppConfig, export controller.EvidenceExport, baseName string) (string, error) {
	name, err := evidenceExportArchiveFileName(baseName, export.Manifest.CreatedAt)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(filepath.Dir(cfg.StateBase), "exports", cfg.RepoHash)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create evidence export directory: %w", err)
	}
	path := filepath.Join(dir, name)
	if err := writeEvidenceExportArchiveFile(path, export); err != nil {
		return "", err
	}
	return path, nil
}

func evidenceExportArchiveFileName(baseName string, observedAt time.Time) (string, error) {
	if !evidenceExportArchiveNameSafe(baseName) {
		return "", fmt.Errorf("evidence export archive name is invalid: %q", baseName)
	}
	var collision [4]byte
	if _, err := rand.Read(collision[:]); err != nil {
		return "", fmt.Errorf("generate evidence export archive collision id: %w", err)
	}
	name := fmt.Sprintf("%s-%d-%s.zip", strings.TrimSuffix(baseName, ".zip"), observedAt.UnixNano(), hex.EncodeToString(collision[:]))
	if !evidenceExportArchiveNameSafe(name) {
		return "", fmt.Errorf("evidence export archive name is invalid: %q", name)
	}
	return name, nil
}

func evidenceExportArchiveNameSafe(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return name == filepath.Base(name) && !strings.ContainsAny(name, `/\`)
}

func writeEvidenceExportArchiveFile(path string, export controller.EvidenceExport) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".evidence-export-*.tmp")
	if err != nil {
		return fmt.Errorf("create evidence export temp archive: %w", err)
	}
	temporary := file.Name()
	defer func() { _ = os.Remove(temporary) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("chmod evidence export temp archive: %w", err)
	}
	if err := writeEvidenceExportZip(file, export); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync evidence export temp archive: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close evidence export temp archive: %w", err)
	}
	if err := publishEvidenceExportArchive(temporary, path); err != nil {
		return err
	}
	return syncEvidenceExportDirectory(dir)
}

func publishEvidenceExportArchive(temporary, path string) error {
	if err := os.Link(temporary, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("evidence export archive already exists: %s", path)
		}
		return fmt.Errorf("publish evidence export archive: %w", err)
	}
	return nil
}

func writeEvidenceExportZip(target io.Writer, export controller.EvidenceExport) error {
	writer := zip.NewWriter(target)
	for _, file := range export.Files {
		entry, err := writer.CreateHeader(&zip.FileHeader{
			Name:     file.Path,
			Method:   zip.Deflate,
			Modified: time.Unix(0, 0).UTC(),
		})
		if err != nil {
			return fmt.Errorf("stage evidence export entry %s: %w", file.Path, err)
		}
		if _, err := entry.Write(file.Data); err != nil {
			return fmt.Errorf("write evidence export entry %s: %w", file.Path, err)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finalize evidence export archive: %w", err)
	}
	return nil
}

func syncEvidenceExportDirectory(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open evidence export directory: %w", err)
	}
	if err := handle.Sync(); err != nil {
		_ = handle.Close()
		return fmt.Errorf("sync evidence export directory: %w", err)
	}
	return handle.Close()
}
