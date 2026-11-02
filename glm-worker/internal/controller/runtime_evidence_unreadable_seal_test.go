package controller

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestAttemptSealRejectsUnreadableValidationRecordsInBothStores(t *testing.T) {
	t.Parallel()
	for _, directory := range []string{qualitygate.RunDirectory, evidenceExportValidationSmokeDirectory} {
		for _, scope := range []string{"runtime", "repository"} {
			t.Run(directory+"/"+scope, func(t *testing.T) {
				t.Parallel()
				st := state.AttachStateStore(config.AppConfig{StateBase: t.TempDir(), RepoHash: "validation-unreadable"})
				const runID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				path := st.Path(filepath.Join(directory, runID, qualitygate.RunFile))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
					t.Fatal(err)
				}
				seal := &AttemptSeal{AttemptID: "attempt"}
				collector := &attemptRuntimeEvidence{runtime: st, seal: seal}
				var err error
				if scope == "runtime" {
					err = collector.captureRuntimeValidationRun(directory, runID)
				} else {
					err = collector.captureRepositoryValidationRun(st, directory, runID)
				}
				if err != nil {
					t.Fatal(err)
				}
				want := []string{"attempt:state/" + directory + "/" + runID + "/" + qualitygate.RunFile}
				if !reflect.DeepEqual(seal.Unreadable, want) || len(seal.Missing) != 0 {
					t.Fatalf("corrupt record was not marked unreadable: %#v", seal)
				}
				applyAttemptSealRuntimeCoverage(seal, true)
				if seal.Coverage != attemptSealCoverageIncomplete {
					t.Fatalf("unreadable record had coverage %q", seal.Coverage)
				}
				seal.RequiredKinds = []string{"runtime-state"}
				seal.Coverage = attemptSealCoverageComplete
				if err := validateAttemptSealCoverage(*seal); err == nil {
					t.Fatal("complete seal accepted unreadable record")
				}
			})
		}
	}
}

func TestAttemptSealDistinguishesMissingRunRecordFromCorruption(t *testing.T) {
	t.Parallel()
	for _, directory := range []string{qualitygate.RunDirectory, evidenceExportValidationSmokeDirectory} {
		t.Run(directory, func(t *testing.T) {
			t.Parallel()
			st := state.AttachStateStore(config.AppConfig{StateBase: t.TempDir(), RepoHash: "validation-missing"})
			const runID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			if err := os.MkdirAll(st.Path(filepath.Join(directory, runID)), 0o755); err != nil {
				t.Fatal(err)
			}
			seal := &AttemptSeal{AttemptID: "attempt"}
			c := &attemptRuntimeEvidence{runtime: st, seal: seal}
			if err := c.captureRuntimeValidationRun(directory, runID); err != nil {
				t.Fatal(err)
			}
			want := []string{"attempt:state/" + directory + "/" + runID + "/" + qualitygate.RunFile}
			if !reflect.DeepEqual(seal.Missing, want) || len(seal.Unreadable) != 0 {
				t.Fatalf("missing run record misclassified: %#v", seal)
			}
		})
	}
}
