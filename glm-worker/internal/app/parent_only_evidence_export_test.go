package app

import (
	"archive/zip"
	"encoding/json"
	"io"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

func assertParentOnlyEvidenceExport(t *testing.T, cfg config.AppConfig, store *controller.Store, attemptID string, live bool) {
	t.Helper()
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	exit, output, diagnostics := runTypedParentActionCLI(t, cfg, "export-bundle")
	if exit != 0 {
		t.Fatalf("parent-only Bundle export: %s", diagnostics)
	}
	var exported controllerEvidenceExportOutput
	if err := json.Unmarshal([]byte(output), &exported); err != nil {
		t.Fatal(err)
	}
	if exported.Target.AttemptID != attemptID || exported.Target.RuntimeTaskID != "" || exported.Target.Live != live || exported.RuntimeSection != "absent" {
		t.Fatalf("parent-only Bundle attributed to model Runtime: %#v", exported)
	}
	if filepath.Base(exported.ArchivePath) != "parent-only-"+attemptID+".zip" {
		t.Fatalf("parent-only Bundle identity: %s", exported.ArchivePath)
	}
	verifyExportArchive(t, &exported)
	archive, err := zip.OpenReader(exported.ArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = archive.Close() }()
	for _, file := range archive.File {
		if file.Name != "manifest.json" {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		var manifest controller.EvidenceExportManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatal(err)
		}
		if manifest.Runtime.RuntimeTaskID != "" || len(manifest.Runtime.SessionIDs) != 0 || manifest.Git == nil {
			t.Fatalf("parent-only Bundle source evidence: %#v", manifest)
		}
		if !live && (manifest.Attempt.Status != "present" || manifest.Attempt.SealDigest == "") {
			t.Fatal("completed parent-only Bundle lacks the attempt seal")
		}
	}
	after, err := store.LoadHead()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("Bundle export changed controller authority: %v", err)
	}
}
