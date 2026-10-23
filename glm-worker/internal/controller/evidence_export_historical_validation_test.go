package controller

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExportEvidenceRetainsAndExcludesProvenHistoricalValidation(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"historical", "missing", "wrong-identity", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			fixture := newEvidenceExportFixture(t)
			history, err := state.NewStateStore(config.AppConfig{StateBase: t.TempDir(), RepoHash: "historical-task"})
			if err != nil {
				t.Fatal(err)
			}
			oldID, err := history.StartNewTask()
			if err != nil {
				t.Fatal(err)
			}
			if err := history.SetTaskStatus(state.TaskStatusUserTerminated); err != nil {
				t.Fatal(err)
			}
			if _, err := history.StartNewTask(); err != nil {
				t.Fatal(err)
			}
			archive, err := os.ReadFile(history.TaskStatsArchivePath(oldID))
			if err != nil {
				t.Fatal(err)
			}
			if mode == "wrong-identity" {
				var value map[string]any
				if err := json.Unmarshal(archive, &value); err != nil {
					t.Fatal(err)
				}
				value["task_id"] = "another-runtime-task"
				archive, err = json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode == "corrupt" {
				archive = []byte("{")
			}
			if mode != "missing" {
				archivePath := fixture.runtime.TaskStatsArchivePath(oldID)
				if err := os.MkdirAll(filepath.Dir(archivePath), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			record := qualitygate.RunRecord{ValidationRunID: "abababababababababababababababab", Form: "go-test", Repository: fixture.repo, WorkingDir: fixture.repo, StartedAt: time.Unix(6100, 0).UTC(), Status: qualitygate.StatusPass, TaskID: oldID}
			writeEvidenceGateRun(t, fixture.runtime, record)
			record.ValidationRunID, record.Form = "babababababababababababababababa", "install-smoke"
			smokeData, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			smoke := fixture.runtime.Path(filepath.Join(evidenceExportValidationSmokeDirectory, record.ValidationRunID))
			if err := os.MkdirAll(smoke, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(smoke, qualitygate.RunFile), smokeData, 0o600); err != nil {
				t.Fatal(err)
			}
			export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
			if mode != "historical" {
				if err == nil {
					t.Fatal("unproven historical validation was admitted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"live/validation/quality-gate/abababababababababababababababab/run.json", "live/validation/install-smoke/" + record.ValidationRunID + "/run.json"} {
				if exportHasEntry(export.Manifest, path) {
					t.Fatal("historical validation was attributed to the current task")
				}
			}
			if !exportHasEntry(export.Manifest, "live/validation/quality-gate/"+evidenceExportGateRunID+"/run.json") {
				t.Fatal("current validation disappeared")
			}
			preserved, err := os.ReadFile(filepath.Join(smoke, qualitygate.RunFile))
			if err != nil || !bytes.Equal(preserved, smokeData) {
				t.Fatal("historical validation was modified")
			}
		})
	}
}
