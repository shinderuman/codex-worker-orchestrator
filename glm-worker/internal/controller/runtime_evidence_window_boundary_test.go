package controller

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestAttemptSealValidationRecordsFilterForeignTaskAndWindowInBothStores(t *testing.T) {
	t.Parallel()
	for _, directory := range []string{qualitygate.RunDirectory, evidenceExportValidationSmokeDirectory} {
		for _, scope := range []string{"runtime", "repository"} {
			t.Run(directory+"/"+scope, func(t *testing.T) {
				t.Parallel()
				fixture, _ := newPublicationTestFixture(t)
				st := state.AttachStateStore(config.AppConfig{
					StateBase: t.TempDir(), RepoHash: "validation-window-fixture",
				})
				const runID = "cccccccccccccccccccccccccccccccc"
				runPath := st.Path(filepath.Join(directory, runID, qualitygate.RunFile))
				if err := os.MkdirAll(filepath.Dir(runPath), 0o700); err != nil {
					t.Fatal(err)
				}
				start := time.Now().UTC().Add(-2 * time.Minute)
				end := start.Add(time.Minute)
				seal := &AttemptSeal{AttemptID: "validation-window-attempt", StartedAt: start, SealedAt: end}
				c := &attemptRuntimeEvidence{
					store: fixture.store, runtime: st, seal: seal,
					taskID: "current-task", workspace: fixture.store.Identity().PrimaryRoot,
				}
				collect := func() error {
					if scope == "runtime" {
						return c.captureRuntimeValidationRun(directory, runID)
					}
					return c.captureRepositoryValidationRun(st, directory, runID)
				}
				record := qualitygate.RunRecord{
					ValidationRunID: runID, TaskID: "foreign-task",
					StartedAt:  start.Add(10 * time.Second),
					Repository: fixture.store.Identity().PrimaryRoot,
					WorkingDir: fixture.store.Identity().PrimaryRoot,
				}
				write := func() {
					t.Helper()
					data, err := json.Marshal(record)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(runPath, data, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				write()
				if err := collect(); err != nil {
					t.Fatal(err)
				}
				if len(seal.EvidenceRefs) != 0 || len(seal.Unreadable) != 0 || len(seal.Missing) != 0 {
					t.Fatalf("foreign task unexpectedly entered evidence: %#v", seal)
				}
				record.TaskID = "current-task"
				record.StartedAt = end.Add(time.Second)
				write()
				if err := collect(); err != nil {
					t.Fatal(err)
				}
				if len(seal.EvidenceRefs) != 0 || len(seal.Unreadable) != 0 || len(seal.Missing) != 0 {
					t.Fatalf("out-of-window record unexpectedly entered evidence: %#v", seal)
				}
				record.StartedAt = start.Add(10 * time.Second)
				write()
				if err := collect(); err != nil {
					t.Fatal(err)
				}
				if len(seal.EvidenceRefs) == 0 || len(seal.Unreadable) != 0 || len(seal.Missing) != 0 {
					t.Fatalf("current valid record not retained: %#v", seal)
				}
			})
		}
	}
}
